import { describe, expect, it } from "vitest";
import type {
  Alert,
  AlertList,
  ErrorEnvelope,
  Event,
  EventList,
} from "./types";

// These literals are checked by tsc against the types generated from the real OpenAPI document.
describe("generated API types", () => {
  it("accepts a minimal event with only required fields", () => {
    const event: Event = {
      schema_version: 1,
      event_id: "11111111-1111-4111-8111-111111111111",
      tenant_id: "acme",
      occurred_at: "2026-01-01T00:00:00Z",
      source: "generator",
      event_type: "auth.failure",
    };
    expect(event.event_type).toBe("auth.failure");
  });

  it("models the error envelope with request_id", () => {
    const err: ErrorEnvelope = {
      error: {
        code: "BAD_REQUEST",
        message: "invalid",
        request_id: "req-1",
        details: [{ field: "events[0].event_id", error: "must be a UUID" }],
      },
    };
    expect(err.error.request_id).toBe("req-1");
    expect(err.error.details?.[0]?.field).toBe("events[0].event_id");
  });

  it("models paginated lists", () => {
    const alert: Alert = {
      schema_version: 1,
      alert_id: "a".repeat(64),
      tenant_id: "acme",
      rule_id: "BRUTE_FORCE",
      rule_version: 1,
      severity: "high",
      status: "open",
      created_at: "2026-01-01T00:00:00Z",
      window_start: "2026-01-01T00:00:00Z",
      window_end: "2026-01-01T00:05:00Z",
      entity_key: "198.51.100.7",
      summary: "many failures",
      event_count: 5,
      sample_event_ids: [],
      dedupe_key: "k",
    };
    expect(alert.dedupe_key).toBe("k");
    const workflow: AlertList["items"][number] = {
      alert_id: "a".repeat(64),
      tenant_id: "acme",
      rule_id: "BRUTE_FORCE",
      rule_version: 1,
      severity: "high",
      status: "open",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      window_start: "2026-01-01T00:00:00Z",
      window_end: "2026-01-01T00:05:00Z",
      entity_key: "198.51.100.7",
      summary: "many failures",
      event_count: 5,
      assignee: null,
      version: 1,
    };
    const alerts: AlertList = {
      items: [workflow],
      next_cursor: null,
      has_more: false,
    };
    const events: EventList = { items: [], next_cursor: "c", has_more: true };
    expect(alerts.items).toHaveLength(1);
    expect(events.has_more).toBe(true);
  });
});
