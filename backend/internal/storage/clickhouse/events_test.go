package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttributesJSONRoundTrip(t *testing.T) {
	t.Parallel()

	for _, attributes := range []map[string]any{
		nil,
		{"count": float64(2), "nested": map[string]any{"enabled": true}},
	} {
		raw, err := marshalAttributes(attributes)
		require.NoError(t, err)
		got, err := unmarshalAttributes(raw)
		require.NoError(t, err)
		require.Equal(t, attributes, got)
	}
}

func TestNullablePortConversion(t *testing.T) {
	t.Parallel()

	require.Nil(t, nullablePort(nil))
	port := 443
	got := nullablePort(&port)
	require.NotNil(t, got)
	require.Equal(t, uint16(443), *got)
}
