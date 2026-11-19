import {
  useEffect,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { useSession } from "../auth/session";
import {
  APIError,
  LAG_GROUPS,
  SYSTEM_SERVICES,
  type SystemMetric,
  type SystemStatus,
} from "../api/client";

const OPERATOR_REFUSAL = "System status is limited to operator administrators.";
const STALE_AFTER_MS = 60000;
const STALE_CHECK_MS = 5000;
const NARROW_QUERY = "(max-width: 599px)";
const MS_PER_SECOND = 1000;

const rate = new Intl.NumberFormat("en-US", {
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});
const integer = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });
const twoDecimals = new Intl.NumberFormat("en-US", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});
const percent = new Intl.NumberFormat("en-US", {
  style: "percent",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const formatRate = (v: number) => rate.format(v);
const formatEventRate = (v: number) => `${rate.format(v)} events/s`;
const formatRequestRate = (v: number) => `${rate.format(v)} requests/s`;
const formatInteger = (v: number) => integer.format(v);
const formatRatio = (v: number) => percent.format(v);
function formatDuration(seconds: number) {
  const ms = seconds * MS_PER_SECOND;
  return ms < MS_PER_SECOND - 0.5
    ? `${integer.format(ms)} ms`
    : `${twoDecimals.format(seconds)} s`;
}
const utcTime = (iso: string) => new Date(iso).toISOString().slice(11, 19);

function isNarrow() {
  return window.matchMedia?.(NARROW_QUERY).matches ?? false;
}
function subscribeToWidth(notify: () => void) {
  const media = window.matchMedia?.(NARROW_QUERY);
  media?.addEventListener("change", notify);
  return () => media?.removeEventListener("change", notify);
}

function safeGrafana(url: string | undefined) {
  if (!url) return undefined;
  try {
    const protocol = new URL(url).protocol;
    return protocol === "http:" || protocol === "https:" ? url : undefined;
  } catch {
    return undefined;
  }
}

function isRefusal(error: unknown): error is APIError {
  return error instanceof APIError && error.status === 403;
}

// Reason codes map to fixed copy; anything but no_data reads "Unavailable".
function MetricValue({
  metric,
  format,
}: {
  metric: SystemMetric;
  format: (value: number) => string;
}) {
  if (metric.state === "available") return <>{format(metric.value)}</>;
  return (
    <span className="badge">
      {metric.reason === "no_data" ? "No data yet" : "Unavailable"}
    </span>
  );
}

type Service = SystemStatus["services"][number];

function ReadinessBadge({ readiness }: { readiness: Service["readiness"] }) {
  switch (readiness.state) {
    case "ready":
      return <span className="badge badge-success">Ready</span>;
    case "degraded":
      return <span className="badge badge-warning">Degraded</span>;
    case "not_ready":
      return <span className="badge badge-critical">Not ready</span>;
    default:
      return <span className="badge">Unavailable</span>;
  }
}

function Replicas({ service }: { service: Service }) {
  const up = service.replicas_up;
  if (up.state !== "available")
    return <MetricValue metric={up} format={formatInteger} />;
  return (
    <>
      {service.expected_replicas > 0
        ? `${formatInteger(up.value)} / ${formatInteger(service.expected_replicas)}`
        : formatInteger(up.value)}
    </>
  );
}

function headline(services: Service[]) {
  const states = services.map((s) => s.readiness.state);
  if (states.every((s) => s === "unavailable"))
    return {
      text: "Service health unavailable",
      badge: "Unavailable",
      className: "badge",
    };
  const notReady = states.filter((s) => s !== "ready").length;
  if (notReady === 0)
    return {
      text: "All services ready",
      badge: "Ready",
      className: "badge badge-success",
    };
  const critical = states.includes("not_ready");
  return {
    text: `${notReady} of ${services.length} services not ready`,
    badge: critical ? "Not ready" : "Degraded",
    className: critical ? "badge badge-critical" : "badge badge-warning",
  };
}

function restartLine(services: Service[]) {
  let total = 0;
  for (const service of services) {
    if (service.restarts_15m.state !== "available")
      return "Restart count unavailable";
    total += service.restarts_15m.value;
  }
  const count = Math.round(total);
  return `${formatInteger(count)} ${count === 1 ? "restart" : "restarts"} in the last 15 minutes`;
}

function prometheusMetrics(data: SystemStatus): SystemMetric[] {
  return [
    ...data.services.flatMap((s) => [s.replicas_up, s.restarts_15m]),
    data.ingest.total,
    ...Object.values(data.ingest.by_result),
    ...Object.values(data.http_errors),
    data.dlq_rate,
    ...data.consumer_lag.map((row) => row.lag),
    ...Object.values(data.latency),
  ];
}

function MetricCard({
  label,
  metric,
  format,
  unit,
  children,
}: {
  label: string;
  metric: SystemMetric;
  format: (value: number) => string;
  unit: string;
  children?: ReactNode;
}) {
  return (
    <div className="metric-card">
      <dl>
        <dt>{label}</dt>
        <dd className="metric-value">
          <MetricValue metric={metric} format={format} />
        </dd>
        <dd className="metric-unit">{unit}</dd>
      </dl>
      {children}
    </div>
  );
}

function Breakdown({
  rows,
  format,
}: {
  rows: [string, SystemMetric][];
  format: (value: number) => string;
}) {
  return (
    <dl className="metric-pairs">
      {rows.map(([label, metric]) => (
        <Pair key={label} label={label}>
          <MetricValue metric={metric} format={format} />
        </Pair>
      ))}
    </dl>
  );
}

function Pair({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </>
  );
}

function Forbidden({ operator }: { operator: boolean }) {
  return operator ? (
    <div className="empty-state">
      <h3>Operator access required</h3>
      <p>
        System status is limited to operator administrators. Your tenant is not
        an operator tenant.
      </p>
    </div>
  ) : (
    <div className="empty-state">
      <h3>Admin access required</h3>
      <p>
        System health shows pipeline activity across all tenants. Ask an admin
        to review it.
      </p>
    </div>
  );
}

function ServicesSection({ services }: { services: Service[] }) {
  const narrow = useSyncExternalStore(subscribeToWidth, isNarrow);
  return (
    <section className="system-section" aria-labelledby="system-services-title">
      <h3 id="system-services-title">Services</h3>
      {narrow ? (
        <ul className="mobile-alerts" aria-labelledby="system-services-title">
          {services.map((service) => (
            <li key={service.name}>
              <p className="identifier">
                <strong>{service.name}</strong>
              </p>
              <dl>
                <dt>Replicas</dt>
                <dd>
                  <Replicas service={service} />
                </dd>
                <dt>Readiness</dt>
                <dd>
                  <ReadinessBadge readiness={service.readiness} />
                </dd>
                <dt>Restarts</dt>
                <dd>
                  <MetricValue
                    metric={service.restarts_15m}
                    format={formatInteger}
                  />
                </dd>
              </dl>
            </li>
          ))}
        </ul>
      ) : (
        <div className="table-region">
          <table aria-labelledby="system-services-title">
            <thead>
              <tr>
                <th scope="col">Service</th>
                <th scope="col">Replicas (up / expected)</th>
                <th scope="col">Readiness</th>
                <th scope="col">Restarts (15 min)</th>
              </tr>
            </thead>
            <tbody>
              {services.map((service) => (
                <tr key={service.name}>
                  <th scope="row" className="identifier">
                    {service.name}
                  </th>
                  <td className="numeric">
                    <Replicas service={service} />
                  </td>
                  <td>
                    <ReadinessBadge readiness={service.readiness} />
                  </td>
                  <td className="numeric">
                    <MetricValue
                      metric={service.restarts_15m}
                      format={formatInteger}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function SystemData({ data }: { data: SystemStatus }) {
  const services = SYSTEM_SERVICES.map((name) =>
    data.services.find((s) => s.name === name)!,
  );
  const lagRows = LAG_GROUPS.map((group) =>
    data.consumer_lag.find((row) => row.group === group)!,
  );
  const status = headline(services);
  const sourceDown = data.metrics_source.state === "unavailable";
  const nothingRecorded = prometheusMetrics(data).every(
    (m) => m.state === "unavailable" && m.reason === "no_data",
  );
  const { by_result: results } = data.ingest;
  const errors = data.http_errors;
  return (
    <>
      <section
        className="metric-card system-section"
        aria-labelledby="system-status-headline"
      >
        <div className="status-row">
          <h3 id="system-status-headline">{status.text}</h3>
          <span className={status.className}>{status.badge}</span>
        </div>
        <p>{restartLine(services)}</p>
      </section>
      {sourceDown ? (
        <p className="notice">
          Metrics source unavailable. Service readiness is shown where
          available. Try refreshing in a moment.
        </p>
      ) : (
        nothingRecorded && (
          <div className="empty-state system-section">
            <h3>No metrics recorded yet</h3>
            <p>
              Services have not reported metrics yet. Start the observability
              stack with the documented command, send synthetic events, then
              refresh.
            </p>
          </div>
        )
      )}
      <ServicesSection services={services} />
      <section
        className="system-section"
        aria-labelledby="system-throughput-title"
      >
        <h3 id="system-throughput-title">Throughput and errors</h3>
        <div className="metric-grid">
          <MetricCard
            label="Ingest events"
            metric={data.ingest.total}
            format={formatRate}
            unit="events/s, 1 min rate"
          >
            <Breakdown
              format={formatEventRate}
              rows={[
                ["Accepted", results.accepted],
                ["Rejected", results.rejected],
                ["Rate limited", results.rate_limited],
                ["Shed", results.shed],
                ["Dependency unavailable", results.unavailable],
              ]}
            />
          </MetricCard>
          <MetricCard
            label="HTTP error ratio"
            metric={errors.ratio}
            format={formatRatio}
            unit="5 min"
          >
            <Breakdown
              format={formatRequestRate}
              rows={[
                ["5xx", errors.status_5xx],
                ["429 rate limited", errors.status_429],
                ["503 unavailable", errors.status_503],
              ]}
            />
          </MetricCard>
          <MetricCard
            label="DLQ events"
            metric={data.dlq_rate}
            format={formatRate}
            unit="events/s, 5 min rate"
          />
        </div>
      </section>
      <section className="system-section" aria-labelledby="system-lag-title">
        <h3 id="system-lag-title">Consumer lag</h3>
        <p id="system-lag-caption" className="page-description">
          Position-based: end offset minus next unprocessed offset, maximum
          across partitions.
        </p>
        <div className="table-region">
          <table
            aria-labelledby="system-lag-title"
            aria-describedby="system-lag-caption"
          >
            <thead>
              <tr>
                <th scope="col">Consumer group</th>
                <th scope="col">Lag (records)</th>
              </tr>
            </thead>
            <tbody>
              {lagRows.map((row) => (
                <tr key={row.group}>
                  <th scope="row" className="identifier">
                    {row.group}
                  </th>
                  <td className="numeric">
                    <MetricValue metric={row.lag} format={formatInteger} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
      <section
        className="system-section"
        aria-labelledby="system-latency-title"
      >
        <h3 id="system-latency-title">Latency</h3>
        <div className="metric-grid">
          <MetricCard
            label="Detection delay p95"
            metric={data.latency.detection_delay_p95_seconds}
            format={formatDuration}
            unit="5 min window"
          />
          <MetricCard
            label="ClickHouse write p95"
            metric={data.latency.clickhouse_write_p95_seconds}
            format={formatDuration}
            unit="5 min window"
          />
        </div>
      </section>
      <p className="access-note system-section">
        Values come from Prometheus with fixed server-side queries. Use Grafana
        for history.
      </p>
    </>
  );
}

export default function System() {
  const { session, client } = useSession();
  const isAdmin = !!session && session.role === "admin";
  const system = useQuery({
    queryKey: ["system", session?.generation],
    queryFn: ({ signal }) => client!.system(signal),
    enabled: isAdmin && !!client,
    retry: false,
    // Poll every 15 s while visible; a 403 is final, so stop polling it.
    refetchInterval: (query) => (isRefusal(query.state.error) ? false : 15000),
    refetchIntervalInBackground: false,
  });
  // Staleness uses the client's last successful fetch, not server time.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), STALE_CHECK_MS);
    return () => window.clearInterval(timer);
  }, []);
  const data = system.data;
  const refused = !isAdmin || isRefusal(system.error);
  const stale =
    !refused &&
    !!data &&
    (system.isError || now - system.dataUpdatedAt > STALE_AFTER_MS);
  // The polite region announces transitions only, never routine polls.
  const [announcement, setAnnouncement] = useState("");
  const [wasStale, setWasStale] = useState(false);
  if (stale !== wasStale) {
    setWasStale(stale);
    setAnnouncement(stale ? "Live refresh failed" : "Live refresh restored");
  }
  const grafana = safeGrafana(data?.grafana_url);
  const retry = () => void system.refetch();
  const refresh = () =>
    void system.refetch().then((result) => {
      if (result.status === "success") setAnnouncement("System status updated");
    });
  let content: ReactNode;
  if (refused)
    content = (
      <Forbidden
        operator={
          isRefusal(system.error) &&
          system.error.serverMessage === OPERATOR_REFUSAL
        }
      />
    );
  else if (data)
    content = (
      <>
        <div className="toolbar">
          <time dateTime={data.observed_at}>
            {`Updated ${utcTime(data.observed_at)} UTC`}
          </time>
          <button disabled={system.isFetching} onClick={refresh}>
            Refresh system status
          </button>
          {grafana && (
            <a href={grafana} target="_blank" rel="noopener noreferrer">
              Open Grafana dashboards{" "}
              <span className="visually-hidden">(opens in a new tab)</span>
            </a>
          )}
        </div>
        {stale && (
          <div className="notice notice-warning">
            <p>{`Showing data from ${utcTime(data.observed_at)} UTC. Live refresh failed.`}</p>
            <button className="secondary" onClick={retry}>
              Retry loading system status
            </button>
          </div>
        )}
        <SystemData data={data} />
      </>
    );
  else if (system.isError)
    content = (
      <div role="alert">
        <p>Could not load system status. Try again.</p>
        <button onClick={retry}>Retry loading system status</button>
      </div>
    );
  else content = <p role="status">Loading system status…</p>;
  return (
    <section className="system-page" aria-labelledby="system-title">
      <h2 id="system-title">System</h2>
      <p className="page-description">
        Pipeline health, throughput, consumer lag and errors across all
        services.
      </p>
      <p id="system-live" role="status" className="visually-hidden">
        {announcement}
      </p>
      {content}
    </section>
  );
}
