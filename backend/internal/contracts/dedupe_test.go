package contracts_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/stretchr/testify/require"
)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

var goldenWindow = time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // immutable test clock

func TestDedupeKeyGolden(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		tenant string
		rule   string
		entity []string
		want   string
	}{
		{"ipv4_user", "demo", "AUTH_BRUTE_FORCE", []string{"192.0.2.10", "example-user"},
			"687ae716032ed9a62e426faf0f42aa90f4a81204aa8fcb17c5354a2a6cbe8779"},
		{"ipv6_user", "demo", "AUTH_BRUTE_FORCE", []string{"2001:db8::1", "example-user"},
			"0159a51147649c3aee7ff5d2eec8fb6d955d742a869a60c85ec6143b6b7f1928"},
		{"zero_components", "demo", "PORT_SCAN", []string{},
			"9e28ebd0bbffbe5ec6ccfca92b2bb9f17dc6251345e424bc531201a70b695cc2"},
		{"one_empty_component", "demo", "PORT_SCAN", []string{""},
			"04fff2248be75eae45d2c61e7ac4b6852df3333d2243e898a8746f384094dfe2"},
		{"other_entity", "demo", "AUTH_BRUTE_FORCE", []string{"198.51.100.7", "svc-a"},
			"13da36537f2ee30090fe9d1bd59161fc3a8556d2c55b207909d68c1530a3832b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := contracts.DedupeKey(tc.tenant, tc.rule, tc.entity, goldenWindow)
			require.Equal(t, tc.want, got)
			require.Regexp(t, hex64, got)
		})
	}
}

func TestDedupeKeyNilEqualsEmptyEntity(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		contracts.DedupeKey("demo", "R", nil, goldenWindow),
		contracts.DedupeKey("demo", "R", []string{}, goldenWindow))
}

func TestDedupeKeyWindowNormalization(t *testing.T) {
	t.Parallel()

	ent := []string{"a"}
	base := contracts.DedupeKey("demo", "R", ent, goldenWindow)

	ist := time.FixedZone("IST", 5*3600+1800)
	require.Equal(t, base, contracts.DedupeKey("demo", "R", ent, goldenWindow.In(ist)))
	require.Equal(t, base, contracts.DedupeKey("demo", "R", ent, goldenWindow.Add(999*time.Microsecond)))
	require.NotEqual(t, base, contracts.DedupeKey("demo", "R", ent, goldenWindow.Add(time.Millisecond)))
}

func TestDedupeKeyCollisions(t *testing.T) {
	t.Parallel()

	w := goldenWindow
	pairs := []struct {
		name string
		a, b string
	}{
		{"pipe_in_entity", contracts.DedupeKey("demo", "R", []string{"x|y", "z"}, w),
			contracts.DedupeKey("demo", "R", []string{"x", "y|z"}, w)},
		{"pipe_tenant_rule", contracts.DedupeKey("a", "b|c", []string{"e"}, w),
			contracts.DedupeKey("a|b", "c", []string{"e"}, w)},
		{"component_count", contracts.DedupeKey("demo", "R", []string{"a"}, w),
			contracts.DedupeKey("demo", "R", []string{"a", ""}, w)},
		{"tenant_vs_rule_boundary", contracts.DedupeKey("ab", "c", nil, w),
			contracts.DedupeKey("a", "bc", nil, w)},
	}

	for _, p := range pairs {
		require.NotEqual(t, p.a, p.b, p.name)
	}
}

func TestDedupeKeyFieldSensitivity(t *testing.T) {
	t.Parallel()

	w := goldenWindow
	base := contracts.DedupeKey("demo", "R", []string{"a", "b"}, w)

	variants := map[string]string{
		"tenant": contracts.DedupeKey("demo2", "R", []string{"a", "b"}, w),
		"rule":   contracts.DedupeKey("demo", "R2", []string{"a", "b"}, w),
		"ent0":   contracts.DedupeKey("demo", "R", []string{"x", "b"}, w),
		"ent1":   contracts.DedupeKey("demo", "R", []string{"a", "x"}, w),
		"order":  contracts.DedupeKey("demo", "R", []string{"b", "a"}, w),
		"window": contracts.DedupeKey("demo", "R", []string{"a", "b"}, w.Add(time.Second)),
	}

	for name, v := range variants {
		require.NotEqual(t, base, v, name)
		require.Regexp(t, hex64, v)
	}
}

func TestDedupeKeyValidWindow(t *testing.T) {
	t.Parallel()

	require.True(t, contracts.ValidWindow(goldenWindow))
	require.True(t, contracts.ValidWindow(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)))
	require.False(t, contracts.ValidWindow(time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)))
	require.False(t, contracts.ValidWindow(time.Date(2101, 1, 1, 0, 0, 0, 0, time.UTC)))
	require.False(t, contracts.ValidWindow(time.Time{}))
	require.Regexp(t, hex64, contracts.DedupeKey("d", "R", nil, time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)))
}

func FuzzDedupeKey(f *testing.F) {
	f.Add("demo", "R", "x|y", "z", "demo", "R", "x", "y|z", int64(0), int64(0))
	f.Add("a", "b|c", "e", "f", "a|b", "c", "e", "f", int64(1), int64(1))
	f.Add("demo", "R", "a", "", "demo", "R", "a", "", int64(1791540000000), int64(1791540000000))
	f.Add("", "", "", "", "", "", "", "", int64(-1), int64(-1))

	f.Fuzz(func(t *testing.T, t1, r1, a1, b1, t2, r2, a2, b2 string, w1, w2 int64) {
		k1 := contracts.DedupeKey(t1, r1, []string{a1, b1}, time.UnixMilli(w1))
		k2 := contracts.DedupeKey(t2, r2, []string{a2, b2}, time.UnixMilli(w2))

		same := t1 == t2 && r1 == r2 && a1 == a2 && b1 == b2 && w1 == w2
		if same != (k1 == k2) {
			t.Fatalf("injectivity violated: inputs equal=%v keys equal=%v", same, k1 == k2)
		}

		if !hex64.MatchString(k1) {
			t.Fatalf("key %q is not 64 lowercase hex", k1)
		}
	})
}
