//go:build phase4

package testphase4_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

type streamDelivery struct {
	Seq   string              `json:"seq"`
	Alert query.WorkflowAlert `json:"alert"`
}

func streamRequest(ctx context.Context, t *testing.T, base, token, after, suffix string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/alerts/stream"+suffix, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if after != "" {
		req.Header.Set("Last-Event-ID", after)
	}
	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	return resp
}

func streamRead(t *testing.T, response *http.Response, count int) []streamDelivery {
	t.Helper()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var items []streamDelivery
	var eventID string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			eventID = strings.TrimPrefix(line, "id: ")
		}
		if strings.HasPrefix(line, "data: ") {
			var item streamDelivery
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &item))
			require.Equal(t, eventID, item.Seq)
			items = append(items, item)
			if len(items) == count {
				return items
			}
		}
	}
	require.NoError(t, scanner.Err())
	require.Len(t, items, count)
	return items
}

func TestPhase4Stream(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	key := make([]byte, 48)
	_, err = rand.Read(key)
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(key)
	t.Setenv("SENTINEL_AUTH.JWT_SECRET", secret)
	// An ordinary finite HTTP deadline remains enabled, and the real route must
	// override it locally while idle (heartbeat is deliberately later than 2s).
	t.Setenv("SENTINEL_SERVER.WRITE_TIMEOUT", "2")
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 9*time.Minute)
	defer cancel()
	owned := deliveryDatabase(ctx, t, cfg.Database, 8)
	t.Setenv("SENTINEL_DATABASE.NAME", owned.Name)
	db, err := database.New(ctx, owned, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir, build.Stdout, build.Stderr = filepath.Join(root, "backend"), io.Discard, io.Discard
	require.NoError(t, build.Run(), "production binary build is setup, not RED")
	a, _ := startQueryAPI(t, binary, root, secret, freeAddress(t))
	b, _ := startQueryAPI(t, binary, root, secret, freeAddress(t))
	tenant := "p4stream-" + uuid.NewString()
	token := func(expiry time.Time) string {
		return signToken(t, map[string]any{"iss": "sentinel-dev", "aud": "sentinel-dashboard",
			"sub": "stream-reader", "tenant": tenant, "role": "viewer",
			"iat": time.Now().Add(-2 * time.Minute).Unix(), "nbf": time.Now().Add(-2 * time.Minute).Unix(),
			"exp": expiry.Unix()}, secret, "HS256")
	}
	credential := token(time.Now().Add(10 * time.Minute))
	probe := streamRequest(ctx, t, a, credential, "0", "")
	status := probe.StatusCode
	_ = probe.Body.Close()
	require.Equal(t, http.StatusOK, status,
		"existing production API must implement authenticated SSE, not dispatch stream as alert ID")
	t.Run("auth_query_and_cursor_fail_before_stream_headers", func(t *testing.T) {
		for _, tc := range []struct {
			token, after, suffix string
			status               int
		}{
			{"", "", "", 401}, {"invalid", "", "", 401}, {token(time.Now().Add(-time.Minute)), "", "", 401},
			{credential, "", "?token=forbidden", 400}, {credential, "01", "", 400},
			{credential, "9223372036854775808", "", 400}, {credential, "9223372036854775807", "", 400},
		} {
			response := streamRequest(ctx, t, a, tc.token, tc.after, tc.suffix)
			require.Equal(t, tc.status, response.StatusCode)
			require.NotContains(t, response.Header.Get("Content-Type"), "text/event-stream")
			_ = response.Body.Close()
		}
	})
	t.Run("two_actual_replicas_poll_every_own_commit_and_reconnect", func(t *testing.T) {
		// The REST response is a real barrier. This held transaction commits
		// strictly AFTER that response and BEFORE either stream attaches.
		code, raw := getJSON(t, a+"/api/v1/alerts/backfill?after_seq=0&limit=1", credential, "")
		require.Equal(t, 200, code)
		var page deliveryPage
		require.NoError(t, json.Unmarshal(raw, &page))
		tx, e := db.Pool.Begin(ctx)
		require.NoError(t, e)
		first := deliveryInsert(ctx, t, tx, tenant)
		require.NoError(t, tx.Commit(ctx))
		foreign := deliveryInsert(ctx, t, db.Pool, "stream-foreign")
		streams := []*http.Response{
			streamRequest(ctx, t, a, credential, page.Next, ""), streamRequest(ctx, t, b, credential, page.Next, ""),
		}
		for _, response := range streams {
			require.Equal(t, 200, response.StatusCode)
			require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
			require.Equal(t, "no-cache, no-transform", response.Header.Get("Cache-Control"))
			require.Equal(t, "no", response.Header.Get("X-Accel-Buffering"))
		}
		seen := make([][]streamDelivery, 2)
		for i, response := range streams {
			seen[i] = streamRead(t, response, 1)
			require.Equal(t, first, seen[i][0].Alert.AlertID)
		}
		// After the first frame, new commits race the continuing replay/poll.
		var expected = []string{first}
		for range 205 {
			expected = append(expected, deliveryInsert(ctx, t, db.Pool, tenant))
		}
		for i, response := range streams {
			seen[i] = append(seen[i], streamRead(t, response, 205)...)
			streamAssertIDs(t, seen[i], expected, tenant, foreign)
			_ = response.Body.Close()
		}
		last := seen[0][len(seen[0])-1].Seq
		disconnected := deliveryInsert(ctx, t, db.Pool, tenant)
		response := streamRequest(ctx, t, b, credential, last, "")
		defer response.Body.Close()
		require.Equal(t, disconnected, streamRead(t, response, 1)[0].Alert.AlertID)
	})
	t.Run("route_survives_ordinary_write_timeout_and_heartbeats", func(t *testing.T) {
		response := streamRequest(ctx, t, a, credential, "0", "")
		defer response.Body.Close()
		streamWaitHeartbeat(t, response)
	})
	t.Run("token_expiry_closes_even_with_auth_leeway", func(t *testing.T) {
		response := streamRequest(ctx, t, a, token(time.Now().Add(2*time.Second)), "0", "")
		defer response.Body.Close()
		started := time.Now()
		_, err = io.Copy(io.Discard, response.Body)
		require.NoError(t, err)
		require.Less(t, time.Since(started), 4*time.Second)
	})
	t.Run("blocked_actual_socket_write_is_bounded", func(t *testing.T) {
		streamSlowClient(ctx, t, a, credential, tenant, db)
	})
	t.Run("hundred_client_admission_and_release", func(t *testing.T) {
		var after string
		require.NoError(t, db.Pool.QueryRow(ctx,
			"SELECT next_seq::text FROM alert_delivery_counter WHERE singleton").Scan(&after))
		var streams []*http.Response
		defer func() {
			for _, response := range streams {
				_ = response.Body.Close()
			}
		}()
		for range 100 {
			response := streamRequest(ctx, t, a, credential, after, "")
			require.Equal(t, 200, response.StatusCode)
			streams = append(streams, response)
		}
		response := streamRequest(ctx, t, a, credential, after, "")
		require.Equal(t, 429, response.StatusCode)
		_ = response.Body.Close()
		for _, response = range streams {
			_ = response.Body.Close()
		}
		require.Eventually(t, func() bool {
			response = streamRequest(ctx, t, a, credential, after, "")
			defer response.Body.Close()
			return response.StatusCode == http.StatusOK
		}, 3*time.Second, 25*time.Millisecond)
	})
}

func streamAssertIDs(t *testing.T, seen []streamDelivery, expected []string, tenant, foreign string) {
	t.Helper()
	ids := make([]string, 0, len(seen))
	for _, item := range seen {
		require.Equal(t, tenant, item.Alert.TenantID)
		require.NotEqual(t, foreign, item.Alert.AlertID)
		ids = append(ids, item.Alert.AlertID)
	}
	sort.Strings(ids)
	sorted := append([]string(nil), expected...)
	sort.Strings(sorted)
	require.Equal(t, sorted, ids, "EACH replica receives ALL own IDs")
}

func streamWaitHeartbeat(t *testing.T, response *http.Response) {
	t.Helper()
	scanner := bufio.NewScanner(response.Body)
	deadline := time.Now().Add(18 * time.Second)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), ": heartbeat") {
			require.True(t, time.Now().Before(deadline))
			return
		}
	}
	t.Fatalf("real stream ended before heartbeat beyond ordinary 2s deadline: %v", scanner.Err())
}

func streamSlowClient(ctx context.Context, t *testing.T, base, credential, tenant string, db *database.Database) {
	t.Helper()
	// Small receive window and a real unread TCP socket force a blocked write.
	address := strings.TrimPrefix(base, "http://")
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	require.NoError(t, err)
	defer conn.Close()
	tcp, ok := conn.(*net.TCPConn)
	require.True(t, ok)
	require.NoError(t, tcp.SetReadBuffer(1024))
	_, err = io.WriteString(conn, "GET /api/v1/alerts/stream HTTP/1.1\r\nHost: "+address+
		"\r\nAuthorization: Bearer "+credential+"\r\nConnection: close\r\n\r\n")
	require.NoError(t, err)
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, status, "200")
	// Insert only newly owned test rows; no shared workflow/counter mutation.
	_, err = db.Pool.Exec(ctx, `INSERT INTO alert_workflows
	 (alert_id,tenant_id,created_at,updated_at,rule_id,rule_version,severity,entity_key,
	 summary,event_count,window_start,window_end,immutable_fingerprint)
	 SELECT md5($1||n::text)||md5(n::text||$1),$2,now(),now(),'AUTH_BRUTE_FORCE',1,'high','192.0.2.1',
	 repeat('x',100000),1,now()-interval '1 minute',now(),md5($1||n::text)||md5(n::text||$1)
	 FROM generate_series(1,100)n`, uuid.NewString(), tenant)
	require.NoError(t, err)
	// Projection polls <=1s. Allow that plus the actual 5s write deadline.
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal("slow-client context ended")
	case <-timer.C:
	}
	// A TCP FIN may wait behind unread kernel-buffered data. The externally
	// observable admission barrier proves that the blocked handler released its
	// resource within the budget, without confusing network draining with it.
	var after string
	require.NoError(t, db.Pool.QueryRow(ctx,
		"SELECT next_seq::text FROM alert_delivery_counter WHERE singleton").Scan(&after))
	var admitted []*http.Response
	defer func() {
		for _, response := range admitted {
			_ = response.Body.Close()
		}
	}()
	for range 100 {
		response := streamRequest(ctx, t, base, credential, after, "")
		admitted = append(admitted, response)
		require.Equal(t, http.StatusOK, response.StatusCode, "unread slow socket must have released admission by now")
	}
}
