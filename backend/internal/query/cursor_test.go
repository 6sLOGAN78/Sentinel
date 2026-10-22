package query_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/query"
)

func TestAlertCursorAuthenticationAndBinding(t *testing.T) {
	codec, err := query.NewCursorCodec([]byte(strings.Repeat("unit-test-key-material-", 3)))
	require.NoError(t, err)
	to := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	f := query.AlertFilters{
		ListOptions: query.ListOptions{From: to.Add(-24 * time.Hour), To: to, Limit: 3}, Status: "open", Search: `%_\`,
	}
	position := query.Position{CreatedAt: to.Add(-time.Hour), AlertID: strings.Repeat("a", 64)}
	raw, err := codec.Encode("alerts", "tenant-a", f, position)
	require.NoError(t, err)
	require.LessOrEqual(t, len(raw), 2048)
	frozen, got, err := codec.Decode(raw, "alerts", "tenant-a", f)
	require.NoError(t, err)
	require.Equal(t, f, frozen)
	require.Equal(t, position, got)
	_, _, err = codec.Decode(raw, "events", "tenant-a", f)
	require.ErrorIs(t, err, query.ErrInvalidQuery, "endpoint is authenticated")
	_, _, err = codec.Decode(raw, "alerts", "tenant-b", f)
	require.ErrorIs(t, err, query.ErrInvalidQuery)
	changed := f
	changed.Search = "other"
	_, _, err = codec.Decode(raw, "alerts", "tenant-a", changed)
	require.ErrorIs(t, err, query.ErrInvalidQuery)
	withoutRange := f
	withoutRange.From, withoutRange.To = time.Time{}, time.Time{}
	frozen, got, err = codec.Decode(raw, "alerts", "tenant-a", withoutRange)
	require.NoError(t, err)
	require.Equal(t, f, frozen, "omitted defaults reuse original range")
	require.Equal(t, position, got)
	bytes, err := base64.RawURLEncoding.DecodeString(raw)
	require.NoError(t, err)
	for _, index := range []int{0, len(bytes) / 2, len(bytes) - 1} {
		mutated := append([]byte(nil), bytes...)
		mutated[index] ^= 1
		_, _, err = codec.Decode(base64.RawURLEncoding.EncodeToString(mutated), "alerts", "tenant-a", f)
		require.ErrorIs(t, err, query.ErrInvalidQuery)
	}
	other, err := query.NewCursorCodec([]byte(strings.Repeat("different-secret-material", 2)))
	require.NoError(t, err)
	_, _, err = other.Decode(raw, "alerts", "tenant-a", f)
	require.ErrorIs(t, err, query.ErrInvalidQuery, "public base64 cannot supply cursor authority")
	for _, invalid := range []string{"", "bad", strings.Repeat("x", 2049)} {
		_, _, err = codec.Decode(invalid, "alerts", "tenant-a", f)
		require.ErrorIs(t, err, query.ErrInvalidQuery)
	}
}

func TestAlertCursorRejectsInvalidPayload(t *testing.T) {
	_, err := query.NewCursorCodec([]byte("short"))
	require.Error(t, err)
	codec, err := query.NewCursorCodec([]byte(strings.Repeat("unit-test-key-material-", 3)))
	require.NoError(t, err)
	to := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	f := query.AlertFilters{ListOptions: query.ListOptions{From: to.Add(-time.Hour), To: to, Limit: 50}}
	for _, position := range []query.Position{
		{CreatedAt: to, AlertID: strings.Repeat("a", 64)},
		{CreatedAt: f.From.Add(-time.Millisecond), AlertID: strings.Repeat("a", 64)},
		{CreatedAt: f.From.Add(time.Microsecond), AlertID: strings.Repeat("a", 64)},
		{CreatedAt: f.From, AlertID: "invalid"},
	} {
		_, err = codec.Encode("alerts", "tenant-a", f, position)
		require.ErrorIs(t, err, query.ErrInvalidQuery)
	}
}
