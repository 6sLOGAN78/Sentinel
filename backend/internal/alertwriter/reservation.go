package alertwriter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const maxReservationPayload = 1048576

// Corrupt durable state is an operational failure, never source poison.
var errReservationState = errors.New("invalid durable alert reservation")

type reservationAttempt struct {
	alert         *contracts.Alert
	preflightDone bool
	retained      bool
}

func (w *Writer) loadReservation(ctx context.Context, tx pgx.Tx, id string) (*contracts.Alert, string, error) {
	tenant, fingerprint, payload, err := w.workflows.FindReservation(ctx, tx, id)
	if err != nil {
		return nil, "", err
	}
	if len(payload) == 0 || len(payload) > maxReservationPayload {
		return nil, "", errReservationState
	}
	alert, err := contracts.DecodeAlert(bytes.NewReader(payload))
	if err != nil {
		return nil, "", errReservationState
	}
	if err = ValidateIdentity(alert, []byte(tenant+"|"+id)); err != nil {
		return nil, "", errReservationState
	}
	actual, err := ImmutableFingerprint(alert)
	if err != nil || actual != fingerprint {
		return nil, "", errReservationState
	}
	return alert, fingerprint, nil
}

func (w *Writer) prepareReservation(ctx context.Context, alert *contracts.Alert,
	fingerprint string,
) (reservationAttempt, error) {
	attempt := reservationAttempt{}
	tx, err := w.workflows.BeginLocked(ctx, alert.AlertID)
	if err != nil {
		return attempt, err
	}
	defer rollback(tx)
	tenant, winner, err := w.workflows.Find(ctx, tx, alert.AlertID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return attempt, err
	}
	saved, selected, reservationErr := w.loadReservation(ctx, tx, alert.AlertID)
	if reservationErr != nil && !errors.Is(reservationErr, pgx.ErrNoRows) {
		return attempt, reservationErr
	}
	if err == nil && (tenant != alert.TenantID || winner != fingerprint) {
		return attempt, ErrConflict
	}
	if errors.Is(reservationErr, pgx.ErrNoRows) {
		attempt, err = w.preflightReservation(ctx, alert, fingerprint)
		if err != nil {
			return attempt, err
		}
		payload, marshalErr := json.Marshal(attempt.alert)
		if marshalErr != nil || len(payload) == 0 || len(payload) > maxReservationPayload {
			return attempt, errReservationState
		}
		if err = w.workflows.InsertReservation(ctx, tx, alert.AlertID, alert.TenantID, fingerprint, payload); err != nil {
			return attempt, err
		}
		// A separate statement sees a concurrent insert-or-ignore winner. Never
		// infer selected bytes from the attempted INSERT or a RETURNING absence.
		saved, selected, reservationErr = w.loadReservation(ctx, tx, alert.AlertID)
		if reservationErr != nil {
			return attempt, errReservationState
		}
	}
	if saved.TenantID != alert.TenantID || selected != fingerprint {
		return attempt, ErrConflict
	}
	attempt.alert = saved
	return attempt, tx.Commit(ctx)
}

func (w *Writer) persistReserved(ctx context.Context, attempt reservationAttempt, fingerprint string) error {
	tx, err := w.workflows.BeginLocked(ctx, attempt.alert.AlertID)
	if err != nil {
		return err
	}
	defer rollback(tx)
	alert, selected, err := w.loadReservation(ctx, tx, attempt.alert.AlertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errReservationState
	}
	if err != nil {
		return err
	}
	if alert.TenantID != attempt.alert.TenantID || selected != fingerprint {
		return ErrConflict
	}
	tenant, winner, err := w.workflows.Find(ctx, tx, alert.AlertID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && (tenant != alert.TenantID || winner != fingerprint) {
		return ErrConflict
	}
	hasWinner := err == nil
	if !attempt.preflightDone {
		attempt, err = w.preflightReservation(ctx, alert, fingerprint)
		if err != nil {
			return err
		}
	}
	if !attempt.retained {
		if err = w.detections.InsertBatch(ctx, []*contracts.Alert{alert}); err != nil {
			return err
		}
	}
	// Skip even ON CONFLICT INSERT for an equal workflow: defaults allocate seq
	// before detecting a conflict, and mutable triage state already has an owner.
	if !hasWinner {
		if err = w.workflows.Ensure(ctx, tx, alert, fingerprint); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Select retained orphan bytes only while establishing a missing reservation.
// Phase two uses the retained flag but always persists its reloaded saved alert.
func (w *Writer) preflightReservation(ctx context.Context, alert *contracts.Alert,
	fingerprint string,
) (reservationAttempt, error) {
	attempt := reservationAttempt{alert: alert, preflightDone: true}
	retained, err := w.detections.Get(ctx, alert.TenantID, alert.AlertID)
	if errors.Is(err, storageclickhouse.ErrNotFound) {
		return attempt, nil
	}
	if err != nil {
		return attempt, err
	}
	actual, err := ImmutableFingerprint(retained)
	if err != nil {
		return attempt, err
	}
	if actual != fingerprint {
		return attempt, ErrConflict
	}
	attempt.alert, attempt.retained = retained, true
	return attempt, nil
}
