import createClient from "openapi-fetch";
import type { paths } from "./schema";

export type Session =
  paths["/api/v1/session"]["get"]["responses"][200]["content"]["application/json"];
export type AlertPage =
  paths["/api/v1/alerts"]["get"]["responses"][200]["content"]["application/json"];
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

export class APIError extends Error {
  constructor(readonly status: number) {
    super("The API request failed.");
  }
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
        throw new APIError(response.status);
      }
      return response;
    },
  });
  return {
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
