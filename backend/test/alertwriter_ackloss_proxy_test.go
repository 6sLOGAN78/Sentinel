//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"

	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

// Byte-transparent upstream: no native parser, altered packets, delayed final
// block, or replacement server. Only downstream responses are suppressed.
type reservationACKProxy struct {
	listener    net.Listener
	ctx         context.Context
	upstream    string
	armed       atomic.Bool
	suppressed  atomic.Int64
	mu          sync.Mutex
	connections []net.Conn
	wg          sync.WaitGroup
}

func newReservationACKProxy(t *testing.T, upstream string) *reservationACKProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	p := &reservationACKProxy{listener: listener, ctx: ctx, upstream: upstream}
	p.wg.Add(2)
	go p.accept()
	go func() {
		defer p.wg.Done()
		<-ctx.Done()
		_ = listener.Close()
		p.cutSockets()
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		p.cutSockets()
		joined := make(chan struct{})
		go func() { p.wg.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("native ACK proxy goroutines did not join")
		}
	})
	return p
}

func (p *reservationACKProxy) accept() {
	defer p.wg.Done()
	for {
		front, err := p.listener.Accept()
		if err != nil {
			return
		}
		back, err := (&net.Dialer{Timeout: time.Second}).DialContext(p.ctx, "tcp", p.upstream)
		if err != nil {
			_ = front.Close()
			return
		}
		p.mu.Lock()
		if p.ctx.Err() != nil {
			p.mu.Unlock()
			_ = front.Close()
			_ = back.Close()
			return
		}
		p.connections = append(p.connections, front, back)
		p.mu.Unlock()
		deadline, _ := p.ctx.Deadline()
		_ = front.SetDeadline(deadline)
		_ = back.SetDeadline(deadline)
		p.wg.Add(2)
		go func() {
			defer p.wg.Done()
			defer front.Close()
			defer back.Close()
			_, _ = io.Copy(back, front)
		}()
		go p.downstream(front, back)
	}
}

func (p *reservationACKProxy) downstream(front, back net.Conn) {
	defer p.wg.Done()
	defer front.Close()
	defer back.Close()
	buffer := make([]byte, 32*1024)
	for {
		n, err := back.Read(buffer)
		if n > 0 {
			if p.armed.Load() {
				p.suppressed.Add(int64(n))
			} else if _, writeErr := front.Write(buffer[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *reservationACKProxy) cutSockets() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range p.connections {
		_ = conn.Close()
	}
}

type reservationACKConn struct {
	driver.Conn
	proxy      *reservationACKProxy
	sendResult chan error
}

func (c *reservationACKConn) PrepareBatch(ctx context.Context, query string,
	options ...driver.PrepareBatchOption,
) (driver.Batch, error) {
	batch, err := c.Conn.PrepareBatch(ctx, query, options...)
	if err != nil {
		return nil, err
	}
	return &reservationACKBatch{Batch: batch, conn: c}, nil
}

type reservationACKBatch struct {
	driver.Batch
	conn *reservationACKConn
}

func (b *reservationACKBatch) Send() error {
	// PrepareBatch's sample/header exchange completed normally. Arming here
	// cannot block the request handshake or fabricate a successful Send result.
	b.conn.proxy.armed.Store(true)
	err := b.Batch.Send()
	b.conn.sendResult <- err
	return err
}

// Native proof: CH independently publishes A while the native Send still waits
// for its response; closing real sockets makes that same Send return an error.
// This proves ACK loss after visibility, not stock-server pre-visibility delay.
func TestAlertWriterReservationNativeAckLoss(t *testing.T) {
	f := newAlertReplayFixture(t)
	preserveReservationPhysicalRows(t, f)
	raw, err := json.Marshal(f.alert)
	require.NoError(t, err)
	proxy := newReservationACKProxy(t, f.chConfig.NativeAddress)
	cfg := f.chConfig
	cfg.NativeAddress = proxy.listener.Addr().String()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	conn, err := storageclickhouse.Open(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	faultConn := &reservationACKConn{Conn: conn, proxy: proxy, sendResult: make(chan error, 1)}
	consumer, err := f.consumer(t)
	require.NoError(t, err)
	f.publish(t, f.alert)
	result := make(chan error, 1)
	go func() {
		result <- f.runConsumer(ctx, consumer, storageclickhouse.NewAlertStore(faultConn),
			storagepostgres.NewAlertStore(f.db.Pool, time.Second))
	}()
	t.Cleanup(func() {
		cancel()
		proxy.cutSockets()
		closeWriterConsumer(t, consumer)
		select {
		case <-result:
		case <-time.After(5 * time.Second):
			t.Error("native fault writer did not join")
		}
	})
	require.Eventually(t, func() bool {
		var count uint64
		queryCtx, queryCancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer queryCancel()
		return f.ch.QueryRow(queryCtx, "SELECT count() FROM alerts WHERE alert_id={id:String}",
			driverclickhouse.Named("id", f.alert.AlertID)).Scan(&count) == nil && count == 1
	}, 6*time.Second, 5*time.Millisecond, "independent native read must prove the complete request took effect")
	require.True(t, proxy.armed.Load())
	requireReservationPhysical(t, f, raw, 1)
	require.Eventually(t, func() bool { return proxy.suppressed.Load() > 0 },
		100*time.Millisecond, time.Millisecond, "native server responses must actually be suppressed")
	select {
	case sendErr := <-faultConn.sendResult:
		t.Fatalf("native Send returned before the forced socket fault: %v", sendErr)
	default:
	}
	proxy.cutSockets()
	var sendErr error
	select {
	case sendErr = <-faultConn.sendResult:
	case <-time.After(time.Second):
		t.Fatal("native Send did not return after real socket closure")
	}
	require.Error(t, sendErr)
	require.NotErrorIs(t, sendErr, context.DeadlineExceeded)
	require.NotErrorIs(t, sendErr, context.Canceled)
	var socketError net.Error
	require.True(t, errors.Is(sendErr, io.EOF) || errors.Is(sendErr, io.ErrUnexpectedEOF) ||
		errors.As(sendErr, &socketError), "must be a native socket failure, got %T: %v", sendErr, sendErr)
	if errors.As(sendErr, &socketError) {
		require.False(t, socketError.Timeout(), "the forced socket fault must precede the operation deadline")
	}
	t.Logf("real native Batch.Send after physical visibility: %T: %v; suppressed response bytes=%d",
		sendErr, sendErr, proxy.suppressed.Load())
	select {
	case runErr := <-result:
		require.Error(t, runErr)
		result <- runErr // Preserve completion for the cleanup join.
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not end the uncertain CH attempt")
	}
	closeWriterConsumer(t, consumer)
	requireReservationTransactionEnded(t, f)
	requireReservationState(t, f, raw, 0)
	require.Zero(t, f.committed(t))
	rejectIndependentReservationConflict(t, f)
	requireReservationState(t, f, raw, 0)
	requireReservationPhysical(t, f, raw, 1)
	p := f.binary(t)
	f.waitOffset(t, 1)
	requireReservationState(t, f, raw, 1)
	requireReservationPhysical(t, f, raw, 1)
	require.Zero(t, p.terminate(t), p.output.String())
}
