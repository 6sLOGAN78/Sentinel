package auth_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/auth"
)

func TestVerifierClaims(t *testing.T) {
	key := make([]byte, 48)
	_, err := rand.Read(key)
	require.NoError(t, err)
	verifier, err := auth.NewVerifier(key, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	for _, name := range []string{
		"valid", "none", "wrong-algorithm", "wrong-issuer", "wrong-audience", "unknown-role",
		"future-iat", "long-lifetime", "overflow", "exp", "iat", "nbf", "sub", "tenant", "role",
	} {
		t.Run(name, func(t *testing.T) {
			claims := jwt.MapClaims{
				"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "operator", "tenant": "tenant-a",
				"role": "analyst", "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(15 * time.Minute).Unix(),
			}
			raw := verifierFixture(t, name, claims, key, now)
			principal, verifyErr := verifier.Verify(t.Context(), raw)
			if name == "valid" {
				require.NoError(t, verifyErr)
				require.Equal(t, "tenant-a", principal.TenantID)
				require.Equal(t, "operator", principal.Subject)
				return
			}
			require.ErrorIs(t, verifyErr, auth.ErrInvalid)
			require.Empty(t, principal.TenantID)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = verifier.Verify(ctx, "invalid")
	require.ErrorIs(t, err, auth.ErrUnavailable)
}

func verifierFixture(t *testing.T, name string, claims jwt.MapClaims, key []byte, now time.Time) string {
	t.Helper()
	method := jwt.SigningMethodHS256
	switch name {
	case "valid":
	case "none":
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
		payload, err := jwt.NewWithClaims(method, claims).SigningString()
		require.NoError(t, err)
		_, payload, _ = strings.Cut(payload, ".")
		return header + "." + payload + "."
	case "wrong-algorithm":
		method = jwt.SigningMethodHS384
	case "wrong-issuer":
		claims["iss"] = "other-issuer"
	case "wrong-audience":
		claims["aud"] = "other-audience"
	case "unknown-role":
		claims["role"] = "owner"
	case "future-iat":
		claims["iat"] = now.Add(time.Minute).Unix()
		claims["exp"] = now.Add(10 * time.Minute).Unix()
	case "long-lifetime":
		claims["exp"] = now.Add(15*time.Minute + time.Second).Unix()
	case "overflow":
		claims["sub"] = strings.Repeat("x", 8193)
	default:
		delete(claims, name)
	}
	raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
	require.NoError(t, err)
	return raw
}

func TestVerifierRequiresKey(t *testing.T) {
	for _, size := range []int{0, 1, 31} {
		_, err := auth.NewVerifier(make([]byte, size), "sentinel-dev", "sentinel-dashboard")
		require.Error(t, err)
	}
}

func TestRequireAdmin(t *testing.T) {
	t.Parallel()
	require.NoError(t, auth.RequireAdmin(auth.Principal{Subject: "a", TenantID: "t", Role: "admin"}))
	for _, role := range []string{"analyst", "viewer", "", "Admin", "operator"} {
		err := auth.RequireAdmin(auth.Principal{Subject: "a", TenantID: "t", Role: role})
		require.ErrorIs(t, err, auth.ErrForbidden, "role %q", role)
	}
}

func TestOperatorTenantsFailClosed(t *testing.T) {
	t.Parallel()
	for name, set := range map[string]auth.OperatorTenants{
		"zero":  {},
		"nil":   auth.NewOperatorTenants(nil),
		"empty": auth.NewOperatorTenants([]string{}),
	} {
		for _, tenant := range []string{"demo", "tenant-a", ""} {
			require.False(t, set.Includes(auth.Principal{Subject: "a", TenantID: tenant, Role: "admin"}),
				"%s allowlist must include no tenant (%q)", name, tenant)
		}
	}
}

func TestOperatorTenantsIncludesOnlyListedTenants(t *testing.T) {
	t.Parallel()
	// Malformed entries never match even if a caller skipped config validation.
	set := auth.NewOperatorTenants([]string{"demo", "Demo2", "", " ops ", "a/b"})
	require.True(t, set.Includes(auth.Principal{Subject: "a", TenantID: "demo", Role: "admin"}))
	for _, tenant := range []string{"demo2", "Demo2", "", "ops", " ops ", "a/b", "demo "} {
		require.False(t, set.Includes(auth.Principal{Subject: "a", TenantID: tenant, Role: "admin"}), tenant)
	}
}
