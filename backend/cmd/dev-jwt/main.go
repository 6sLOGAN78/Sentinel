// Command dev-jwt mints bounded local/test dashboard tokens using a private environment key.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const maxLifetime = 15 * time.Minute

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr,
			"dev-jwt: mint refused; check local/test mode, private key and identity/lifetime flags")
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	// Check the original process mode before local dotenv loading.
	mode := os.Getenv("SENTINEL_PRIMARY.ENV")
	if mode != "local" && mode != "test" {
		return errors.New("development token minting requires local or test mode")
	}
	options := flag.NewFlagSet("dev-jwt", flag.ContinueOnError)
	options.SetOutput(io.Discard)
	tenant := options.String("tenant", "", "verified tenant slug")
	subject := options.String("subject", "", "operator subject")
	role := options.String("role", "", "viewer, analyst or admin")
	lifetime := options.Duration("lifetime", maxLifetime, "token validity, 1s through 15m")
	if err := options.Parse(args); err != nil {
		return errors.New("invalid token flags")
	}
	if options.NArg() != 0 || !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`).MatchString(*tenant) ||
		strings.TrimSpace(*subject) == "" || (*role != "viewer" && *role != "analyst" && *role != "admin") ||
		*lifetime < time.Second || *lifetime > maxLifetime {
		return errors.New("invalid token identity or lifetime")
	}
	cfg, err := config.Load(config.ServiceDevJWT)
	if err != nil {
		return errors.New("invalid token configuration")
	}
	now := time.Now().UTC().Truncate(time.Second)
	claims := jwt.MapClaims{
		"iss": cfg.Auth.Issuer, "aud": cfg.Auth.Audience, "sub": *subject, "tenant": *tenant, "role": *role,
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(*lifetime).Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.Auth.JWTSecret))
	if err != nil || len(token) > 8192 {
		return errors.New("cannot mint bounded token")
	}
	_, err = fmt.Fprintln(output, token)
	return err
}
