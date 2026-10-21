//go:build integration

package test_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/detection"
)

func TestDetectorIncorporatedOwnerSnapshot(t *testing.T) {
	// Obtain real assignment wrappers from Kafka rather than inventing the
	// private kadm representation. The schedules below control observations only.
	topic := "detector-owner-snapshot-" + uuid.NewString()
	groupID := "detector-owner-snapshot-" + uuid.NewString()
	client, err := kgo.NewClient(kgo.SeedBrokers(kafkaCfg.Brokers))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	admin := kadm.NewClient(client)
	created, err := admin.CreateTopics(t.Context(), 12, 1, nil, topic)
	require.NoError(t, err)
	require.NoError(t, created[topic].Err)
	for range 2 {
		consumer, consumerErr := kgo.NewClient(kgo.SeedBrokers(kafkaCfg.Brokers), kgo.ConsumerGroup(groupID),
			kgo.ConsumeTopics(topic), kgo.Balancers(kgo.RangeBalancer()), kgo.DisableAutoCommit())
		require.NoError(t, consumerErr)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			for ctx.Err() == nil {
				consumer.PollFetches(ctx)
			}
		}()
		t.Cleanup(func() {
			cancel()
			consumer.Close()
			<-done
		})
	}
	var group kadm.DescribedGroup
	require.Eventually(t, func() bool {
		groups, describeErr := admin.DescribeGroups(t.Context(), groupID)
		group = groups[groupID]
		if describeErr != nil || group.Err != nil || group.State != "Stable" || len(group.Members) != 2 {
			return false
		}
		for _, member := range group.Members {
			assignment, ok := member.Assigned.AsConsumer()
			if !ok || len(assignment.Topics) != 1 || len(assignment.Topics[0].Partitions) != 6 {
				return false
			}
		}
		return true
	}, 15*time.Second, 25*time.Millisecond)

	t.Run("complete current snapshot", func(t *testing.T) {
		owners := make(map[int32]string)
		require.True(t, collectIncorporatedOwners(owners, group, topic, ownerSnapshotObservations(group)))
		require.Len(t, owners, 12)
		for partition, member := range owners {
			require.True(t, groupOwns(group, member, topic, partition))
		}
	})
	t.Run("two members but only one incorporated owner", func(t *testing.T) {
		first, _ := group.Members[0].Assigned.AsConsumer()
		second, _ := group.Members[1].Assigned.AsConsumer()
		firstTopics, secondTopics := first.Topics, second.Topics
		defer func() { first.Topics, second.Topics = firstTopics, secondTopics }()
		first.Topics = nil
		second.Topics = []kmsg.ConsumerMemberAssignmentTopic{{Topic: topic,
			Partitions: []int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}}}
		require.False(t, collectIncorporatedOwners(make(map[int32]string), group, topic,
			ownerSnapshotObservations(group)), "a member count does not prove a retained live window")
	})
	t.Run("previous poll cannot supply a missing current owner", func(t *testing.T) {
		owners := make(map[int32]string)
		observations := ownerSnapshotObservations(group)
		require.True(t, collectIncorporatedOwners(owners, group, topic, observations))
		observations[0] = nil // The current owner's commit observation is not yet visible.
		require.False(t, collectIncorporatedOwners(owners, group, topic, observations),
			"poll history must not stand in for a complete current snapshot")
	})
	t.Run("rebalance still in progress", func(t *testing.T) {
		changing := group
		changing.State = "CompletingRebalance"
		require.False(t, collectIncorporatedOwners(make(map[int32]string), changing, topic,
			ownerSnapshotObservations(group)))
	})
	t.Run("incomplete current checkpoint coverage", func(t *testing.T) {
		observations := ownerSnapshotObservations(group)
		observations[0] = observations[0][1:]
		require.False(t, collectIncorporatedOwners(make(map[int32]string), group, topic, observations))
	})
	t.Run("historical observations from the wrong owner", func(t *testing.T) {
		observations := ownerSnapshotObservations(group)
		observations[0][0].Member = group.Members[1].MemberID
		require.False(t, collectIncorporatedOwners(make(map[int32]string), group, topic, observations))
	})
}

func ownerSnapshotObservations(group kadm.DescribedGroup) [][]detectorObservation {
	observations := make([][]detectorObservation, len(group.Members))
	for i, member := range group.Members {
		assignment, _ := member.Assigned.AsConsumer()
		for _, topic := range assignment.Topics {
			for _, partition := range topic.Partitions {
				observations[i] = append(observations[i], detectorObservation{
					Boundary: "committed", Topic: topic.Topic, Partition: partition, Member: member.MemberID,
					Checkpoint: &detection.Checkpoint{Metadata: `{"processed_next":9}`},
				})
			}
		}
	}
	return observations
}
