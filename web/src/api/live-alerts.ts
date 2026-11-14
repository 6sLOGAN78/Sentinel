import { fetchEventSource } from "@microsoft/fetch-event-source";
import { APIError } from "./client";
import type { APIClient, BackfillPage } from "./client";

type Delivery = BackfillPage["items"][number];
export type LiveState = "connecting" | "live" | "retrying" | "stopped";
const cursors = new WeakMap<APIClient, string>();
const workflowKeys = [
  "alert_id",
  "tenant_id",
  "created_at",
  "updated_at",
  "rule_id",
  "rule_version",
  "severity",
  "entity_key",
  "summary",
  "event_count",
  "window_start",
  "window_end",
  "status",
  "assignee",
  "version",
];

function decimal(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^(0|[1-9][0-9]{0,18})$/.test(value) &&
    BigInt(value) <= 9223372036854775807n
  );
}
function object(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function validDelivery(value: unknown, tenant: string): value is Delivery {
  if (
    !object(value) ||
    Object.keys(value).length !== 2 ||
    !decimal(value.seq) ||
    value.seq === "0" ||
    !object(value.alert)
  )
    return false;
  const a = value.alert;
  return (
    Object.keys(a).length === workflowKeys.length &&
    workflowKeys.every((key) => Object.hasOwn(a, key)) &&
    typeof a.alert_id === "string" &&
    /^[0-9a-f]{64}$/.test(a.alert_id) &&
    a.tenant_id === tenant &&
    ["created_at", "updated_at", "window_start", "window_end"].every(
      (key) =>
        typeof a[key] === "string" &&
        /Z$/.test(a[key]) &&
        Number.isFinite(Date.parse(a[key])),
    ) &&
    ["rule_id", "entity_key", "summary"].every(
      (key) => typeof a[key] === "string",
    ) &&
    ["rule_version", "event_count", "version"].every(
      (key) => Number.isSafeInteger(a[key]) && (a[key] as number) > 0,
    ) &&
    ["low", "medium", "high", "critical"].includes(a.severity as string) &&
    ["open", "acknowledged", "resolved"].includes(a.status as string) &&
    (a.assignee === null || typeof a.assignee === "string")
  );
}

function wait(signal: AbortSignal, delay: number) {
  return new Promise<void>((resolve) => {
    if (signal.aborted) return resolve();
    const finish = () => {
      window.clearTimeout(timer);
      signal.removeEventListener("abort", finish);
      resolve();
    };
    const timer = window.setTimeout(finish, delay);
    signal.addEventListener("abort", finish, { once: true });
  });
}

// Exactly one owner sequences REST pages and SSE frames for this mounted
// generation. The library never retries itself or owns the committed cursor.
export async function receiveLiveAlerts(
  client: APIClient,
  tenant: string,
  signal: AbortSignal,
  process: (
    items: Delivery[],
    through: string,
    initial: boolean,
  ) => Promise<void>,
  status: (state: LiveState) => void,
) {
  // The first complete catch-up walk of this mounted generation is the load
  // baseline: its pages are history, not arrivals. Everything after it is new.
  let baseline = true;
  let backoff = 250;
  let recoveringStream = false;
  while (!signal.aborted) {
    let attached = false;
    try {
      client.assertActive(signal);
      status("connecting");
      for (;;) {
        const page = await client.backfill(cursors.get(client) ?? "0", signal);
        client.assertActive(signal);
        if (!page.items.every((item) => validDelivery(item, tenant)))
          throw new APIError(503);
        await process(page.items, page.next_seq, baseline);
        client.assertActive(signal);
        cursors.set(client, page.next_seq);
        if (!recoveringStream) backoff = 250;
        if (!page.has_more) break;
      }
      baseline = false;
      attached = true;
      const attempt = new AbortController();
      const combined = AbortSignal.any([signal, attempt.signal]);
      let processing = Promise.resolve();
      let queued = 0;
      let processingError: unknown;
      let transportError: unknown;
      try {
        await fetchEventSource("/api/v1/alerts/stream", {
          signal: combined,
          openWhenHidden: true,
          fetch: async (_input, init) =>
            client.stream(
              cursors.get(client) ?? "0",
              AbortSignal.any([combined, init!.signal!]),
            ),
          async onopen(response) {
            client.assertActive(combined);
            if (
              response.headers.get("Content-Type")?.split(";")[0] !==
              "text/event-stream"
            )
              throw new APIError(503);
            status("live");
            backoff = 250;
            recoveringStream = false;
          },
          onmessage(message) {
            client.assertActive(combined);
            if (!message.data && !message.id && !message.event) return;
            if (message.event !== "alert" || !decimal(message.id))
              throw new APIError(503);
            const item: unknown = JSON.parse(message.data);
            if (!validDelivery(item, tenant) || item.seq !== message.id)
              throw new APIError(503);
            if (BigInt(item.seq) <= BigInt(cursors.get(client) ?? "0")) return;
            if (queued >= 32) throw new APIError(503);
            queued++;
            processing = processing
              .then(async () => {
                if (processingError) return;
                client.assertActive(combined);
                if (BigInt(item.seq) <= BigInt(cursors.get(client) ?? "0"))
                  return;
                await process([item], item.seq, false);
                client.assertActive(combined);
                cursors.set(client, item.seq);
              })
              .catch((error) => {
                processingError = error;
                attempt.abort();
              })
              .finally(() => {
                queued--;
              });
          },
          onclose() {
            throw new APIError(503);
          },
          onerror(error) {
            throw error;
          },
        });
      } catch (error) {
        transportError = error;
      }
      await processing;
      client.assertActive(signal);
      if (processingError) throw processingError;
      if (transportError) throw transportError;
      throw new APIError(503);
    } catch (error) {
      if (
        signal.aborted ||
        (error instanceof DOMException && error.name === "AbortError") ||
        (error instanceof APIError && error.status === 401)
      )
        return;
      if (
        error instanceof APIError &&
        error.status < 500 &&
        error.status !== 429
      ) {
        status("stopped");
        return;
      }
      status("retrying");
      if (attached) recoveringStream = true;
      // Preserve the existing REST recovery schedule. Stream failures add
      // bounded jitter to avoid synchronizing replicas' reconnecting clients.
      await wait(
        signal,
        attached ? backoff * (0.5 + Math.random() * 0.5) : backoff,
      );
      backoff = Math.min(backoff * 2, 5000);
    }
  }
}
