//go:build phase4

package testphase4_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// The real command must load the configured file once, before opening listeners.
func TestPhase4SummaryRules(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	material := make([]byte, 48)
	_, err = rand.Read(material)
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(material)
	t.Setenv("SENTINEL_AUTH.JWT_SECRET", secret)
	t.Setenv("SENTINEL_PRIMARY.ENV", "local")
	t.Setenv("SENTINEL_ENV_FILE", filepath.Join(root, ".env"))
	raw, err := os.ReadFile(filepath.Join(root, "config/rules.yaml"))
	require.NoError(t, err)
	updated := strings.ReplaceAll(string(raw), "version: 1\n    enabled:", "version: 12\n    enabled:")
	updated = strings.ReplaceAll(updated, "threshold: 10", "threshold: 17")
	raw = []byte(strings.ReplaceAll(updated, "window: 300s", "window: 301.5s"))
	raw = []byte(strings.Replace(string(raw), "enabled: true", "enabled: false", 1))
	path := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	t.Setenv("SENTINEL_DETECTOR.RULES_PATH", path)
	definitions, err := detection.LoadRules(strings.NewReader(string(raw)))
	require.NoError(t, err, "valid fixture is setup, not RED")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir, build.Stdout, build.Stderr = filepath.Join(root, "backend"), io.Discard, io.Discard
	require.NoError(t, build.Run(), "build must pass before behavioral RED")
	base, _ := startQueryAPI(t, binary, root, secret, "")
	tenant := "p4rules-" + uuid.NewString()
	tokenFor := func(role string) string {
		now := time.Now()
		return signToken(t, map[string]any{
			"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "rules-operator", "tenant": tenant,
			"role": role, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix()}, secret, "HS256")
	}
	t.Run("summary_actual_binary_authenticated_route", func(t *testing.T) {
		status, _ := getJSON(t, base+"/api/v1/metrics/summary", "", "")
		require.Equal(t, 401, status, "summary must require a verified principal")
		status, body := getJSON(t, base+"/api/v1/metrics/summary", tokenFor("viewer"), "")
		require.Equal(t, 200, status, "actual binary must register the native summary adapters")
		var summary struct {
			Rate  float64                          `json:"event_rate_per_sec"`
			Open  int64                            `json:"open_alerts"`
			Trend []struct{ Events, Alerts int64 } `json:"trend_24h"`
		}
		require.NoError(t, json.Unmarshal(body, &summary))
		require.Zero(t, summary.Rate)
		require.Zero(t, summary.Open)
		require.Len(t, summary.Trend, 288)
	})
	read := func(t *testing.T, url, token string) []byte {
		t.Helper()
		status, body := getJSON(t, url+"/api/v1/rules", token, "")
		require.Equal(t, 200, status, "configured startup rules must be available on the actual binary")
		return body
	}
	for _, role := range []string{"viewer", "analyst", "admin"} {
		t.Run("exact_startup_definitions_"+role, func(t *testing.T) {
			var page struct {
				Rules []struct {
					ID          string `json:"rule_id"`
					Version     int    `json:"version"`
					Enabled     bool   `json:"enabled"`
					Name        string `json:"name"`
					Description string `json:"description"`
					Severity    string `json:"severity"`
					Parameters  struct {
						Threshold  int   `json:"threshold"`
						WindowMS   int64 `json:"window_ms"`
						CooldownMS int64 `json:"cooldown_ms"`
					} `json:"parameters"`
				} `json:"rules"`
			}
			require.NoError(t, json.Unmarshal(read(t, base, tokenFor(role)), &page))
			require.Len(t, page.Rules, 4)
			for i, want := range definitions.Rules {
				got := page.Rules[i]
				require.Equal(t, want.ID, got.ID)
				require.Equal(t, want.Version, got.Version)
				require.Equal(t, want.Enabled, got.Enabled)
				require.Equal(t, want.Severity, got.Severity)
				require.Equal(t, want.Threshold, got.Parameters.Threshold)
				require.Equal(t, want.Window.Milliseconds(), got.Parameters.WindowMS)
				require.Equal(t, want.Cooldown.Milliseconds(), got.Parameters.CooldownMS)
				require.NotEmpty(t, got.Name)
				require.NotEmpty(t, got.Description)
			}
		})
	}
	t.Run("unauthenticated_and_read_only", func(t *testing.T) {
		status, _ := getJSON(t, base+"/api/v1/rules", "", "")
		require.Equal(t, 401, status)
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			req, requestErr := http.NewRequestWithContext(ctx, method,
				base+"/api/v1/rules", strings.NewReader(`{"enabled":true}`))
			require.NoError(t, requestErr)
			req.Header.Set("Authorization", "Bearer "+tokenFor("admin"))
			response, requestErr := (&http.Client{Timeout: 5 * time.Second}).Do(req)
			require.NoError(t, requestErr)
			require.NoError(t, response.Body.Close())
			require.Equal(t, 405, response.StatusCode)
		}
	})
	t.Run("immutable_until_restart", func(t *testing.T) {
		before := read(t, base, tokenFor("viewer"))
		changed := strings.ReplaceAll(string(raw), "version: 12", "version: 13")
		require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
		require.JSONEq(t, string(before), string(read(t, base, tokenFor("viewer"))))
		restarted, _ := startQueryAPI(t, binary, root, secret, "")
		require.Contains(t, string(read(t, restarted, tokenFor("viewer"))), `"version":13`)
		require.NoError(t, os.Remove(path))
		require.JSONEq(t, string(before), string(read(t, base, tokenFor("viewer"))))
	})
	t.Run("retained_version_is_not_current_registry", func(t *testing.T) {
		cfg, loadErr := config.Load(config.ServiceQueryAPI)
		require.NoError(t, loadErr)
		db, openErr := database.New(ctx, cfg.Database, zerolog.Nop())
		require.NoError(t, openErr)
		defer db.Close()
		history := *definitions
		history.Rules = append([]detection.Rule(nil), definitions.Rules...)
		for i := range history.Rules {
			history.Rules[i].Version = 77
			history.Rules[i].Threshold = 13
		}
		manifest, marshalErr := json.Marshal(history)
		require.NoError(t, marshalErr)
		group, id := "p4rules-"+uuid.NewString(), strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
		fingerprint := history.Fingerprint()
		_, insertErr := db.Pool.Exec(ctx, `INSERT INTO detection_partition_fences
		(group_id,topic,partition_id,incarnation,member_id,generation,assignment_token,
		config_fingerprint,rules_manifest)
		VALUES ($1,'phase4-rules',0,$2,'fixture',1,1,$3,$4)`, group, strings.Repeat("1", 64), fingerprint, manifest)
		require.NoError(t, insertErr)
		_, insertErr = db.Pool.Exec(ctx, `INSERT INTO detection_decisions
		(alert_id,group_id,tenant_id,rule_id,rule_version,entity_components,config_fingerprint,
		allowed,trigger_occurred_at,effective_clock,topic,partition_id,source_offset,rule_ordinal,
		canonical_payload,immutable_fingerprint,decision_generation)
		VALUES ($1,$2,$3,'AUTH_BRUTE_FORCE',77,'[]',$4,true,now(),now(),
		'phase4-rules',0,0,0,'{}',$1,1)`, id, group, tenant, fingerprint)
		require.NoError(t, insertErr)
		explanation, explanationErr := storagepostgres.NewAlertStore(db.Pool, 5*time.Second).
			RuleExplanation(ctx, tenant, id, "AUTH_BRUTE_FORCE", 77)
		require.NoError(t, explanationErr)
		require.Contains(t, explanation, "threshold 13")
		body := string(read(t, base, tokenFor("viewer")))
		require.Contains(t, body, `"version":12`)
		require.NotContains(t, body, `"version":77`)
	})
	verifyRulesFailClosed(ctx, t, binary, root, path, raw)
}

func verifyRulesFailClosed(ctx context.Context, t *testing.T, binary, root, path string, raw []byte) {
	t.Helper()
	for _, fixture := range []struct{ name, body string }{
		{"malformed", "rules: ["},
		{"invalid_settings", strings.Replace(string(raw), "threshold: 17", "threshold: 0", 1)},
		{"missing", ""},
	} {
		t.Run("fail_closed_startup_"+fixture.name, func(t *testing.T) {
			if fixture.body != "" {
				require.NoError(t, os.WriteFile(path, []byte(fixture.body), 0o600))
			} else {
				_ = os.Remove(path)
			}
			httpAddress, opsAddress := freeAddress(t), freeAddress(t)
			_, hp, _ := strings.Cut(httpAddress, ":")
			_, op, _ := strings.Cut(opsAddress, ":")
			childCtx, stop := context.WithTimeout(ctx, 8*time.Second)
			defer stop()
			cmd := exec.CommandContext(childCtx, binary)
			cmd.Dir = filepath.Join(root, "backend")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "SENTINEL_SERVER.PORT=") && !strings.HasPrefix(entry, "SENTINEL_OPS.PORT=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "SENTINEL_SERVER.PORT="+hp, "SENTINEL_OPS.PORT="+op)
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			require.Error(t, cmd.Run(), "invalid startup rules must exit nonzero")
			require.NoError(t, childCtx.Err(), "a timeout is not failclosed startup proof")
			for _, address := range []string{httpAddress, opsAddress} {
				response, requestErr := (&http.Client{Timeout: time.Second}).Get("http://" + address + "/healthz")
				if response != nil {
					_ = response.Body.Close()
				}
				require.Error(t, requestErr, "no public or ops listener after invalid startup")
			}
		})
	}
}
