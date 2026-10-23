import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import type { AlertPage } from "../api/client";

const id = "a".repeat(64);
const eventID = "00000000-0000-4000-8000-000000000001";
const malicious = '<img src=x onerror="window.compromised=true">';
const workflow: AlertPage["items"][number] = {
  alert_id: id,
  tenant_id: "detail-tenant",
  created_at: "2026-10-23T00:00:00.000Z",
  updated_at: "2026-10-23T00:00:00.000Z",
  rule_id: "AUTH_BRUTE_FORCE",
  rule_version: 77,
  severity: "high",
  entity_key: "192.0.2.44",
  summary: "Investigate detail",
  event_count: 2,
  window_start: "2026-10-22T23:59:00.000Z",
  window_end: "2026-10-23T00:00:00.000Z",
  status: "open",
  assignee: null,
  version: 1,
};
function detail() {
  return {
    workflow,
    detection: {
      schema_version: 1,
      ...Object.fromEntries(
        Object.entries(workflow).filter(
          ([key]) => !["updated_at", "assignee", "version"].includes(key),
        ),
      ),
      sample_event_ids: [eventID],
      dedupe_key: "recorded-" + "x".repeat(1000),
      summary: malicious,
    },
    evidence: [
      {
        schema_version: 1,
        event_id: eventID,
        tenant_id: workflow.tenant_id,
        occurred_at: workflow.created_at,
        source: "detail-test",
        event_type: "auth.failure",
        attributes: { payload: malicious },
      },
    ],
    missing_evidence_ids: ["00000000-0000-4000-8000-000000000002"],
    rule_explanation:
      "This rule version is unavailable. The detection retains its recorded rule ID and version. AUTH_BRUTE_FORCE version 77.",
    history: [],
  };
}
function signIn() {
  fireEvent.change(screen.getByLabelText("Development access token"), {
    target: { value: "detail-test-credential" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}
async function start(
  read: (request: Request) => Promise<Response> = async () =>
    Response.json(detail()),
) {
  const requests: Request[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/session")
        return Response.json({
          subject: "detail-operator",
          tenant_id: workflow.tenant_id,
          role: "analyst",
          expires_at: new Date(Date.now() + 600000).toISOString(),
        });
      if (path === "/api/v1/alerts")
        return Response.json({
          items: [workflow],
          next_cursor: null,
          has_more: false,
        });
      requests.push(request);
      return read(request);
    }),
  );
  const cache = new QueryClient();
  render(<App queryClient={cache} />);
  signIn();
  await screen.findByText(workflow.summary);
  return { requests, cache };
}
async function open() {
  const trigger = screen.getByRole("button", { name: "Open alert details" });
  trigger.focus();
  fireEvent.click(trigger);
  return {
    trigger,
    dialog: await screen.findByRole("dialog", { name: "Alert details" }),
  };
}

describe("alert evidence details", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/alerts");
    // jsdom has no top layer/focus trap/Escape default. This shim models only
    // open/close; native keyboard containment is checked in real Chromium.
    Object.defineProperties(HTMLDialogElement.prototype, {
      showModal: {
        configurable: true,
        value: function (this: HTMLDialogElement) {
          this.setAttribute("open", "");
        },
      },
      close: {
        configurable: true,
        value: function (this: HTMLDialogElement) {
          this.removeAttribute("open");
        },
      },
    });
  });
  afterEach(() => {
    Reflect.deleteProperty(HTMLDialogElement.prototype, "showModal");
    Reflect.deleteProperty(HTMLDialogElement.prototype, "close");
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    window.history.replaceState(null, "", "/alerts");
  });

  it("opens actual detail flow and renders canonical JSON, retention and recorded rule version as text", async () => {
    const { requests } = await start();
    const { dialog, trigger } = await open();
    await within(dialog).findByText("Some evidence is no longer retained.");
    expect(dialog.textContent).toContain("AUTH_BRUTE_FORCE version 77");
    expect(dialog.textContent).toContain(malicious);
    expect(dialog.textContent).toContain("recorded-" + "x".repeat(1000));
    expect(dialog.querySelector("img")).toBeNull();
    expect(dialog.querySelector("script")).toBeNull();
    expect(dialog.textContent).toContain("No status changes recorded.");
    expect(new URL(requests[0].url).pathname).toBe(`/api/v1/alerts/${id}`);
    expect(new URL(requests[0].url).search).toBe("");
    expect(window.location.search).toContain(`alert_id=${id}`);
    expect(document.activeElement).toBe(
      within(dialog).getByRole("button", { name: "Close alert details" }),
    );
    fireEvent.cancel(dialog);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(trigger);
    expect(new URLSearchParams(window.location.search).has("alert_id")).toBe(
      false,
    );
  });

  it("supports deep links, Back and Forward without replacing the active filter range", async () => {
    window.history.replaceState(null, "", `/alerts?alert_id=${id}&status=open`);
    await start();
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText("Some evidence is no longer retained.");
    expect(new URLSearchParams(window.location.search).get("status")).toBe(
      "open",
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Close alert details" }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    act(() => window.history.back());
    await screen.findByRole("dialog");
    act(() => window.history.forward());
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("announces loading then allows dependency retry and honest empty evidence", async () => {
    let release!: (response: Response) => void;
    let count = 0;
    await start(async () =>
      ++count === 1
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : Response.json({
            ...detail(),
            evidence: [],
            missing_evidence_ids: [],
          }),
    );
    const { dialog } = await open();
    expect(within(dialog).getByText("Loading alert details…")).toBeTruthy();
    await act(async () => release(new Response(null, { status: 503 })));
    await within(dialog).findByText("Could not load alert details. Try again.");
    fireEvent.click(
      within(dialog).getByRole("button", {
        name: "Retry loading alert details",
      }),
    );
    await within(dialog).findByText("No retained sample events.");
    expect(
      within(dialog).queryByText("Some evidence is no longer retained."),
    ).toBeNull();
  });

  it("retains last real detail during failed refresh", async () => {
    let count = 0;
    await start(async () =>
      ++count === 1
        ? Response.json(detail())
        : new Response(null, { status: 503 }),
    );
    const { dialog } = await open();
    await within(dialog).findByText("Some evidence is no longer retained.");
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Refresh alert details" }),
    );
    await within(dialog).findByText("Could not load alert details. Try again.");
    expect(dialog.textContent).toContain(malicious);
  });

  it("aborts detail when closed and discards a body that arrives after sign-out", async () => {
    let release!: (value: unknown) => void;
    const { requests, cache } = await start(async () => {
      const response = Response.json(detail());
      response.json = () =>
        new Promise((resolve) => {
          release = resolve;
        });
      return response;
    });
    const { dialog } = await open();
    await waitFor(() => expect(release).toBeTypeOf("function"));
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Close alert details" }),
    );
    await waitFor(() => expect(requests[0].signal.aborted).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await act(async () => release(detail()));
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.body.textContent).not.toContain(malicious);
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
    expect(window.location.href).not.toContain("detail-test-credential");
  });
});
