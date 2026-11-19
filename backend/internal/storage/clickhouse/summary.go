package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/6sLOGAN78/sentinel/internal/query"
)

const (
	// maxSummaryConflicts bounds the identities whose rows disagree on
	// occurred_at (client misuse, ADR 0002) that one summary may correct.
	// Exceeding it is reported as unavailable, never as a partial summary.
	maxSummaryConflicts = 10_000
	// summaryMainComment and summaryCorrectionComment tag the two round trips
	// in system.query_log (log_comment); the caller's query id is kept.
	summaryMainComment       = "sentinel.summarize_events.main"
	summaryCorrectionComment = "sentinel.summarize_events.correction"
	// summaryWindowSQL bounds a winner's occurred_at to [start, now).
	summaryWindowSQL = `occurred_at >= fromUnixTimestamp64Nano({start:Int64},'UTC') AND
 occurred_at < fromUnixTimestamp64Nano({now:Int64},'UTC')`
	summaryRateSQL = `fromUnixTimestamp64Nano({rate:Int64},'UTC')`
)

// summaryMainSQL is round trip 1. One GROUP BY event_id over the tenant reads
// only tenant_id, event_id and occurred_at. Only identities that can have a
// winner in [start, now) survive (hi >= start AND lo < now; for lo = hi that
// is lo in [start, now)). Identities with lo = hi are counted in their
// 5-minute bucket; identities with lo != hi fall in the NULL bucket, whose
// count is the conflict total and whose array holds at most
// maxSummaryConflicts ids. The array cap is a Go constant, never caller input.
func summaryMainSQL() string {
	return `SELECT if(lo = hi, toStartOfInterval(lo, INTERVAL 5 MINUTE, 'UTC'), NULL) AS bucket,
 count(), countIf(lo = hi AND lo >= ` + summaryRateSQL + `),
 groupArrayIf(` + strconv.Itoa(maxSummaryConflicts) + `)(event_id, lo != hi)
 FROM (
 SELECT event_id, min(occurred_at) AS lo, max(occurred_at) AS hi
 FROM events WHERE tenant_id={tenant:String} GROUP BY event_id
 ) WHERE hi >= fromUnixTimestamp64Nano({start:Int64},'UTC')
 AND lo < fromUnixTimestamp64Nano({now:Int64},'UTC')
 GROUP BY bucket ORDER BY bucket`
}

// summaryCorrectionSQL is round trip 2: the unchanged canonical argMin, run
// only for the conflicting ids of the same tenant.
const summaryCorrectionSQL = `SELECT
 toStartOfInterval(occurred_at, INTERVAL 5 MINUTE, 'UTC') AS bucket,
 count(), countIf(occurred_at >= ` + summaryRateSQL + `)
 FROM (` + canonicalRowsPrefixSQL + `AND event_id IN {ids:Array(String)} GROUP BY tenant_id,event_id
 )) WHERE ` + summaryWindowSQL + `
 GROUP BY bucket ORDER BY bucket`

// ErrSummaryConflictsExceeded means more conflicting identities than
// maxSummaryConflicts could affect the summary; the query API maps it to 503.
var ErrSummaryConflictsExceeded = errors.New("event summary conflicting identities exceeded")

// summaryAccumulator merges per-bucket counts from both round trips.
type summaryAccumulator struct {
	buckets map[int64]query.SummaryCount
	rate    uint64
}

func (a *summaryAccumulator) add(at time.Time, count, rate uint64) {
	key := at.UnixNano()
	bucket, ok := a.buckets[key]
	if !ok {
		bucket.Start = at.UTC()
	}
	bucket.Count += count
	a.buckets[key] = bucket
	a.rate += rate
}

// result keeps only buckets before end, ascending, like the trend contract.
func (a *summaryAccumulator) result(end time.Time) query.EventSummary {
	result := query.EventSummary{RateCount: a.rate}
	for _, bucket := range a.buckets {
		if bucket.Start.Before(end) {
			result.Trend = append(result.Trend, bucket)
		}
	}
	slices.SortFunc(result.Trend, func(x, y query.SummaryCount) int { return x.Start.Compare(y.Start) })
	return result
}

// SummarizeEvents counts canonical identities whose winner occurred_at lies
// in the rate window and the 24 h trend, exactly equal to applying those
// predicates after whole-tenant dedupe, without a whole-tenant argMin.
//
// Exactness: the winner of an identity is one of its rows, so its
// occurred_at lies in [lo, hi] (the identity's min and max occurred_at). When
// lo = hi every row shares that time, so the winner's occurred_at is lo
// whatever the tie-break, and the main pass buckets it from three columns.
// When lo != hi the winner can only land in [start, now) if hi >= start and
// lo < now; exactly those ids get the canonical argMin in a second round trip
// that buckets the winners the same way. The two sets are disjoint and
// together cover every identity that can be counted.
//
// Snapshot: the result reflects the main pass's read point. Rows inserted
// between the round trips may be missing; events are append-only and the
// correction only refines identities the main pass already found
// conflicting, so it never adds an identity. More than maxSummaryConflicts
// conflicting identities return ErrSummaryConflictsExceeded.
func (s *QueryEventStore) SummarizeEvents(
	ctx context.Context, tenant string, now time.Time,
) (query.EventSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	end := now.UTC().Truncate(query.SummaryBucketWidth)
	args := []any{driverclickhouse.Named("tenant", tenant),
		driverclickhouse.Named("rate", now.Add(-time.Minute).UnixNano()),
		driverclickhouse.Named("start", end.Add(-24*time.Hour).UnixNano()),
		driverclickhouse.Named("now", now.UnixNano())}
	acc := &summaryAccumulator{buckets: map[int64]query.SummaryCount{}}
	conflicts, err := s.summaryMain(ctx, args, acc)
	if err != nil {
		return query.EventSummary{}, err
	}
	if len(conflicts) > 0 {
		args = append(args, driverclickhouse.Named("ids", conflicts))
		if err = s.summaryCorrection(ctx, args, acc); err != nil {
			return query.EventSummary{}, err
		}
	}
	return acc.result(end), nil
}

// summaryQueryContext bounds one round trip and tags it in query_log.
func summaryQueryContext(ctx context.Context, comment string) context.Context {
	return driverclickhouse.Context(ctx, driverclickhouse.WithSettings(driverclickhouse.Settings{
		"max_execution_time": int(queryTimeout / time.Second),
		"log_comment":        comment,
	}))
}

// summaryMain runs round trip 1, adds the non-conflicting buckets and returns
// the conflicting ids.
func (s *QueryEventStore) summaryMain(
	ctx context.Context, args []any, acc *summaryAccumulator,
) ([]string, error) {
	rows, err := s.conn.Query(summaryQueryContext(ctx, summaryMainComment), summaryMainSQL(), args...)
	if err != nil {
		return nil, fmt.Errorf("event summary: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var conflicts []string
	for rows.Next() {
		var at *time.Time
		var count, rate uint64
		var ids []string
		if err = rows.Scan(&at, &count, &rate, &ids); err != nil {
			return nil, fmt.Errorf("scan event summary: %w", err)
		}
		if at != nil {
			acc.add(*at, count, rate)
			continue
		}
		if count > maxSummaryConflicts {
			return nil, fmt.Errorf("event summary with %d conflicting identities: %w",
				count, ErrSummaryConflictsExceeded)
		}
		conflicts = ids
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read event summary: %w", err)
	}
	return conflicts, nil
}

// summaryCorrection runs round trip 2 and adds the conflicting winners.
func (s *QueryEventStore) summaryCorrection(ctx context.Context, args []any, acc *summaryAccumulator) error {
	rows, err := s.conn.Query(summaryQueryContext(ctx, summaryCorrectionComment), summaryCorrectionSQL, args...)
	if err != nil {
		return fmt.Errorf("event summary correction: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var at time.Time
		var count, rate uint64
		if err = rows.Scan(&at, &count, &rate); err != nil {
			return fmt.Errorf("scan event summary correction: %w", err)
		}
		acc.add(at, count, rate)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("read event summary correction: %w", err)
	}
	return nil
}
