// Package redisx constructs the Redis client used for rate limiting.
package redisx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const pingTimeout = 5 * time.Second

// errNoCACerts is returned when the CA file holds no PEM certificate.
var errNoCACerts = errors.New("redis ca_cert_file contains no PEM certificates")

// Options builds the client options for cfg: AUTH credentials when set and,
// when TLS is enabled, TLS 1.2+ verified against the system roots or the
// configured CA bundle, with the server name taken from the address host.
func Options(cfg config.RedisConfig) (*redis.Options, error) {
	opts := &redis.Options{
		Addr: cfg.Address, Username: cfg.Username, Password: cfg.Password,
		DialTimeout: cfg.DialTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout,
		MaxRetries: cfg.MaxRetries, ContextTimeoutEnabled: true,
	}

	if !cfg.TLS {
		return opts, nil
	}

	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("redis address: %w", err)
	}

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}

	if cfg.CACertFile != "" {
		pem, rerr := os.ReadFile(cfg.CACertFile)
		if rerr != nil {
			return nil, fmt.Errorf("reading redis ca_cert_file: %w", rerr)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errNoCACerts
		}

		tlsCfg.RootCAs = pool
	}

	opts.TLSConfig = tlsCfg

	return opts, nil
}

// New creates a Redis client and pings it. A failed ping closes the client and
// returns an error; callers must treat it as fatal at startup.
func New(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	opts, err := Options(cfg)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	if err = client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()

		return nil, fmt.Errorf("pinging redis: %w", err)
	}

	return client, nil
}
