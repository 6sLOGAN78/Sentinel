//go:build integration

package test_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/detection"
)

func TestDetectorCapacity(t *testing.T) {
	env, kafka := detectorPGEnv(t, pgCfg)
	p := startProc(t, "detector", "", env)
	url := "http://127.0.0.1:" + opsPort(t, env)
	requireStatusEventually(t, url+"/readyz", http.StatusOK)
	admin := checkpointAdmin(t)
	producer := newWriterProducer(t, kafka)
	start := time.Now().UTC().Truncate(5 * time.Minute).Add(time.Second)
	events := canonicalWriterEvents(detection.MaxWindowEvents+1, start)
	ip, principal := "192.0.2.19", "capacity"
	for _, event := range events {
		event.EventType, event.SourceIP, event.Principal = "auth.failure", &ip, &principal
		event.OccurredAt.Time = start
	}
	for first := 0; first < detection.MaxWindowEvents; first += 256 {
		end := min(first+256, detection.MaxWindowEvents)
		require.NoError(t, producer.PublishBatch(t.Context(), events[first:end], uuid.NewString()))
	}
	require.Eventually(t, func() bool {
		offsets, err := admin.FetchOffsets(t.Context(), kafka.DetectorGroup)
		if err != nil {
			return false
		}
		o, ok := offsets.Lookup(kafka.EventsTopic, 0)
		var m detection.Metadata
		return ok && o.Err == nil && json.Unmarshal([]byte(o.Metadata), &m) == nil &&
			m.ProcessedNext == detection.MaxWindowEvents
	}, 20*time.Second, 25*time.Millisecond, p.output.String())
	before := brokerCheckpoint(t, admin, kafka)
	require.Zero(t, before.At, "live state must retain its first contributor")
	require.NoError(t, producer.PublishBatch(t.Context(), events[detection.MaxWindowEvents:], uuid.NewString()))
	require.NotZero(t, p.requireExit(t, 5*time.Second), p.output.String())
	require.Contains(t, p.output.String(), "capacity")
	after := brokerCheckpoint(t, admin, kafka)
	require.Equal(t, before.At, after.At)
	require.Equal(t, before.Metadata, after.Metadata)
	response, err := (&http.Client{Timeout: time.Second}).Get(url + "/readyz")
	if err == nil {
		defer response.Body.Close()
		require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	}
	t.Run("late_counter_runtime", func(t *testing.T) {
		metricEnv, metricKafka := detectorPGEnv(t, pgCfg)
		process := startProc(t, "detector", "", metricEnv)
		metricURL := "http://127.0.0.1:" + opsPort(t, metricEnv)
		requireStatusEventually(t, metricURL+"/readyz", http.StatusOK)
		inputs := canonicalWriterEvents(3, start)
		for _, e := range inputs {
			e.EventType = "auth.success"
			e.SourceIP = &ip
			e.OccurredAt.Time = start
		}
		inputs[1].OccurredAt.Time = start.Add(-detection.Lateness)
		inputs[2].OccurredAt.Time = start.Add(-detection.Lateness - time.Millisecond)
		require.NoError(t, newWriterProducer(t, metricKafka).PublishBatch(t.Context(), inputs, uuid.NewString()))
		require.Eventually(t, func() bool {
			r, getErr := (&http.Client{Timeout: time.Second}).Get(metricURL + "/metrics")
			if getErr != nil {
				return false
			}
			defer r.Body.Close()
			body, readErr := io.ReadAll(r.Body)
			return readErr == nil && r.StatusCode == http.StatusOK &&
				strings.Contains(string(body), "detection_late_events_total 1\n") && !strings.Contains(string(body), "{")
		}, 5*time.Second, 25*time.Millisecond, "worker late classification must reach internal counter")
		require.Zero(t, process.terminate(t), process.output.String())
	})
}
