package clickhouse

import (
	"context"
	"fmt"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/6sLOGAN78/sentinel/internal/query"
)

// SummarizeEvents selects the complete canonical identity before applying any
// occurrence-time predicate. Later representations cannot move an event into
// the rate window or trend. At most 289 grouped rows cross the native boundary.
func (s *QueryEventStore) SummarizeEvents(
	ctx context.Context, tenant string, now time.Time,
) (query.EventSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	end := now.UTC().Truncate(query.SummaryBucketWidth)
	rows, err := s.conn.Query(boundedQueryContext(ctx), `SELECT
	 toStartOfInterval(occurred_at, INTERVAL 5 MINUTE, 'UTC') AS bucket,
	 count(),countIf(occurred_at >= fromUnixTimestamp64Nano({rate:Int64},'UTC'))
	 FROM (`+uniqueEventRowsSQL+`) WHERE
	 occurred_at >= fromUnixTimestamp64Nano({start:Int64},'UTC') AND
	 occurred_at < fromUnixTimestamp64Nano({now:Int64},'UTC')
	 GROUP BY bucket ORDER BY bucket`, driverclickhouse.Named("tenant", tenant),
		driverclickhouse.Named("rate", now.Add(-time.Minute).UnixNano()),
		driverclickhouse.Named("start", end.Add(-24*time.Hour).UnixNano()),
		driverclickhouse.Named("now", now.UnixNano()))
	if err != nil {
		return query.EventSummary{}, fmt.Errorf("event summary: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := query.EventSummary{}
	for rows.Next() {
		var at time.Time
		var count, rate uint64
		if err = rows.Scan(&at, &count, &rate); err != nil {
			return query.EventSummary{}, fmt.Errorf("scan event summary: %w", err)
		}
		result.RateCount += rate
		if at.Before(end) {
			result.Trend = append(result.Trend, query.SummaryCount{Start: at.UTC(), Count: count})
		}
	}
	if err = rows.Err(); err != nil {
		return query.EventSummary{}, fmt.Errorf("read event summary: %w", err)
	}
	return result, nil
}
