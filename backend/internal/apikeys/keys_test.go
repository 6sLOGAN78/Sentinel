package apikeys_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
)

func TestAPIKeyGenerateParse(t *testing.T) {
	t.Parallel()
	raw, err := apikeys.Generate()
	require.NoError(t, err)
	id, secret, err := apikeys.Parse(raw)
	require.NoError(t, err)
	require.Len(t, id, 12)
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	require.NoError(t, err)
	require.Len(t, decoded, 32)
	require.True(t, apikeys.VerifyHash(secret, apikeys.Hash(secret)))
	require.False(t, apikeys.VerifyHash("wrong", apikeys.Hash(secret)))
	underscore := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("\xff", 32)))
	_, parsed, err := apikeys.Parse("snt_abcdefghijkl_" + underscore)
	require.NoError(t, err)
	require.Equal(t, underscore, parsed, "SplitN preserves underscores in synthetic secret")
}

func TestAPIKeyRejectMalformed(t *testing.T) {
	t.Parallel()
	secret := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, raw := range []string{
		"", "snt_bad_" + secret, "snt_ABCDEFGHIJKL_" + secret, "snt_abc0efghijkl_" + secret,
		"bad_abcdefghijkl_" + secret, "snt_abcdefghijkl_" + secret + "=",
		"snt_abcdefghijkl_" + strings.Repeat("!", 43), "snt_abcdefghijkl_" + strings.Repeat("a", 43),
	} {
		_, _, err := apikeys.Parse(raw)
		require.Error(t, err)
	}
}
