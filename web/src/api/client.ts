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
