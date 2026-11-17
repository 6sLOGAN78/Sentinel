package alertstream_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/alertstream"
	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/retry"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

const (
	unitSecret     = "unit-test-secret-unit-test-secret-0123456789"
	refusedDSN     = "postgres://unit@127.0.0.1:1/unit?sslmode=disable"
	recordedWaits  = 8
	jitterFraction = 0.95
	jitterProbes   = 4
	attemptGap     = 5 * time.Millisecond
)

// streamBackoff is the 100 ms..5 s doubling policy the stream loops must keep.
func streamBackoff() retry.Policy {
	return retry.Policy{Min: 100 * time.Millisecond, Max: 5 * time.Second, Attempts: 1}
}

// fakePostgres accepts TCP connections and either holds them silently (so a
// stream stays admitted inside its first durable read) or closes them at once
// (so every connect attempt fails fast). It records accept times.
type fakePostgres struct {
	listener net.Listener
	hold     bool
	done     chan struct{}
	mu       sync.Mutex
	conns    []net.Conn
	accepted []time.Time
}

func startFakePostgres(t *testing.T, hold bool) *fakePostgres {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakePostgres{listener: listener, hold: hold}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			f.mu.Lock()
			f.accepted = append(f.accepted, time.Now())
			if hold {
				f.conns = append(f.conns, conn)
			} else {
				_ = conn.Close()
			}
			f.mu.Unlock()
		}
	}()
	f.done = done
	return f
}

// close stops accepting and drops held connections. Register it after the
// pool's cleanup so it runs first and unblocks pending connects.
func (f *fakePostgres) close() {
	_ = f.listener.Close()
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, conn := range f.conns {
		_ = conn.Close()
	}
}

func (f *fakePostgres) dsn() string {
	return "postgres://unit@" + f.listener.Addr().String() + "/unit?sslmode=disable&connect_timeout=10"
}

func (f *fakePostgres) acceptTimes() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.accepted...)
}

func newPool(t *testing.T, dsn string, server *fakePostgres) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	if server != nil {
		t.Cleanup(server.close)
	}
	return pool
}

func mintToken(t *testing.T) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "analyst-1", "tenant": "tenant-a", "role": "analyst",
		"iss": "sentinel-dev", "aud": "sentinel-dashboard",
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
	})
	signed, err := token.SignedString([]byte(unitSecret))
	require.NoError(t, err)
	return signed
}

type streamFixture struct {
	handler *alertstream.Handler
	server  *httptest.Server
	reg     *prometheus.Registry
	token   string
}

func newStreamFixture(t *testing.T, settings config.StreamConfig) *streamFixture {
	t.Helper()
	reg := telemetry.NewRegistry()
	silent := startFakePostgres(t, true)
	store := alertstream.NewStore(newPool(t, silent.dsn(), silent))
	handler := alertstream.NewHandler(context.Background(), store, settings).WithMetrics(telemetry.NewSSEMetrics(reg))
	t.Cleanup(func() { _ = handler.Close(context.Background()) })
	verifier, err := auth.NewVerifier([]byte(unitSecret), "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	e := echo.New()
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		status := http.StatusInternalServerError
		var httpErr *errs.HTTPError
		if errors.As(err, &httpErr) {
			status = httpErr.Status
		}
		_ = c.NoContent(status)
	}
	handler.Register(e, verifier)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)
	return &streamFixture{handler: handler, server: server, reg: reg, token: mintToken(t)}
}

type streamResult struct {
	status int
	err    error
}

// open starts one stream request; the returned cancel disconnects the client.
func (f *streamFixture) open(t *testing.T) (<-chan streamResult, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.server.URL+"/api/v1/alerts/stream", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+f.token)
	result := make(chan streamResult, 1)
	go func() {
		resp, doErr := f.server.Client().Do(req)
		if doErr != nil {
			result <- streamResult{err: doErr}
			return
		}
		_ = resp.Body.Close()
		result <- streamResult{status: resp.StatusCode}
	}()
	return result, cancel
}

func sseClients(t *testing.T, reg prometheus.Gatherer) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == "sentinel_sse_clients" {
			require.Len(t, family.GetMetric(), 1)
			return family.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatal("sentinel_sse_clients is not registered")
	return 0
}

func requireClientsEventually(t *testing.T, reg prometheus.Gatherer, want float64) {
	t.Helper()
	require.Eventually(t, func() bool { return sseClients(t, reg) == want }, 3*time.Second, 5*time.Millisecond,
		"sentinel_sse_clients never reached %v", want)
}

func defaultStreamSettings(clients int, lifetime time.Duration) config.StreamConfig {
	return config.StreamConfig{Clients: clients, Queue: 32, WriteTimeout: 5 * time.Second, Lifetime: lifetime}
}

func TestStreamClientsGaugeTracksAdmittedStreams(t *testing.T) {
	f := newStreamFixture(t, defaultStreamSettings(3, 10*time.Minute))
	require.InDelta(t, 0, sseClients(t, f.reg), 0)

	cancels := make([]context.CancelFunc, 0, 3)
	results := make([]<-chan streamResult, 0, 3)
	for range 3 {
		result, cancel := f.open(t)
		results = append(results, result)
		cancels = append(cancels, cancel)
	}
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	requireClientsEventually(t, f.reg, 3)

	// The admission cap is full: the fourth stream is refused and not counted.
	rejected, cancelRejected := f.open(t)
	defer cancelRejected()
	got := <-rejected
	require.NoError(t, got.err)
	require.Equal(t, http.StatusTooManyRequests, got.status)
	require.InDelta(t, 3, sseClients(t, f.reg), 0, "a rejected admission must not change the gauge")

	cancels[0]()
	cancels[1]()
	<-results[0]
	<-results[1]
	requireClientsEventually(t, f.reg, 1)

	// Close joins every admitted handler, so the gauge is 0 when it returns.
	require.NoError(t, f.handler.Close(t.Context()))
	require.InDelta(t, 0, sseClients(t, f.reg), 0, "handler shutdown must release every counted stream")
	<-results[2]

	// After shutdown admission is refused and the gauge stays at 0.
	closed, cancelClosed := f.open(t)
	defer cancelClosed()
	got = <-closed
	require.NoError(t, got.err)
	require.Equal(t, http.StatusTooManyRequests, got.status)
	require.InDelta(t, 0, sseClients(t, f.reg), 0)
}

func TestStreamClientsGaugeReleasedWhenLifetimeEnds(t *testing.T) {
	f := newStreamFixture(t, defaultStreamSettings(3, 300*time.Millisecond))
	result, cancel := f.open(t)
	defer cancel()
	requireClientsEventually(t, f.reg, 1)
	select {
	case <-result:
	case <-time.After(4 * time.Second):
		t.Fatal("stream outlived its configured lifetime")
	}
	requireClientsEventually(t, f.reg, 0)
}

// ceilingRecorder replaces the jittered wait and cancels after limit waits.
type ceilingRecorder struct {
	mu       sync.Mutex
	ceilings []time.Duration
	limit    int
	cancel   context.CancelFunc
}

func (r *ceilingRecorder) wait(ctx context.Context, ceiling time.Duration) error {
	r.mu.Lock()
	r.ceilings = append(r.ceilings, ceiling)
	n := len(r.ceilings)
	r.mu.Unlock()
	if n >= r.limit {
		r.cancel()
		return ctx.Err()
	}
	return nil
}

func (r *ceilingRecorder) recorded() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.ceilings...)
}

func expectedCeilings() []time.Duration {
	want := make([]time.Duration, 0, recordedWaits)
	for attempt := 1; attempt <= recordedWaits; attempt++ {
		want = append(want, streamBackoff().Ceiling(attempt))
	}
	return want
}

func TestStreamBackoffPolicyIsHundredMillisToFiveSeconds(t *testing.T) {
	ms := time.Millisecond
	require.Equal(t, []time.Duration{100 * ms, 200 * ms, 400 * ms, 800 * ms, 1600 * ms, 3200 * ms,
		5000 * ms, 5000 * ms}, expectedCeilings())
}

func runJoined(t *testing.T, run func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- run() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not return after the waiter canceled it")
		return nil
	}
}

func TestListenerReconnectUsesInjectedJitteredWaiter(t *testing.T) {
	h := alertstream.NewHandler(t.Context(), nil)
	defer func() { require.NoError(t, h.Close(t.Context())) }()
	listener, err := alertstream.NewListener(refusedDSN, h)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	recorder := &ceilingRecorder{limit: recordedWaits, cancel: cancel}
	listener = listener.WithRetryWaiter(recorder.wait)
	require.NoError(t, runJoined(t, func() error { return listener.Run(ctx) }))
	require.Equal(t, expectedCeilings(), recorder.recorded())
	require.Error(t, listener.Ready(t.Context()))
}

func TestDeliveryProjectorBackoffUsesInjectedJitteredWaiter(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	recorder := &ceilingRecorder{limit: recordedWaits, cancel: cancel}
	store := alertstream.NewStore(newPool(t, refusedDSN, nil)).WithRetryWaiter(recorder.wait)
	err := runJoined(t, func() error { return store.Run(ctx) })
	if err != nil {
		require.ErrorIs(t, err, context.Canceled)
	}
	require.Equal(t, expectedCeilings(), recorder.recorded())
}

// attemptStarts groups accept times into connect attempts. pgx v5.11 opens a
// second TCP connection within a millisecond when the first is reset, so
// accepts closer than attemptGap belong to the same attempt. Grouping can only
// hide a short jittered interval, never invent one: under a fixed backoff every
// group interval is still at least its ceiling.
func attemptStarts(accepted []time.Time) []time.Time {
	starts := make([]time.Time, 0, len(accepted))
	for i, at := range accepted {
		if i == 0 || at.Sub(accepted[i-1]) >= attemptGap {
			starts = append(starts, at)
		}
	}
	return starts
}

// requireJitteredIntervals proves the default waiter is not a fixed sleep. A
// fixed backoff makes every reconnect interval at least its ceiling. With full
// jitter each interval is uniform in [0, ceiling], so the chance that all
// probed intervals stay above 95% of their ceiling is about 0.05^4 (6e-6).
func requireJitteredIntervals(t *testing.T, server *fakePostgres) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		starts := attemptStarts(server.acceptTimes())
		for k := 1; k < len(starts) && k <= jitterProbes; k++ {
			interval := starts[k].Sub(starts[k-1])
			if float64(interval) < jitterFraction*float64(streamBackoff().Ceiling(k)) {
				return
			}
		}
		if len(starts) > jitterProbes {
			break
		}
		time.Sleep(time.Millisecond)
	}
	starts := attemptStarts(server.acceptTimes())
	intervals := make([]string, 0, len(starts))
	for k := 1; k < len(starts); k++ {
		intervals = append(intervals, strconv.FormatInt(starts[k].Sub(starts[k-1]).Milliseconds(), 10)+"ms")
	}
	t.Fatalf("every reconnect waited its full ceiling (fixed backoff): %s", strings.Join(intervals, ", "))
}

func TestListenerDefaultWaiterIsJittered(t *testing.T) {
	server := startFakePostgres(t, false)
	t.Cleanup(server.close)
	h := alertstream.NewHandler(t.Context(), nil)
	defer func() { require.NoError(t, h.Close(t.Context())) }()
	listener, err := alertstream.NewListener(server.dsn(), h)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()
	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()
	requireJitteredIntervals(t, server)
}

func TestDeliveryProjectorDefaultWaiterIsJittered(t *testing.T) {
	server := startFakePostgres(t, false)
	store := alertstream.NewStore(newPool(t, server.dsn(), server))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- store.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	}()
	requireJitteredIntervals(t, server)
}
