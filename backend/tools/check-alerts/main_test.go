package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckAlertsManifest(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/detection/scenarios.json")
	require.NoError(t, err)
	var fixtures struct {
		Cases []struct {
			Expected []struct {
				Payload json.RawMessage `json:"payload"`
			} `json:"expected_alerts"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	var canonical bytes.Buffer
	require.NoError(t, json.Compact(&canonical, fixtures.Cases[0].Expected[0].Payload))
	payload := canonical.Bytes()
	m := manifest{Tenant: "demo", SourceKeys: []string{"demo|192.0.2.11"}, EventIDs: []string{"current-source"}, Expected: []string{string(payload)}}
	t.Run("exact across three independent stores", func(t *testing.T) {
		require.NoError(t, reconcile(m, [][]byte{payload}, [][]byte{payload}, [][]byte{payload}))
	})
	for _, tc := range []struct {
		name          string
		kafka, ch, pg [][]byte
	}{
		{"historical only", nil, [][]byte{payload}, [][]byte{payload}},
		{"missing CH", [][]byte{payload}, nil, [][]byte{payload}},
		{"missing workflow", [][]byte{payload}, [][]byte{payload}, nil},
		{"extra", [][]byte{payload, bytes.Replace(payload, []byte("192.0.2.11"), []byte("192.0.2.11"), 1)}, [][]byte{payload}, [][]byte{payload}},
		{"wrong tenant", [][]byte{bytes.Replace(payload, []byte(`"tenant_id":"demo"`), []byte(`"tenant_id":"demo2"`), 1)}, [][]byte{payload}, [][]byte{payload}},
		{"wrong window", [][]byte{bytes.Replace(payload, []byte("00:05:00"), []byte("00:10:00"), 1)}, [][]byte{payload}, [][]byte{payload}},
		{"wrong payload", [][]byte{payload}, [][]byte{bytes.Replace(payload, []byte("Repeated failed"), []byte("Unexpected failed"), 1)}, [][]byte{payload}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Error(t, reconcile(m, tc.kafka, tc.ch, tc.pg)) })
	}
	t.Run("benign zero", func(t *testing.T) {
		m.Expected = nil
		require.NoError(t, reconcile(m, nil, nil, nil))
		require.Error(t, reconcile(m, [][]byte{payload}, nil, nil))
	})
}
