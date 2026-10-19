//go:build integration

package test_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestDetectorThirdReplicaRebalance(t *testing.T) {
	f := newCooldownFixture(t, false)
	require.Zero(t, f.process.terminate(t), f.process.output.String())
	topic := "detector-transfer-" + uuid.NewString()
	response, err := f.admin.CreateTopics(t.Context(), 12, 1, nil, topic)
	require.NoError(t, err)
	require.NoError(t, response[topic].Err)
	f.kafka.EventsTopic = topic
	f.env = append(without(f.env, "SENTINEL_KAFKA.EVENTS_TOPIC="), "SENTINEL_KAFKA.EVENTS_TOPIC="+topic)
	binary := buildIntegrationBinary(t, "detector")
	first, firstDir := observedDetector(t, binary, f.env)
	second, secondDir := observedDetector(t, binary, f.env)
	f.process = first
	require.Eventually(t, func() bool {
		groups, describeErr := f.admin.DescribeGroups(t.Context(), f.kafka.DetectorGroup)
		return describeErr == nil && groups[f.kafka.DetectorGroup].Err == nil && len(groups[f.kafka.DetectorGroup].Members) == 2
	}, 15*time.Second, 25*time.Millisecond)
	producer, err := kgo.NewClient(kgo.SeedBrokers(kafkaCfg.Brokers), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	canonical := make(map[int32][]byte)
	tenth := make(map[int32]*kgo.Record)
	for partition := range int32(12) {
		events, payload := recoveryAuthEvents(t, f, fmt.Sprintf("transfer-%s-%d", f.principal, partition))
		canonical[partition] = payload
		for i, event := range events {
			raw, marshalErr := json.Marshal(event)
			require.NoError(t, marshalErr)
			record := &kgo.Record{Topic: topic, Partition: partition, Key: []byte("demo|" + f.ip), Value: raw}
			if i == 9 {
				tenth[partition] = record
			} else {
				require.NoError(t, producer.ProduceSync(t.Context(), record).FirstErr())
			}
		}
		recoveryCheckpoint(t, f, partition, 9)
	}
	oldOwners := make(map[int32]string)
	require.Eventually(t, func() bool {
		for _, dir := range []string{firstDir, secondDir} {
			for _, observation := range detectorObservations(t, dir) {
				if observation.Boundary == "committed" && observation.Checkpoint != nil {
					var m struct {
						ProcessedNext int64 `json:"processed_next"`
					}
					if json.Unmarshal([]byte(observation.Checkpoint.Metadata), &m) == nil && m.ProcessedNext == 9 {
						oldOwners[observation.Partition] = observation.Member
					}
				}
			}
		}
		return len(oldOwners) == 12
	}, 20*time.Second, 25*time.Millisecond, "missing actual per-process incorporated-nine checkpoint observations")
	third, thirdDir := observedDetector(t, binary, f.env)
	var moved detectorObservation
	require.Eventually(t, func() bool {
		groups, describeErr := f.admin.DescribeGroups(t.Context(), f.kafka.DetectorGroup)
		group := groups[f.kafka.DetectorGroup]
		if describeErr != nil || group.Err != nil || len(group.Members) != 3 {
			return false
		}
		for _, observation := range detectorObservations(t, thirdDir) {
			if observation.Boundary == "restored" && observation.Checkpoint != nil &&
				observation.Member != oldOwners[observation.Partition] && oldOwners[observation.Partition] != "" {
				var m struct {
					ProcessedNext int64 `json:"processed_next"`
				}
				if json.Unmarshal([]byte(observation.Checkpoint.Metadata), &m) == nil && m.ProcessedNext == 9 {
					moved = observation
					return true
				}
			}
		}
		return false
	}, 25*time.Second, 25*time.Millisecond, "third replica never restored a seeded target from a different actual owner")
	require.NotEqual(t, oldOwners[moved.Partition], moved.Member)
	f.process = third
	before := recoveryCheckpoint(t, f, moved.Partition, 9)
	require.Equal(t, before, *moved.Checkpoint)
	require.NoError(t, producer.ProduceSync(t.Context(), tenth[moved.Partition]).FirstErr())
	recoveryCheckpoint(t, f, moved.Partition, 10)
	requireRecoveredAlert(t, f, canonical[moved.Partition])
	t.Logf("target partition=%d oldOwner=%s newOwner=%s restored=%+v", moved.Partition,
		oldOwners[moved.Partition], moved.Member, *moved.Checkpoint)
	for _, process := range []*proc{third, second, first} {
		require.Zero(t, process.terminate(t), process.output.String())
	}
}
