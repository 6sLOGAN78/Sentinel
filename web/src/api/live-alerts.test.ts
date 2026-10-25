import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { createElement } from "react";
import { createAPIClient } from "./client";

const workflow = {
  alert_id: "a".repeat(64),
  tenant_id: "stream-tenant",
  created_at: "2026-10-21T12:00:00Z",
  updated_at: "2026-10-21T12:00:00Z",
  rule_id: "AUTH_BRUTE_FORCE",
  rule_version: 1,
  severity: "high",
  entity_key: "192.0.2.44",
  summary: "Retained snapshot",
  event_count: 2,
  window_start: "2026-10-21T11:59:00Z",
  window_end: "2026-10-21T12:00:00Z",
  status: "open",
  assignee: null,
  version: 1,
};
const encoder = new TextEncoder();
async function mounted(
  streamResponse?: (request: Request, attempt: number) => Response,
) {
  window.history.replaceState(null, "", "/alerts?severity=high");
  const streams: Request[] = [],
    backfills: Request[] = [];
  const bodies: ReadableStreamDefaultController<Uint8Array>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/session")
        return Response.json({
          subject: "reader",
          tenant_id: workflow.tenant_id,
          role: "viewer",
          expires_at: new Date(Date.now() + 600000).toISOString(),
        });
      if (path === "/api/v1/alerts/backfill") {
        backfills.push(request);
        return Response.json({
          items: [],
          next_seq:
            backfills.length === 1
              ? "9007199254740993"
              : request.url.includes("9007199254740994")
                ? "9007199254740994"
                : "9007199254740993",
          has_more: false,
        });
      }
      if (path === "/api/v1/alerts/stream") {
        streams.push(request);
        return (
          streamResponse?.(request, streams.length) ??
          new Response(
            new ReadableStream({
              start(c) {
                bodies.push(c);
              },
            }),
            { headers: { "Content-Type": "text/event-stream" } },
          )
        );
      }
      return Response.json({
        items: [workflow],
        next_cursor: null,
        has_more: false,
      });
    }),
  );
  const view = render(createElement(App));
  fireEvent.change(screen.getByLabelText("Development access token"), {
    target: { value: "stream-private-credential" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await screen.findByText(workflow.summary);
  return { streams, backfills, bodies, view };
}
function frame(seq = "9007199254740994", alert: unknown = workflow) {
  return `id: ${seq}\nevent: alert\ndata: ${JSON.stringify({ seq, alert })}\n\n`;
}
describe("live delivery through the actual guarded client and App", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    window.history.replaceState(null, "", "/alerts");
  });
  it("attaches after REST with exact header cursor, processes live arrivals without appending or changing filters, and aborts on unmount", async () => {
    const { streams, bodies, backfills, view } = await mounted();
    await waitFor(() => expect(streams).toHaveLength(1));
    expect(backfills).toHaveLength(1);
    expect(streams[0].headers.get("Authorization")).toBe(
      "Bearer stream-private-credential",
    );
    expect(streams[0].headers.get("Last-Event-ID")).toBe("9007199254740993");
    expect(new URL(streams[0].url).search).toBe("");
    await act(async () => bodies[0].enqueue(encoder.encode(frame())));
    await screen.findByText("New alerts available");
    expect(screen.getAllByText(workflow.summary)).toHaveLength(1);
    expect(new URL(window.location.href).searchParams.get("severity")).toBe(
      "high",
    );
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    view.unmount();
    expect(streams[0].signal.aborted).toBe(true);
  });
  it("reconnects through REST from the processed decimal frame and visibly deduplicates replay", async () => {
    const { streams, bodies, backfills } = await mounted();
    await waitFor(() => expect(streams).toHaveLength(1));
    await act(async () => bodies[0].enqueue(encoder.encode(frame())));
    await screen.findByText("New alerts available");
    await act(async () => bodies[0].close());
    await waitFor(() => expect(streams).toHaveLength(2), { timeout: 4000 });
    expect(new URL(backfills[1].url).searchParams.get("after_seq")).toBe(
      "9007199254740994",
    );
    expect(streams[1].headers.get("Last-Event-ID")).toBe("9007199254740994");
    await act(async () => bodies[1].enqueue(encoder.encode(frame())));
    expect(screen.getAllByText(workflow.summary)).toHaveLength(1);
  });
  it.each([
    [
      "foreign tenant",
      frame("9007199254740994", { ...workflow, tenant_id: "foreign" }),
    ],
    ["missing workflow", frame("9007199254740994", {})],
    [
      "numeric cursor",
      'id: 9007199254740994\nevent: alert\ndata: {"seq":9007199254740994,"alert":{}}\n\n',
    ],
    ["wrong event", frame().replace("event: alert", "event: unexpected")],
    [
      "ID mismatch",
      frame().replace("id: 9007199254740994", "id: 9007199254740995"),
    ],
    ["malformed JSON", "id: 9007199254740994\nevent: alert\ndata: {\n\n"],
    [
      "unsafe version",
      frame("9007199254740994", { ...workflow, version: 9007199254740992 }),
    ],
  ])("rejects %s before cursor advance", async (_, bytes) => {
    const { streams, bodies, backfills } = await mounted();
    await waitFor(() => expect(streams).toHaveLength(1));
    await act(async () => bodies[0].enqueue(encoder.encode(bytes)));
    await waitFor(() => expect(backfills).toHaveLength(2), { timeout: 4000 });
    expect(new URL(backfills[1].url).searchParams.get("after_seq")).toBe(
      "9007199254740993",
    );
    expect(screen.queryByText("New alerts available")).toBeNull();
  });
  it("ignores post-logout frames and never retries an ended generation", async () => {
    const { streams, bodies, backfills } = await mounted();
    await waitFor(() => expect(streams).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(streams[0].signal.aborted).toBe(true);
    await act(async () => {
      bodies[0].enqueue(encoder.encode(frame()));
      bodies[0].close();
    });
    expect(screen.queryByText("New alerts available")).toBeNull();
    expect(backfills).toHaveLength(1);
  });
  it("stops current401 with session disposal and no retry", async () => {
    const { streams } = await mounted(
      () => new Response(null, { status: 401 }),
    );
    await screen.findByRole("heading", { name: "Sign in to Sentinel" });
    expect(streams).toHaveLength(1);
  });
  it("retries503 with bounded jitter and REST recovery before every attach", async () => {
    const timestamps: number[] = [];
    const { streams, backfills } = await mounted(() => {
      timestamps.push(Date.now());
      return new Response(null, { status: 503 });
    });
    await waitFor(() => expect(streams.length).toBeGreaterThanOrEqual(3), {
      timeout: 5000,
    });
    expect(backfills.length).toBe(streams.length);
    expect(timestamps[1] - timestamps[0]).toBeGreaterThanOrEqual(125);
    expect(timestamps[1] - timestamps[0]).toBeLessThan(1500);
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(streams.every((request) => request.signal.aborted)).toBe(true);
  });
  it.each(["caller", "lifetime", "generation"])(
    "guards stream credential access on %s disposal",
    async (kind) => {
      const lifetime = new AbortController(),
        caller = new AbortController();
      let current = true;
      const credential = vi.fn(() => "private-test-credential"),
        unauthorized = vi.fn();
      const client = createAPIClient(
        credential,
        lifetime.signal,
        () => current,
        unauthorized,
      );
      expect("stream" in client).toBe(true);
      const stream = (
        client as unknown as {
          stream: (after: string, signal: AbortSignal) => Promise<Response>;
        }
      ).stream;
      if (kind === "caller") caller.abort();
      else if (kind === "lifetime") lifetime.abort();
      else current = false;
      await expect(stream("0", caller.signal)).rejects.toMatchObject({
        name: "AbortError",
      });
      expect(credential).not.toHaveBeenCalled();
    },
  );
  it.each([true, false])(
    "checks post-response generation before current401 disposal: %s",
    async (currentAtResponse) => {
      const lifetime = new AbortController();
      let current = true;
      let release!: (response: Response) => void;
      vi.stubGlobal(
        "fetch",
        vi.fn(
          () =>
            new Promise<Response>((resolve) => {
              release = resolve;
            }),
        ),
      );
      const unauthorized = vi.fn();
      const client = createAPIClient(
        () => "private",
        lifetime.signal,
        () => current,
        unauthorized,
      );
      expect("stream" in client).toBe(true);
      const pending = (
        client as unknown as {
          stream: (after: string, signal: AbortSignal) => Promise<Response>;
        }
      ).stream("0", new AbortController().signal);
      current = currentAtResponse;
      release(new Response(null, { status: 401 }));
      await expect(pending).rejects.toMatchObject(
        current ? { status: 401 } : { name: "AbortError" },
      );
      expect(unauthorized).toHaveBeenCalledTimes(current ? 1 : 0);
    },
  );
});
