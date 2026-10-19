package alertwriter

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

type effectRetries struct {
	remaining int
	backoff   time.Duration
}

// retry shares failed-attempt allowance and backoff across all sink and poison
// effects in one owned batch. Success cannot reset either bound.
func (w *Writer) retry(ctx context.Context, retries *effectRetries, effect func(context.Context) error) error {
	for ctx.Err() == nil {
		opCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
		err := effect(opCtx)
		cancel()
		if err == nil || errors.Is(err, ErrConflict) {
			return err
		}
		if retries.remaining == 0 {
			return err
		}
		retries.remaining--
		w.log.Warn().Msg("alert effect failed; retrying")
		timer := time.NewTimer(retries.backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		retries.backoff = min(retries.backoff*backoffMultiplier, w.settings.RetryMaxBackoff)
	}
	return ctx.Err()
}

func (w *Writer) quarantine(ctx context.Context, record *kgo.Record, reason kafkax.DLQReason,
	retries *effectRetries) error {
	if w.dlq == nil {
		return errors.New("alert quarantine unavailable")
	}
	publish := func(op context.Context) error { return w.dlq.Publish(op, record, reason) }
	if err := w.retry(ctx, retries, publish); err != nil {
		return errors.New("alert quarantine failed within bounded attempts")
	}
	return nil
}

func (w *Writer) persistRecord(ctx context.Context, record *kgo.Record, retries *effectRetries) error {
	if ctx.Err() != nil {
		return errors.New("alert persistence budget exhausted")
	}
	alert, err := contracts.DecodeAlert(bytes.NewReader(record.Value))
	if err != nil {
		return w.quarantine(ctx, record, kafkax.DLQReasonDecode, retries)
	}
	if err = ValidateIdentity(alert, record.Key); err != nil {
		return w.quarantine(ctx, record, kafkax.DLQReasonValidation, retries)
	}
	fingerprint, err := ImmutableFingerprint(alert)
	if err != nil {
		return errors.New("alert fingerprint failed")
	}
	err = w.retry(ctx, retries, func(op context.Context) error { return w.persist(op, alert, fingerprint) })
	if errors.Is(err, ErrConflict) {
		// persist has returned and independently rolled back its lock before
		// delayed quarantine. Rejected content was never eligible for CH insert.
		return w.quarantine(ctx, record, kafkax.DLQReasonValidation, retries)
	}
	if err != nil {
		return errors.New("alert persistence failed within bounded attempts")
	}
	return nil
}
