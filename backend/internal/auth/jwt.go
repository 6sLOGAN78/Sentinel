// Package auth verifies dashboard identities before exposing tenant authority.
package auth

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/errs"
)

const (
	principalKey   = "sentinel.verified-principal"
	minSecretBytes = 32
	tokenLeeway    = 30 * time.Second
)

// Authentication errors never contain supplied credentials or parser details.
var (
	ErrInvalid     = errors.New("invalid dashboard credential")
	ErrUnavailable = errors.New("dashboard authentication unavailable")
	ErrForbidden   = errors.New("dashboard role not permitted")
	tenantPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
)

// Principal contains only verified identity and authorization information.
type Principal struct {
	Subject   string    `json:"subject"`
	TenantID  string    `json:"tenant_id"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

type claims struct {
	jwt.RegisteredClaims
	Tenant string `json:"tenant"`
	Role   string `json:"role"`
}

// Verifier accepts only the fixed development issuer/audience and HS256.
type Verifier struct {
	secret   []byte
	issuer   string
	audience string
}

// NewVerifier requires an operator-provided key of at least 32 bytes.
func NewVerifier(secret []byte, issuer, audience string) (*Verifier, error) {
	if len(secret) < minSecretBytes {
		return nil, errors.New("dashboard JWT secret must contain at least 32 bytes")
	}
	if issuer != "sentinel-dev" || audience != "sentinel-dashboard" {
		return nil, errors.New("dashboard JWT issuer and audience must match the fixed contract")
	}
	return &Verifier{secret: append([]byte(nil), secret...), issuer: issuer, audience: audience}, nil
}

// Verify checks signature and all required claims before constructing a principal.
func (v *Verifier) Verify(ctx context.Context, raw string) (Principal, error) {
	if ctx.Err() != nil {
		return Principal{}, ErrUnavailable
	}
	if v == nil || len(v.secret) < minSecretBytes {
		return Principal{}, ErrUnavailable
	}
	if len(raw) == 0 || len(raw) > 8192 {
		return Principal{}, ErrInvalid
	}
	c := &claims{}
	token, err := jwt.ParseWithClaims(raw, c, func(*jwt.Token) (any, error) { return v.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(), jwt.WithNotBeforeRequired(), jwt.WithIssuedAt(),
		jwt.WithLeeway(tokenLeeway), jwt.WithStrictDecoding())
	if err != nil || token == nil || !token.Valid {
		return Principal{}, ErrInvalid
	}
	if strings.TrimSpace(c.Subject) == "" || !tenantPattern.MatchString(c.Tenant) ||
		(c.Role != "viewer" && c.Role != "analyst" && c.Role != "admin") || c.IssuedAt == nil ||
		c.ExpiresAt == nil || c.NotBefore == nil || !c.ExpiresAt.After(c.IssuedAt.Time) ||
		c.ExpiresAt.Sub(c.IssuedAt.Time) > 15*time.Minute || c.NotBefore.After(c.ExpiresAt.Time) {
		return Principal{}, ErrInvalid
	}
	return Principal{Subject: c.Subject, TenantID: c.Tenant, Role: c.Role, ExpiresAt: c.ExpiresAt.UTC()}, nil
}

// Middleware refuses ambiguous headers and stores only a verified principal.
func (v *Verifier) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		headers := c.Request().Header.Values("Authorization")
		if len(headers) != 1 {
			return unauthorized()
		}
		parts := strings.Fields(headers[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return unauthorized()
		}
		p, err := v.Verify(c.Request().Context(), parts[1])
		if errors.Is(err, ErrUnavailable) {
			return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
		}
		if err != nil {
			return unauthorized()
		}
		c.Set(principalKey, p)
		c.Response().Header().Set("Cache-Control", "no-store")
		return next(c)
	}
}

// RequirePrincipal reads the verified middleware result, never request fields.
func RequirePrincipal(c echo.Context) (Principal, error) {
	p, ok := c.Get(principalKey).(Principal)
	if !ok {
		return Principal{}, unauthorized()
	}
	return p, nil
}

// RequireTriage permits only the two roles allowed to transition workflows.
func RequireTriage(p Principal) error {
	if p.Role != "analyst" && p.Role != "admin" {
		return ErrForbidden
	}
	return nil
}

func unauthorized() error {
	return errs.NewUnauthorizedError(http.StatusText(http.StatusUnauthorized), false)
}
