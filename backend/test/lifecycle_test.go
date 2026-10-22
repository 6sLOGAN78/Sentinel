//go:build integration

package test_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

const (
	stayUpFor       = 3 * time.Second
	shutdownLimit   = 30 * time.Second
	failFastLimit   = 15 * time.Second
	terminateSignal = syscall.SIGTERM
)

// serviceEnv returns the dotted SENTINEL_* variables a service needs. Every
// case other than TestBinaryLifecycleEnvFile uses a non-local environment, so
// no dotenv file is ever loaded.
func serviceEnv(t *testing.T, service string) []string {
	t.Helper()

	serverPort, err := sentineltesting.FreePort()
	if err != nil {
		t.Fatalf("allocate server port: %v", err)
	}
	opsPort, err := sentineltesting.FreePort()
	if err != nil {
		t.Fatalf("allocate ops port: %v", err)
	}

	redis := []string{"SENTINEL_REDIS.ADDRESS=" + redisCfg.Address}
	kafka := []string{
		"SENTINEL_KAFKA.BROKERS=" + kafkaCfg.Brokers,
		"SENTINEL_KAFKA.EVENTS_TOPIC=" + lifecycleTopic,
		"SENTINEL_KAFKA.EVENT_WRITER_GROUP=" + lifecycleGroup,
		"SENTINEL_KAFKA.ALERTS_TOPIC=" + lifecycleAlertsTopic,
		"SENTINEL_KAFKA.DETECTOR_GROUP=lifecycle-detector-" + uuid.NewString(),
		"SENTINEL_KAFKA.ALERT_WRITER_GROUP=lifecycle-alert-writer-" + uuid.NewString(),
	}
	ch := []string{
		"SENTINEL_CLICKHOUSE.HTTP_ADDRESS=" + chCfg.HTTPAddress,
		"SENTINEL_CLICKHOUSE.NATIVE_ADDRESS=" + chCfg.NativeAddress,
		"SENTINEL_CLICKHOUSE.USER=" + chCfg.User,
		"SENTINEL_CLICKHOUSE.PASSWORD=" + chCfg.Password,
		"SENTINEL_CLICKHOUSE.DATABASE=" + chCfg.Database,
	}
	pg := []string{
		"SENTINEL_DATABASE.HOST=" + pgCfg.Host,
		"SENTINEL_DATABASE.PORT=" + strconv.Itoa(pgCfg.Port),
		"SENTINEL_DATABASE.USER=" + pgCfg.User,
		"SENTINEL_DATABASE.PASSWORD=" + pgCfg.Password,
		"SENTINEL_DATABASE.NAME=" + pgCfg.Name,
		"SENTINEL_DATABASE.SSL_MODE=disable",
	}

	env := []string{
		"SENTINEL_PRIMARY.ENV=test",
		"SENTINEL_SERVER.BIND_ADDRESS=127.0.0.1",
		"SENTINEL_SERVER.PORT=" + serverPort,
		"SENTINEL_OPS.PORT=" + opsPort,
	}

	switch service {
	case "ingest":
		env = append(env, redis...)
		env = append(env, kafka...)
		env = append(env, pg...)
	case "detector":
		env = append(env, kafka...)
		env = append(env, pg...)
		env = append(env, "SENTINEL_DETECTOR.RULES_PATH="+lifecycleRulesPath)
	case "event-writer":
		env = append(env, kafka...)
		env = append(env, ch...)
	case "alert-writer":
		env = append(env, kafka...)
		env = append(env, ch...)
		env = append(env, pg...)
	case "query-api":
		key := make([]byte, 48)
		_, err = rand.Read(key)
		if err != nil {
			t.Fatalf("generate independent test key: %v", err)
		}
		env = append(env, "SENTINEL_AUTH.JWT_SECRET="+base64.RawURLEncoding.EncodeToString(key))
		env = append(env, pg...)
		env = append(env, ch...)
		env = append(env, redis...)
	}

	return env
}

// without drops variables with the given key prefix.
func without(env []string, prefix string) []string {
	out := make([]string, 0, len(env))

	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}

	return out
}

type proc struct {
	cmd    *exec.Cmd
	done   chan error
	output *syncBuffer
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// startProc starts a service binary with exactly the given environment and a
// fresh temporary working directory (or dir when non-empty).
func startProc(t *testing.T, service, dir string, env []string) *proc {
	t.Helper()

	if dir == "" {
		dir = t.TempDir()
	}

	out := &syncBuffer{}
	cmd := exec.CommandContext(t.Context(), filepath.Join(binDir, service))
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", service, err)
	}

	p := &proc{cmd: cmd, done: make(chan error, 1), output: out}

	go func() { p.done <- cmd.Wait() }()

	t.Cleanup(func() {
		select {
		case err := <-p.done:
			p.done <- err
		default:
			_ = cmd.Process.Kill()
		}
	})

	return p
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}

	return -1
}

// requireExit waits for the process to exit within limit and returns its code.
func (p *proc) requireExit(t *testing.T, limit time.Duration) int {
	t.Helper()

	select {
	case err := <-p.done:
		p.done <- err

		return exitCode(err)
	case <-time.After(limit):
		t.Fatalf("process did not exit within %s; output:\n%s", limit, p.output.String())

		return -1
	}
}

func (p *proc) requireRunningFor(t *testing.T, d time.Duration) {
	t.Helper()

	select {
	case err := <-p.done:
		p.done <- err
		t.Fatalf("process exited early (code %d); output:\n%s", exitCode(err), p.output.String())
	case <-time.After(d):
	}
}

func (p *proc) terminate(t *testing.T) int {
	t.Helper()

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	return p.requireExit(t, shutdownLimit)
}

func TestBinaryLifecycle(t *testing.T) {
	t.Parallel()

	for _, service := range services {
		t.Run(service+"/graceful", func(t *testing.T) {
			t.Parallel()

			p := startProc(t, service, "", serviceEnv(t, service))
			p.requireRunningFor(t, stayUpFor)

			if code := p.terminate(t); code != 0 {
				t.Fatalf("exit code %d after SIGTERM; output:\n%s", code, p.output.String())
			}

			if !strings.Contains(p.output.String(), "service started") {
				t.Fatalf("missing 'service started' log; output:\n%s", p.output.String())
			}
		})

		t.Run(service+"/sigint", func(t *testing.T) {
			t.Parallel()

			p := startProc(t, service, "", serviceEnv(t, service))
			p.requireRunningFor(t, stayUpFor)

			if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
				t.Fatalf("send SIGINT: %v", err)
			}

			if code := p.requireExit(t, shutdownLimit); code != 0 {
				t.Fatalf("exit code %d after SIGINT; output:\n%s", code, p.output.String())
			}
		})
	}
}

func TestBinaryLifecycleRedisDown(t *testing.T) {
	t.Parallel()

	env := without(serviceEnv(t, "ingest"), "SENTINEL_REDIS.ADDRESS=")
	env = append(env, "SENTINEL_REDIS.ADDRESS=127.0.0.1:1")

	p := startProc(t, "ingest", "", env)

	if code := p.requireExit(t, failFastLimit); code == 0 {
		t.Fatalf("expected non-zero exit; output:\n%s", p.output.String())
	}

	if !strings.Contains(strings.ToLower(p.output.String()), "redis") {
		t.Fatalf("output does not mention redis:\n%s", p.output.String())
	}
}

func TestBinaryLifecycleBadPostgresPassword(t *testing.T) {
	t.Parallel()

	env := without(serviceEnv(t, "query-api"), "SENTINEL_DATABASE.PASSWORD=")
	env = append(env, "SENTINEL_DATABASE.PASSWORD=definitely-wrong")

	p := startProc(t, "query-api", "", env)

	if code := p.requireExit(t, failFastLimit); code == 0 {
		t.Fatalf("expected non-zero exit; output:\n%s", p.output.String())
	}

	if strings.Contains(p.output.String(), "definitely-wrong") {
		t.Fatalf("password leaked in output:\n%s", p.output.String())
	}
}

func TestBinaryLifecycleQueryAPIKeyRequired(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", "short"} {
		t.Run("bytes-"+strconv.Itoa(len(key)), func(t *testing.T) {
			t.Parallel()
			env := without(serviceEnv(t, "query-api"), "SENTINEL_AUTH.JWT_SECRET=")
			if key != "" {
				env = append(env, "SENTINEL_AUTH.JWT_SECRET="+key)
			}
			p := startProc(t, "query-api", "", env)
			if code := p.requireExit(t, failFastLimit); code == 0 {
				t.Fatal("query-api accepted missing or short JWT key")
			}
			if !strings.Contains(p.output.String(), "JWT") {
				t.Fatal("startup did not report JWT configuration failure")
			}
		})
	}
}

func TestBinaryLifecycleKafkaDown(t *testing.T) {
	t.Parallel()

	env := without(serviceEnv(t, "detector"), "SENTINEL_KAFKA.BROKERS=")
	env = append(env, "SENTINEL_KAFKA.BROKERS=127.0.0.1:1")

	p := startProc(t, "detector", "", env)

	if code := p.requireExit(t, failFastLimit); code == 0 {
		t.Fatalf("expected non-zero exit; output:\n%s", p.output.String())
	}
}

func TestBinaryLifecycleMissingConfig(t *testing.T) {
	t.Parallel()

	env := without(serviceEnv(t, "ingest"), "SENTINEL_REDIS.ADDRESS=")
	p := startProc(t, "ingest", "", env)

	if code := p.requireExit(t, failFastLimit); code == 0 {
		t.Fatalf("expected non-zero exit; output:\n%s", p.output.String())
	}

	if !strings.Contains(p.output.String(), "Address") {
		t.Fatalf("config error does not name the field:\n%s", p.output.String())
	}
}

func TestBinaryLifecycleEnvFile(t *testing.T) {
	t.Parallel()

	envFile := filepath.Join(t.TempDir(), "root.env")
	content := strings.Join(serviceEnv(t, "ingest")[1:], "\n") + "\n"

	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("local loads the file", func(t *testing.T) {
		t.Parallel()

		p := startProc(t, "ingest", "", []string{"SENTINEL_PRIMARY.ENV=local", "SENTINEL_ENV_FILE=" + envFile})
		p.requireRunningFor(t, stayUpFor)

		if code := p.terminate(t); code != 0 {
			t.Fatalf("exit code %d; output:\n%s", code, p.output.String())
		}
	})

	t.Run("production ignores the file", func(t *testing.T) {
		t.Parallel()

		p := startProc(t, "ingest", "", []string{"SENTINEL_PRIMARY.ENV=production", "SENTINEL_ENV_FILE=" + envFile})

		if code := p.requireExit(t, failFastLimit); code == 0 {
			t.Fatalf("expected non-zero exit; output:\n%s", p.output.String())
		}

		if !strings.Contains(p.output.String(), "validate") {
			t.Fatalf("expected config validation error:\n%s", p.output.String())
		}
	})

	t.Run("working directory dotenv is not read", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		p := startProc(t, "ingest", dir, []string{"SENTINEL_PRIMARY.ENV=local"})

		if code := p.requireExit(t, failFastLimit); code == 0 {
			t.Fatalf("expected non-zero exit; output:\n%s", p.output.String())
		}
	})
}
