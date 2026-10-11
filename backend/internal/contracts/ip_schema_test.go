package contracts_test

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ipv6Spellings returns every hex-only text form of addr: padded and
// unpadded full forms in both cases plus each "::" compression of every run
// of zero groups (including single-group runs).
func ipv6Spellings(addr netip.Addr) []string {
	raw := addr.As16()

	groups := make([]string, 8)
	for i := range groups {
		groups[i] = strconv.FormatUint(uint64(raw[2*i])<<8|uint64(raw[2*i+1]), 16)
	}

	padded := make([]string, 8)
	for i, g := range groups {
		padded[i] = fmt.Sprintf("%04s", g)
	}

	out := []string{
		strings.Join(groups, ":"),
		strings.Join(padded, ":"),
		strings.ToUpper(strings.Join(padded, ":")),
	}

	for start := range groups {
		for end := start; end < len(groups) && groups[end] == "0"; end++ {
			out = append(out, strings.Join(groups[:start], ":")+"::"+strings.Join(groups[end+1:], ":"))
		}
	}

	return out
}

// TestSchemaIPv6AgreesWithGo checks that the JSON Schema and Go optIP agree
// on IPv4-mapped detection for every spelling of mapped and near-mapped
// addresses (CR WR-08).
func TestSchemaIPv6AgreesWithGo(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(eventSchemaName)
	require.NoError(t, err)

	var base map[string]any
	require.NoError(t, json.Unmarshal(mustRead(t, filepath.Join("testdata", "event", "valid", "ipv6.json")), &base))

	addrs := []string{
		"::ffff:192.0.2.1", "::ffff:0.0.0.0", "::ffff:0.0.0.1", "::ffff:0.1.0.0", // mapped
		"::ffff:1", "::ffff", "::fffe:c000:201", "0:0:0:0:1:ffff:c000:201", // not mapped
		"0:ffff::", "ffff::1", "2001:db8::ffff:1", "::1", "::", "0:0:0:0:ffff:0:0:0", "1::ffff:c000:201",
	}

	for _, a := range addrs {
		for _, spelled := range ipv6Spellings(netip.MustParseAddr(a)) {
			doc := map[string]any{}
			for k, v := range base {
				doc[k] = v
			}

			doc["source_ip"] = spelled

			raw, merr := json.Marshal(doc)
			require.NoError(t, merr)

			schemaOK, _ := schemaAccepts(t, sch, raw)
			goOK, goErr := goAccepts(raw)
			require.Equal(t, goOK, schemaOK, "%s (from %s): go err %v", spelled, a, goErr)
		}
	}
}
