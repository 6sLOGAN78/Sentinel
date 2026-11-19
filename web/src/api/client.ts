import createClient from "openapi-fetch";
import type { paths } from "./schema";

export type Session =
  paths["/api/v1/session"]["get"]["responses"][200]["content"]["application/json"];
export type AlertPage =
  paths["/api/v1/alerts"]["get"]["responses"][200]["content"]["application/json"];
export type BackfillPage =
  paths["/api/v1/alerts/backfill"]["get"]["responses"][200]["content"]["application/json"];

function isDeliverySeq(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^(0|[1-9][0-9]{0,18})$/.test(value) &&
    BigInt(value) <= 9223372036854775807n
  );
}
export type AlertFilters = NonNullable<
  paths["/api/v1/alerts"]["get"]["parameters"]["query"]
>;
export type AlertDetail =
  paths["/api/v1/alerts/{alert_id}"]["get"]["responses"][200]["content"]["application/json"];
export type AlertPatch =
  paths["/api/v1/alerts/{alert_id}"]["patch"]["requestBody"]["content"]["application/json"];
export type EventPage =
  paths["/api/v1/events"]["get"]["responses"][200]["content"]["application/json"];
export type EventFilters = NonNullable<
  paths["/api/v1/events"]["get"]["parameters"]["query"]
>;
export type EventDetail =
  paths["/api/v1/events/{event_id}"]["get"]["responses"][200]["content"]["application/json"];

export type SystemStatus =
  paths["/api/v1/system"]["get"]["responses"][200]["content"]["application/json"];
export type ConsumerLag =
  paths["/api/v1/metrics/lag"]["get"]["responses"][200]["content"]["application/json"];
export type SystemMetric = ConsumerLag["consumer_lag"][number]["lag"];

export const LAG_GROUPS = ["event-writer", "detector", "alert-writer"] as const;
export const SYSTEM_SERVICES = [
  "ingest",
  "event-writer",
  "detector",
  "alert-writer",
  "query-api",
] as const;

export class APIError extends Error {
  // serverMessage is kept only so callers can tell refusals apart; it is
  // compared against fixed strings and never rendered.
  constructor(
    readonly status: number,
    readonly serverMessage?: string,
  ) {
    super("The API request failed.");
  }
}

async function forbiddenMessage(response: Response) {
  try {
    const text = await response.text();
    if (text.length > 4096) return undefined;
    const body: unknown = JSON.parse(text);
    const message =
      body && typeof body === "object" && "error" in body
        ? (body.error as { message?: unknown } | null)?.message
        : undefined;
    return typeof message === "string" ? message : undefined;
  } catch {
    return undefined;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

// isMetric enforces the discriminated union: a value only when available,
// a reason and never a value when unavailable.
function isMetric(value: unknown): value is SystemMetric {
  if (!isRecord(value)) return false;
  if (value.state === "available")
    return (
      typeof value.value === "number" &&
      Number.isFinite(value.value) &&
      !("reason" in value)
    );
  return (
    value.state === "unavailable" &&
    typeof value.reason === "string" &&
    !("value" in value)
  );
}

function isTimestamp(value: unknown) {
  return typeof value === "string" && Number.isFinite(Date.parse(value));
}

function isLagRows(rows: unknown) {
  return (
    Array.isArray(rows) &&
    rows.length === LAG_GROUPS.length &&
    LAG_GROUPS.every(
      (group) =>
        rows.filter(
          (row) => isRecord(row) && row.group === group && isMetric(row.lag),
        ).length === 1,
    )
  );
}

function isReadiness(value: unknown) {
  if (!isRecord(value)) return false;
  if (value.state === "unavailable")
    return typeof value.reason === "string" && !("ready_replicas" in value);
  return (
    (value.state === "ready" ||
      value.state === "degraded" ||
      value.state === "not_ready") &&
    Number.isSafeInteger(value.ready_replicas) &&
    Number.isSafeInteger(value.probed_replicas) &&
    (value.ready_replicas as number) >= 0 &&
    (value.probed_replicas as number) >= 0
  );
}

function isService(value: unknown) {
  return (
    isRecord(value) &&
    Number.isSafeInteger(value.expected_replicas) &&
    (value.expected_replicas as number) >= 0 &&
    isMetric(value.replicas_up) &&
    isMetric(value.restarts_15m) &&
    isReadiness(value.readiness)
  );
}

function allMetrics(group: unknown, keys: string[]) {
  return isRecord(group) && keys.every((key) => isMetric(group[key]));
}

function isSystemStatus(value: unknown): value is SystemStatus {
  if (!isRecord(value)) return false;
  const services = value.services;
  const source = value.metrics_source;
  const ingest = value.ingest;
  return (
    isTimestamp(value.observed_at) &&
    (value.grafana_url === undefined ||
      typeof value.grafana_url === "string") &&
    isRecord(source) &&
    (source.state === "available" ||
      (source.state === "unavailable" && typeof source.reason === "string")) &&
    Array.isArray(services) &&
    services.length === SYSTEM_SERVICES.length &&
    SYSTEM_SERVICES.every(
      (name) =>
        services.filter((s) => isRecord(s) && s.name === name && isService(s))
          .length === 1,
    ) &&
    isRecord(ingest) &&
    isMetric(ingest.total) &&
    allMetrics(ingest.by_result, [
      "accepted",
      "rejected",
      "rate_limited",
      "shed",
      "unavailable",
    ]) &&
    allMetrics(value.http_errors, [
      "ratio",
      "status_5xx",
      "status_429",
      "status_503",
    ]) &&
    isMetric(value.dlq_rate) &&
    isLagRows(value.consumer_lag) &&
    allMetrics(value.latency, [
      "detection_delay_p95_seconds",
      "clickhouse_write_p95_seconds",
    ])
  );
}

// One verification attempt/session owns each client. Even a retained old client
// must pass the generation check before it can obtain the active credential.
export function createAPIClient(
  credential: () => string,
  lifetime: AbortSignal,
  isCurrent: () => boolean,
  unauthorized: () => void,
) {
  function assertCurrent() {
    if (lifetime.aborted || !isCurrent())
      throw new DOMException("Session ended", "AbortError");
  }
  const client = createClient<paths>({
    baseUrl: window.location.origin,
    cache: "no-store",
    credentials: "omit",
    redirect: "error",
    fetch: async (request) => {
      assertCurrent();
      const signal = AbortSignal.any([lifetime, request.signal]);
      const headers = new Headers(request.headers);
      headers.set("Authorization", `Bearer ${credential()}`);
      const response = await globalThis.fetch(
        new Request(request, { headers, signal }),
      );
      assertCurrent();
      if (!response.ok) {
        if (response.status === 401) unauthorized();
        if (response.status === 403) {
          const message = await forbiddenMessage(response);
          assertCurrent();
          throw new APIError(403, message);
        }
        throw new APIError(response.status);
      }
      return response;
    },
  });
  return {
    assertActive(signal?: AbortSignal) {
      signal?.throwIfAborted();
      assertCurrent();
    },
    async stream(after: string, signal: AbortSignal) {
      signal.throwIfAborted();
      assertCurrent();
      if (!isDeliverySeq(after)) throw new APIError(400);
      const headers = new Headers({
        Accept: "text/event-stream",
        "Last-Event-ID": after,
      });
      headers.set("Authorization", `Bearer ${credential()}`);
      const response = await globalThis.fetch(
        new Request(new URL("/api/v1/alerts/stream", window.location.origin), {
          headers,
          signal: AbortSignal.any([lifetime, signal]),
          cache: "no-store",
          credentials: "omit",
          redirect: "error",
        }),
      );
      signal.throwIfAborted();
      assertCurrent();
      if (!response.ok) {
        if (response.status === 401) unauthorized();
        throw new APIError(response.status);
      }
      return response;
    },
    async backfill(after = "0", signal?: AbortSignal) {
      signal?.throwIfAborted();
      if (!isDeliverySeq(after)) throw new APIError(400);
      const result = await client.GET("/api/v1/alerts/backfill", {
        params: { query: { after_seq: after, limit: 200 } },
        signal,
      });
      assertCurrent();
      signal?.throwIfAborted();
      const page = result.data;
      if (
        !page ||
        !Array.isArray(page.items) ||
        page.items.length > 200 ||
        typeof page.has_more !== "boolean" ||
        !isDeliverySeq(page.next_seq) ||
        BigInt(page.next_seq) < BigInt(after)
      )
        throw new APIError(503);
      let previous = after;
      for (const item of page.items) {
        if (
          !item ||
          !isDeliverySeq(item.seq) ||
          BigInt(item.seq) <= BigInt(previous) ||
          BigInt(item.seq) > BigInt(page.next_seq) ||
          !item.alert ||
          !/^[0-9a-f]{64}$/.test(item.alert.alert_id) ||
          typeof item.alert.tenant_id !== "string"
        )
          throw new APIError(503);
        previous = item.seq;
      }
      if (page.has_more && (!page.items.length || previous !== page.next_seq))
        throw new APIError(503);
      return page;
    },
    async summary(signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.GET("/api/v1/metrics/summary", { signal });
      assertCurrent();
      signal?.throwIfAborted();
      const summary = result.data;
      if (
        !summary ||
        !Number.isFinite(summary.event_rate_per_sec) ||
        summary.event_rate_per_sec < 0 ||
        !Number.isSafeInteger(summary.open_alerts) ||
        summary.open_alerts < 0 ||
        !Array.isArray(summary.alerts_by_severity) ||
        summary.alerts_by_severity.length !== 4 ||
        !Array.isArray(summary.top_rules) ||
        summary.top_rules.length > 20 ||
        !Array.isArray(summary.trend_24h) ||
        summary.trend_24h.length !== 288
      )
        throw new APIError(503);
      return summary;
    },
    async consumerLag(signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.GET("/api/v1/metrics/lag", { signal });
      assertCurrent();
      signal?.throwIfAborted();
      const lag: unknown = result.data;
      if (
        !isRecord(lag) ||
        !isTimestamp(lag.observed_at) ||
        !isLagRows(lag.consumer_lag)
      )
        throw new APIError(503);
      return lag as ConsumerLag;
    },
    async system(signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.GET("/api/v1/system", { signal });
      assertCurrent();
      signal?.throwIfAborted();
      const status: unknown = result.data;
      if (!isSystemStatus(status)) throw new APIError(503);
      return status;
    },
    async rules(signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.GET("/api/v1/rules", { signal });
      assertCurrent();
      signal?.throwIfAborted();
      if (
        !result.data ||
        !Array.isArray(result.data.rules) ||
        result.data.rules.length > 4
      )
        throw new APIError(503);
      return result.data;
    },
    async transitionAlert(id: string, body: AlertPatch, signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.PATCH("/api/v1/alerts/{alert_id}", {
        params: { path: { alert_id: id } },
        body,
        signal,
      });
      assertCurrent();
      signal?.throwIfAborted();
      const workflow = result.data;
      if (
        !workflow ||
        workflow.alert_id !== id ||
        workflow.status !== body.status ||
        workflow.version !== body.version + 1
      )
        throw new APIError(503);
      return workflow;
    },
    async eventDetail(id: string, signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.GET("/api/v1/events/{event_id}", {
        params: { path: { event_id: id } },
        signal,
      });
      assertCurrent();
      signal?.throwIfAborted();
      const event = result.data;
      if (
        !event ||
        event.event_id !== id ||
        event.schema_version !== 1 ||
        typeof event.tenant_id !== "string" ||
        !event.tenant_id ||
        typeof event.source !== "string" ||
        typeof event.occurred_at !== "string"
      )
        throw new APIError(503);
      return event;
    },
    async events(filters: EventFilters = {}, signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.GET("/api/v1/events", {
        params: { query: filters },
        signal,
      });
      assertCurrent();
      signal?.throwIfAborted();
      const page = result.data;
      const limit = filters.limit ?? 50;
      if (
        !page ||
        !Array.isArray(page.items) ||
        page.items.length > limit ||
        typeof page.has_more !== "boolean" ||
        (page.has_more
          ? typeof page.next_cursor !== "string" ||
            !page.next_cursor ||
            page.next_cursor.length > 2048 ||
            page.items.length !== limit
          : page.next_cursor !== null)
      )
        throw new APIError(503);
      return page;
    },
    async session() {
      const result = await client.GET("/api/v1/session");
      // Parsing may finish after logout even when fetch has returned headers.
      assertCurrent();
      return result.data;
    },
    async alerts(filters: AlertFilters = {}, signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.GET("/api/v1/alerts", {
        params: { query: filters },
        signal,
      });
      assertCurrent();
      signal?.throwIfAborted();
      if (!result.data || !Array.isArray(result.data.items))
        throw new APIError(503);
      return result.data;
    },
    async alertDetail(id: string, signal?: AbortSignal) {
      signal?.throwIfAborted();
      const result = await client.GET("/api/v1/alerts/{alert_id}", {
        params: { path: { alert_id: id } },
        signal,
      });
      assertCurrent();
      signal?.throwIfAborted();
      if (
        !result.data ||
        !Array.isArray(result.data.evidence) ||
        !Array.isArray(result.data.missing_evidence_ids) ||
        !Array.isArray(result.data.history)
      )
        throw new APIError(503);
      return result.data;
    },
  };
}

export type APIClient = ReturnType<typeof createAPIClient>;
