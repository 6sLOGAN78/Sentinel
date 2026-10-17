package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/stretchr/testify/require"
)

// No detector import: these vectors certify the existing wire/identity contract,
// independently of both the Python builder and the later detector rule proofs.
func TestScenarioIdentityFixtures(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../../testdata/detection/scenarios.json")
	require.NoError(t, err)
	var fixture struct {
		Cases []struct {
			Name           string            `json:"name"`
			Tenant         string            `json:"tenant"`
			Events         []json.RawMessage `json:"events"`
			ExpectedAlerts []struct {
				Entity           []string        `json:"entity"`
				Payload          json.RawMessage `json:"payload"`
				CanonicalPayload string          `json:"canonical_payload"`
			} `json:"expected_alerts"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	require.Len(t, fixture.Cases, 9)
	for _, tc := range fixture.Cases {
		t.Run(tc.Name+"/"+tc.Tenant, func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, tc.Events)
			ids := make(map[string]struct{}, len(tc.Events))
			for _, raw := range tc.Events {
				event, decodeErr := contracts.DecodeEvent(bytes.NewReader(raw))
				require.NoError(t, decodeErr)
				require.Equal(t, tc.Tenant, event.TenantID)
				ids[event.EventID] = struct{}{}
			}
			for _, expected := range tc.ExpectedAlerts {
				alert, decodeErr := contracts.DecodeAlert(bytes.NewReader(expected.Payload))
				require.NoError(t, decodeErr)
				require.NoError(t, alert.Validate())
				require.Equal(t, tc.Tenant, alert.TenantID)
				require.Equal(t, contracts.DedupeKey(tc.Tenant, alert.RuleID,
					expected.Entity, alert.WindowStart.Time), alert.AlertID)
				require.True(t, alert.CreatedAt.Equal(alert.WindowEnd.Time))
				require.Nil(t, alert.DetectedAt)
				for _, id := range alert.SampleEventIDs {
					require.Contains(t, ids, id)
				}
				wire, marshalErr := json.Marshal(alert)
				require.NoError(t, marshalErr)
				require.Equal(t, expected.CanonicalPayload, string(wire))
			}
			if tc.Name == "benign" {
				require.Empty(t, tc.ExpectedAlerts)
			}
		})
	}
}
