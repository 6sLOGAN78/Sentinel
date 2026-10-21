//go:build phase4

package testphase4_test

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
)

// This family deliberately runs the real command, not a copied test router.
// Fixtures use unique synthetic tenants in the preserved local PG database.
func TestPhase4Auth(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Setenv("SENTINEL_PRIMARY.ENV", "local")
	t.Setenv("SENTINEL_ENV_FILE", filepath.Join(root, ".env"))
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	require.NoError(t, database.Migrate(ctx, cfg.Database, zerolog.Nop()), "apply actual PG migrations")
	db, err := database.New(ctx, cfg.Database, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	tenantA, tenantB := "p4a-"+uuid.NewString(), "p4b-"+uuid.NewString()
	now := time.Now().UTC().Truncate(time.Millisecond)
	ids := make([]string, 0, 56)
	for i := range 56 {
		tenant := tenantA
		if i == 55 {
			tenant = tenantB
		}
		digest := sha256.Sum256([]byte(tenant + strconv.Itoa(i)))
		id := hex.EncodeToString(digest[:])
		ids = append(ids, id)
		_, err = db.Pool.Exec(ctx, `INSERT INTO alert_workflows
			(alert_id,tenant_id,created_at,updated_at,rule_id,rule_version,severity,entity_key,summary,event_count,window_start,window_end,immutable_fingerprint)
			VALUES ($1,$2,$3,$3,'AUTH_BRUTE_FORCE',1,'high','192.0.2.10','Native authentication fixture',10,$4,$3,$1)`,
			id, tenant, now.Add(time.Duration(i)*time.Millisecond), now.Add(-time.Minute))
		require.NoError(t, err)
	}
	key, err := apikeys.NewStore(db.Pool).Create(ctx, tenantA, []string{apikeys.ScopeRead}, "phase4 fixture")
	require.NoError(t, err)
	secret := make([]byte, 48)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	secretText := base64.RawURLEncoding.EncodeToString(secret)
	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir = filepath.Join(root, "backend")
	build.Stdout, build.Stderr = io.Discard, io.Discard
	require.NoError(t, build.Run(), "build actual query-api before behavioral assertions")
	base, _ := startQueryAPI(t, binary, root, secretText, "")
	claims := func(tenant string) map[string]any {
		return map[string]any{
			"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "operator", "tenant": tenant,
			"role": "analyst", "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
		}
	}
	token := signToken(t, claims(tenantA), secretText, "HS256")
	t.Run("verified_session", func(t *testing.T) {
		status, body := getJSON(t, base+"/api/v1/session", token, "")
		require.Equal(t, 200, status, "signed identity must reach production session handler")
		require.JSONEq(t, `{"subject":"operator","tenant_id":"`+tenantA+`","role":"analyst","expires_at":"`+now.Add(10*time.Minute).Truncate(time.Second).Format(time.RFC3339)+`"}`, string(body))
	})
	t.Run("own_pg_workflows_only", func(t *testing.T) {
		status, body := getJSON(t, base+"/api/v1/alerts", token, "")
		require.Equal(t, 200, status)
		var page struct {
			Items      []map[string]any `json:"items"`
			HasMore    bool             `json:"has_more"`
			NextCursor *string          `json:"next_cursor"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		require.Len(t, page.Items, 50)
		require.True(t, page.HasMore)
		require.NotNil(t, page.NextCursor)
		for _, row := range page.Items {
			require.Equal(t, tenantA, row["tenant_id"])
			require.Len(t, row, 16, "only actual public PG workflow columns")
			for _, absent := range []string{"sample_event_ids", "dedupe_key", "schema_version", "seq", "immutable_fingerprint"} {
				require.NotContains(t, row, absent)
			}
		}
		status, body = getJSON(t, base+"/api/v1/alerts?limit=200", signToken(t, claims(tenantB), secretText, "HS256"), "")
		require.Equal(t, 200, status)
		require.NoError(t, json.Unmarshal(body, &page))
		require.Len(t, page.Items, 1)
		require.Equal(t, ids[55], page.Items[0]["alert_id"])
		require.False(t, page.HasMore)
		for _, query := range []string{"?limit=201", "?limit=0", "?tenant_id=" + tenantB} {
			status, _ = getJSON(t, base+"/api/v1/alerts"+query, token, "")
			require.Equal(t, 400, status)
		}
	})
	for _, name := range []string{"missing_token", "bad_signature", "wrong_algorithm", "expired", "oversized", "iss", "aud", "sub", "tenant", "role", "exp", "iat", "nbf"} {
		t.Run("reject_"+name, func(t *testing.T) {
			c := claims(tenantA)
			algorithm, signingKey := "HS256", secretText
			switch name {
			case "bad_signature":
				signingKey += "x"
			case "wrong_algorithm":
				algorithm = "HS384"
			case "expired":
				c["exp"] = now.Add(-time.Minute).Unix()
			default:
				delete(c, name)
			}
			raw := signToken(t, c, signingKey, algorithm)
			if name == "missing_token" {
				raw = ""
			}
			if name == "oversized" {
				raw = strings.Repeat("x", 8193)
			}
			for _, path := range []string{"/api/v1/session", "/api/v1/alerts"} {
				status, body := getJSON(t, base+path, raw, key)
				require.Equal(t, 401, status, "invalid bearer/key must not authorize dashboard")
				require.NotContains(t, string(body), raw+".")
			}
		})
	}
	t.Run("legacy_key_not_broadened_or_fallback", func(t *testing.T) {
		status, _ := getJSON(t, base+"/api/v1/events/"+uuid.NewString(), "", key)
		require.Equal(t, 404, status, "legacy read key still reaches real event lookup")
		status, _ = getJSON(t, base+"/api/v1/events/"+uuid.NewString(), "invalid", key)
		require.Equal(t, 401, status, "present invalid bearer must not fall back to read key")
	})
	t.Run("pg_reads_with_clickhouse_unreachable", func(t *testing.T) {
		base, ops := startQueryAPI(t, binary, root, secretText, freeAddress(t))
		for _, path := range []string{"/api/v1/session", "/api/v1/alerts"} {
			status, _ := getJSON(t, base+path, token, "")
			require.Equal(t, 200, status, "PG slice must work without ClickHouse")
		}
		status, _ := getJSON(t, ops+"/readyz", "", "")
		require.Equal(t, 503, status, "readiness must report actual unreachable dependency")
	})
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := l.Addr().String()
	require.NoError(t, l.Close())
	return address
}

func startQueryAPI(t *testing.T, binary, root, secret, chAddress string) (string, string) {
	t.Helper()
	httpAddress, opsAddress := freeAddress(t), freeAddress(t)
	_, httpPort, _ := net.SplitHostPort(httpAddress)
	_, opsPort, _ := net.SplitHostPort(opsAddress)
	cmd := exec.Command(binary)
	cmd.Dir = filepath.Join(root, "backend")
	env := map[string]string{"SENTINEL_SERVER.BIND_ADDRESS": "127.0.0.1", "SENTINEL_SERVER.PORT": httpPort,
		"SENTINEL_OPS.BIND_ADDRESS": "127.0.0.1", "SENTINEL_OPS.PORT": opsPort, "SENTINEL_AUTH.JWT_SECRET": secret}
	if chAddress != "" {
		env["SENTINEL_CLICKHOUSE.NATIVE_ADDRESS"] = chAddress
	}
	for _, entry := range os.Environ() {
		k, _, _ := strings.Cut(entry, "=")
		if _, overridden := env[k]; !overridden {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("owned query-api process did not join")
			}
		}
	})
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + opsAddress + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return "http://" + httpAddress, "http://" + opsAddress
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("production query-api did not become live within 15s")
	return "", ""
}

func signToken(t *testing.T, claims map[string]any, secret, algorithm string) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": algorithm, "typ": "JWT"})
	require.NoError(t, err)
	body, err := json.Marshal(claims)
	require.NoError(t, err)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, err = mac.Write([]byte(unsigned))
	require.NoError(t, err)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func getJSON(t *testing.T, url, token, key string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	require.NoError(t, err)
	return resp.StatusCode, body
}
