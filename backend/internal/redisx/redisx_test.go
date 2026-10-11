package redisx_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
)

func TestRedisUnreachableIsFatal(t *testing.T) {
	t.Parallel()

	c, err := redisx.New(context.Background(), config.RedisConfig{Address: "127.0.0.1:1"})
	if err == nil || c != nil {
		t.Fatalf("expected nil client and error, got %v %v", c, err)
	}

	if !strings.Contains(err.Error(), "pinging redis") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRedisCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c, err := redisx.New(ctx, config.RedisConfig{Address: "127.0.0.1:1"})
	if err == nil || c != nil {
		t.Fatalf("expected nil client and error, got %v %v", c, err)
	}
}

// writeCA writes a self-signed PEM certificate to a temp file.
func writeCA(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "sentinel-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "ca.pem")
	if err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestRedisOptionsPlainCarriesAuth(t *testing.T) {
	t.Parallel()

	opts, err := redisx.Options(config.RedisConfig{Address: "127.0.0.1:6379", Username: "svc", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	if opts.Addr != "127.0.0.1:6379" || opts.Username != "svc" || opts.Password != "pw" || opts.TLSConfig != nil {
		t.Fatalf("unexpected options: addr=%q user=%q tls=%v", opts.Addr, opts.Username, opts.TLSConfig != nil)
	}
}

func TestRedisOptionsTLS(t *testing.T) {
	t.Parallel()

	opts, err := redisx.Options(config.RedisConfig{Address: "redis.example.test:6380", Password: "pw", TLS: true})
	if err != nil {
		t.Fatal(err)
	}

	if opts.TLSConfig == nil || opts.TLSConfig.ServerName != "redis.example.test" ||
		opts.TLSConfig.MinVersion != tls.VersionTLS12 || opts.TLSConfig.InsecureSkipVerify {
		t.Fatalf("unexpected TLS config: %+v", opts.TLSConfig)
	}

	if opts.TLSConfig.RootCAs != nil {
		t.Fatal("system roots expected when no CA file is set")
	}

	opts, err = redisx.Options(config.RedisConfig{Address: "redis.example.test:6380", TLS: true, CACertFile: writeCA(t)})
	if err != nil {
		t.Fatal(err)
	}

	if opts.TLSConfig.RootCAs == nil {
		t.Fatal("CA file not loaded into RootCAs")
	}
}

func TestRedisOptionsBadCAFile(t *testing.T) {
	t.Parallel()

	junk := filepath.Join(t.TempDir(), "junk.pem")
	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{junk, filepath.Join(t.TempDir(), "missing.pem")} {
		if _, err := redisx.Options(config.RedisConfig{Address: "127.0.0.1:6380", TLS: true, CACertFile: path}); err == nil {
			t.Fatalf("expected error for CA file %s", path)
		}

		if _, err := redisx.New(context.Background(), config.RedisConfig{
			Address: "127.0.0.1:6380", TLS: true, CACertFile: path,
		}); err == nil {
			t.Fatalf("New must fail for CA file %s", path)
		}
	}
}

// TestRedisTLSAgainstPlainServerFails proves TLS is really negotiated: a
// listener that never speaks TLS makes the startup ping fail.
func TestRedisTLSAgainstPlainServerFails(t *testing.T) {
	t.Parallel()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}

			_, _ = c.Write([]byte("-ERR plaintext\r\n"))
			_ = c.Close()
		}
	}()

	c, err := redisx.New(context.Background(), config.RedisConfig{Address: ln.Addr().String(), TLS: true})
	if err == nil || c != nil {
		t.Fatalf("expected TLS handshake failure, got %v %v", c, err)
	}
}
