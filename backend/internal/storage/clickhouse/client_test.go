package clickhouse_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

// silentServer accepts TCP connections and never answers, like a paused
// ClickHouse whose port is still published. Connections stay open until cleanup.
func silentServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var held []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range held {
			_ = conn.Close()
		}
	})

	return listener.Addr().String()
}

// A new connection's handshake must stay inside the caller's deadline. The
// driver alone bounds it by its 5 s dial timeout, so an operation timeout
// shorter than that was overrun tenfold against a paused server.
func TestOpenHonorsCallerDeadlineDuringHandshake(t *testing.T) {
	t.Parallel()

	const deadline = 300 * time.Millisecond
	const ceiling = 2 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), deadline)
	defer cancel()
	started := time.Now()
	conn, err := clickhouse.Open(ctx, config.ClickHouseConfig{
		NativeAddress: silentServer(t), Database: "sentinel", User: "sentinel",
	})
	elapsed := time.Since(started)
	if conn != nil {
		_ = conn.Close()
	}
	require.Error(t, err)
	require.Less(t, elapsed, ceiling, "handshake against a silent server ignored the %s deadline", deadline)
	var netErr net.Error
	if errors.As(err, &netErr) {
		require.True(t, netErr.Timeout(), "expected a timeout, got %v", err)
	}
}
