import { useQuery } from "@tanstack/react-query";
import { useSession } from "../auth/session";

export default function Overview() {
  const { session, client } = useSession();
  const summary = useQuery({
    queryKey: ["overview", session?.generation],
    queryFn: ({ signal }) => client!.summary(signal),
    enabled: !!session && !!client,
    retry: false,
  });
  const data = summary.data;
  const maximum = data
    ? Math.max(
        1,
        ...data.trend_24h.flatMap((p) => [p.event_count, p.alert_count]),
      )
    : 1;
  const empty =
    data &&
    data.open_alerts === 0 &&
    data.event_rate_per_sec === 0 &&
    data.top_rules.length === 0 &&
    data.trend_24h.every((p) => p.event_count === 0 && p.alert_count === 0);
  return (
    <section aria-labelledby="overview-title">
      <h2 id="overview-title">Overview</h2>
      <p>
        Tenant activity from canonical events and authoritative alert workflows.
      </p>
      {summary.isPending && <p role="status">Loading overview…</p>}
      {summary.isError && (
        <div role="alert">
          <p>Could not load overview. Try again.</p>
          <button onClick={() => void summary.refetch()}>
            Retry loading overview
          </button>
        </div>
      )}
      {data && (
        <>
          <section>
            <h3 id="open-alerts-title">Open alerts</h3>
            <p
              aria-label="Open alerts"
              style={{ fontSize: "28px", fontWeight: 600 }}
            >
              {data.open_alerts}
            </p>
            <dl>
              {data.alerts_by_severity.map((item) => (
                <div key={item.severity}>
                  <dt>{item.severity}</dt>
                  <dd>{item.count}</dd>
                </div>
              ))}
            </dl>
          </section>
          <h3>Event rate</h3>
          <p>
            <span aria-label="Events per second">
              {data.event_rate_per_sec}
            </span>{" "}
            events / second
          </p>
          <p>Unique events in the last 60 seconds, divided by 60.</p>
          {empty && (
            <div className="empty-state">
              <h3>No activity in this period</h3>
              <p>
                Send synthetic events with the documented seed command, then
                refresh.
              </p>
            </div>
          )}
          <h3>24 hour activity</h3>
          <p>
            Completed five-minute UTC buckets. Events and all alerts, including
            acknowledged and resolved alerts.
          </p>
          <svg
            role="img"
            aria-labelledby="activity-title"
            aria-describedby="activity-description"
            viewBox="0 0 600 180"
            style={{ width: "100%", maxWidth: "900px", display: "block" }}
          >
            <title id="activity-title">24 hour tenant activity</title>
            <desc id="activity-description">
              Events in blue and alerts in muted gray. Exact bucket values
              follow in the activity table.
            </desc>
            <polyline
              data-series="events"
              fill="none"
              stroke="var(--accent)"
              strokeWidth="2"
              points={data.trend_24h
                .map(
                  (p, i) =>
                    `${(i * 600) / 287},${170 - (p.event_count / maximum) * 160}`,
                )
                .join(" ")}
            />
            <polyline
              data-series="alerts"
              fill="none"
              stroke="var(--muted)"
              strokeWidth="2"
              strokeDasharray="4 4"
              points={data.trend_24h
                .map(
                  (p, i) =>
                    `${(i * 600) / 287},${170 - (p.alert_count / maximum) * 160}`,
                )
                .join(" ")}
            />
          </svg>
          <p>Solid: events · Dashed: alerts</p>
          <details>
            <summary>View all 288 activity buckets</summary>
            <div
              role="region"
              aria-label="Activity values"
              tabIndex={0}
              style={{ overflowX: "auto" }}
            >
              <table aria-label="24 hour activity">
                <thead>
                  <tr>
                    <th scope="col">Bucket start (UTC)</th>
                    <th scope="col">Events</th>
                    <th scope="col">Alerts</th>
                  </tr>
                </thead>
                <tbody>
                  {data.trend_24h.map((p) => (
                    <tr key={p.bucket_start}>
                      <th scope="row">
                        <time dateTime={p.bucket_start}>{p.bucket_start}</time>
                      </th>
                      <td>{p.event_count}</td>
                      <td>{p.alert_count}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </details>
          <h3>Top rules</h3>
          <p>All alerts in the last 24 hours, ordered by count then rule ID.</p>
          {data.top_rules.length === 0 ? (
            <p>No alerts in the last 24 hours.</p>
          ) : (
            <table aria-label="Top rules">
              <thead>
                <tr>
                  <th scope="col">Rule</th>
                  <th scope="col">Alerts</th>
                </tr>
              </thead>
              <tbody>
                {data.top_rules.map((p) => (
                  <tr key={p.rule_id}>
                    <th scope="row">
                      <code>{p.rule_id}</code>
                    </th>
                    <td>{p.count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <button
            className="secondary"
            disabled={summary.isFetching}
            onClick={() => void summary.refetch()}
          >
            Refresh overview
          </button>
        </>
      )}
      <p>Consumer lag — available in Phase 5</p>
    </section>
  );
}
