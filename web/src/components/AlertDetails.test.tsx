import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { createAPIClient } from "../api/client";
import type { AlertDetail, AlertPage } from "../api/client";

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
function detail(): AlertDetail {
  return {
    workflow,
    detection: {
      schema_version: 1,
      alert_id: id,
      tenant_id: workflow.tenant_id,
      created_at: workflow.created_at,
      rule_id: workflow.rule_id,
      rule_version: workflow.rule_version,
      severity: workflow.severity,
      status: "open",
      entity_key: workflow.entity_key,
      event_count: workflow.event_count,
      window_start: workflow.window_start,
      window_end: workflow.window_end,
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
  strict = false,
  role = "analyst",
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
          role,
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
  render(
    strict ? (
      <StrictMode>
        <App queryClient={cache} />
      </StrictMode>
    ) : (
      <App queryClient={cache} />
    ),
  );
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
    cleanup();
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
    expect(dialog.textContent).toContain(
      JSON.stringify(malicious).slice(1, -1),
    );
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
    fireEvent(dialog, new Event("cancel", { cancelable: true }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(trigger);
    expect(new URLSearchParams(window.location.search).has("alert_id")).toBe(
      false,
    );
  });

  it("acknowledges through the real session/client with pending disable and committed history", async () => {
    let finish!: (response: Response) => void;
    let acknowledged = false;
    const { requests } = await start(async (request) => {
      if (request.method === "PATCH") {
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      }
      return Response.json(
        acknowledged
          ? {
              ...detail(),
              workflow: { ...workflow, status: "acknowledged", version: 2 },
              history: [
                {
                  from_status: "open",
                  to_status: "acknowledged",
                  actor: "detail-operator",
                  changed_at: "2026-10-24T01:00:00Z",
                },
              ],
            }
          : detail(),
      );
    });
    const { dialog } = await open();
    const button = await within(dialog).findByRole("button", {
      name: "Acknowledge alert",
    });
    fireEvent.click(button);
    await waitFor(() =>
      expect((button as HTMLButtonElement).disabled).toBe(true),
    );
    const patches = requests.filter((request) => request.method === "PATCH");
    expect(patches).toHaveLength(1);
    expect(await patches[0].clone().json()).toEqual({
      status: "acknowledged",
      version: 1,
    });
    expect(patches[0].credentials).toBe("omit");
    acknowledged = true;
    await act(async () =>
      finish(
        Response.json({ ...workflow, status: "acknowledged", version: 2 }),
      ),
    );
    await within(dialog).findByText("Status: acknowledged · Version: 2");
    await within(dialog).findByText(/open → acknowledged · detail-operator/);
    expect(
      within(dialog).queryByRole("button", { name: "Acknowledge alert" }),
    ).toBeNull();
  });

  it("refetches conflict and requires an explicit new action without automatic PATCH retry", async () => {
    let reads = 0;
    const { requests } = await start(async (request) => {
      if (request.method === "PATCH")
        return new Response(null, { status: 409 });
      reads++;
      return Response.json({
        ...detail(),
        workflow: { ...workflow, version: reads },
      });
    });
    const { dialog } = await open();
    fireEvent.click(
      await within(dialog).findByRole("button", { name: "Acknowledge alert" }),
    );
    await within(dialog).findByText(
      "This alert changed. Review the latest details before acting again.",
    );
    await within(dialog).findByText("Status: open · Version: 2");
    expect(
      requests.filter((request) => request.method === "PATCH"),
    ).toHaveLength(1);
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Acknowledge alert" }),
    );
    await waitFor(() =>
      expect(
        requests.filter((request) => request.method === "PATCH"),
      ).toHaveLength(2),
    );
    expect(
      await requests
        .filter((request) => request.method === "PATCH")[1]
        .clone()
        .json(),
    ).toEqual({ status: "acknowledged", version: 2 });
  });

  it("confirms resolve with exact copy, cancels and escapes without PATCH, then commits once", async () => {
    let finish!: (response: Response) => void;
    let resolved = false;
    const { requests } = await start(async (request) => {
      if (request.method === "PATCH")
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      return Response.json({
        ...detail(),
        workflow: {
          ...workflow,
          status: resolved ? "resolved" : "acknowledged",
          version: resolved ? 3 : 2,
        },
        history: [
          {
            from_status: "open",
            to_status: "acknowledged",
            actor: "detail-operator",
            changed_at: workflow.updated_at,
          },
          ...(resolved
            ? [
                {
                  from_status: "acknowledged",
                  to_status: "resolved",
                  actor: malicious,
                  changed_at: workflow.updated_at,
                },
              ]
            : []),
        ],
      });
    });
    const { dialog } = await open();
    const resolve = await within(dialog).findByRole("button", {
      name: "Resolve alert",
      exact: true,
    });
    fireEvent.click(resolve);
    let confirmation = await screen.findByRole("dialog", {
      name: "Resolve alert?",
      exact: true,
    });
    expect(
      within(confirmation).getByText(
        "This closes the alert. Reopening is not supported in v1.",
      ),
    ).toBeTruthy();
    const cancel = within(confirmation).getByRole("button", {
      name: "Return to alert",
    });
    expect(document.activeElement).toBe(cancel);
    fireEvent.click(cancel);
    expect(screen.queryByRole("dialog", { name: "Resolve alert?" })).toBeNull();
    expect(document.activeElement).toBe(resolve);
    fireEvent.click(resolve);
    confirmation = await screen.findByRole("dialog", {
      name: "Resolve alert?",
    });
    fireEvent(confirmation, new Event("cancel", { cancelable: true }));
    expect(screen.queryByRole("dialog", { name: "Resolve alert?" })).toBeNull();
    expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(0);
    fireEvent.click(resolve);
    confirmation = await screen.findByRole("dialog", {
      name: "Resolve alert?",
    });
    const submit = within(confirmation).getByRole("button", {
      name: "Resolve alert",
      exact: true,
    });
    fireEvent.click(submit);
    await waitFor(() =>
      expect((submit as HTMLButtonElement).disabled).toBe(true),
    );
    fireEvent.click(submit);
    const patches = requests.filter((r) => r.method === "PATCH");
    expect(patches).toHaveLength(1);
    expect(await patches[0].clone().json()).toEqual({
      status: "resolved",
      version: 2,
    });
    resolved = true;
    await act(async () =>
      finish(Response.json({ ...workflow, status: "resolved", version: 3 })),
    );
    await within(dialog).findByText("Status: resolved · Version: 3");
    await within(dialog).findByText(/acknowledged → resolved/);
    expect(dialog.querySelector("img")).toBeNull();
    expect(screen.queryByRole("dialog", { name: "Resolve alert?" })).toBeNull();
    expect(
      within(dialog).queryByRole("button", { name: "Resolve alert" }),
    ).toBeNull();
  });

  it("refetches resolve conflict and requires a new confirmation with the latest version", async () => {
    let reads = 0;
    const { requests } = await start(async (request) => {
      if (request.method === "PATCH")
        return new Response(null, { status: 409 });
      return Response.json({
        ...detail(),
        workflow: { ...workflow, status: "acknowledged", version: ++reads + 1 },
      });
    });
    const { dialog } = await open();
    fireEvent.click(
      await within(dialog).findByRole("button", {
        name: "Resolve alert",
        exact: true,
      }),
    );
    fireEvent.click(
      within(
        await screen.findByRole("dialog", { name: "Resolve alert?" }),
      ).getByRole("button", { name: "Resolve alert", exact: true }),
    );
    await within(dialog).findByText(
      "This alert changed. Review the latest details before acting again.",
    );
    await within(dialog).findByText("Status: acknowledged · Version: 3");
    expect(screen.queryByRole("dialog", { name: "Resolve alert?" })).toBeNull();
    expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(1);
    fireEvent.click(
      within(dialog).getByRole("button", {
        name: "Resolve alert",
        exact: true,
      }),
    );
    expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(1);
    fireEvent.click(
      within(
        await screen.findByRole("dialog", { name: "Resolve alert?" }),
      ).getByRole("button", { name: "Resolve alert", exact: true }),
    );
    await waitFor(() =>
      expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(2),
    );
    expect(
      await requests
        .filter((r) => r.method === "PATCH")[1]
        .clone()
        .json(),
    ).toEqual({ status: "resolved", version: 3 });
  });

  it("hides resolve from verified viewers and closes confirmation on navigation without PATCH", async () => {
    const read = async () =>
      Response.json({
        ...detail(),
        workflow: { ...workflow, status: "acknowledged", version: 2 },
      });
    let run = await start(read, false, "viewer");
    let opened = await open();
    await within(opened.dialog).findByText("Status: acknowledged · Version: 2");
    expect(
      within(opened.dialog).queryByRole("button", { name: "Resolve alert" }),
    ).toBeNull();
    expect(run.requests.filter((r) => r.method === "PATCH")).toHaveLength(0);
    cleanup();
    run = await start(read);
    opened = await open();
    fireEvent.click(
      await within(opened.dialog).findByRole("button", {
        name: "Resolve alert",
        exact: true,
      }),
    );
    await screen.findByRole("dialog", { name: "Resolve alert?" });
    act(() => window.history.back());
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(run.requests.filter((r) => r.method === "PATCH")).toHaveLength(0);
  });

  it("aborts resolve on navigation and rejects late resolved bodies after session disposal", async () => {
    let finish!: (text: string) => void;
    const { requests, cache } = await start(async (request) => {
      if (request.method !== "PATCH")
        return Response.json({
          ...detail(),
          workflow: { ...workflow, status: "acknowledged", version: 2 },
        });
      const response = Response.json({
        ...workflow,
        status: "resolved",
        version: 3,
      });
      response.text = () =>
        new Promise((resolve) => {
          finish = resolve;
        });
      return response;
    });
    const { dialog } = await open();
    fireEvent.click(
      await within(dialog).findByRole("button", {
        name: "Resolve alert",
        exact: true,
      }),
    );
    fireEvent.click(
      within(
        await screen.findByRole("dialog", { name: "Resolve alert?" }),
      ).getByRole("button", { name: "Resolve alert", exact: true }),
    );
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    act(() => window.history.back());
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(requests.find((r) => r.method === "PATCH")?.signal.aborted).toBe(
      true,
    );
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await act(async () =>
      finish(JSON.stringify({ ...workflow, status: "resolved", version: 3 })),
    );
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(document.body.textContent).not.toContain("resolved");
    expect(localStorage.length + sessionStorage.length).toBe(0);
  });

  it("hides mutation for verified viewers", async () => {
    const { requests } = await start(undefined, false, "viewer");
    const { dialog } = await open();
    await within(dialog).findByText("No status changes recorded.");
    expect(
      within(dialog).queryByRole("button", { name: "Acknowledge alert" }),
    ).toBeNull();
    expect(
      requests.filter((request) => request.method === "PATCH"),
    ).toHaveLength(0);
  });

  it("aborts pending PATCH on close and discards a parsed mutation body after sign-out", async () => {
    let finish!: (text: string) => void;
    const { requests, cache } = await start(async (request) => {
      const response = Response.json(
        request.method === "PATCH"
          ? { ...workflow, status: "acknowledged", version: 2 }
          : detail(),
      );
      if (request.method === "PATCH")
        response.text = () =>
          new Promise((resolve) => {
            finish = resolve;
          });
      return response;
    });
    const { dialog } = await open();
    fireEvent.click(
      await within(dialog).findByRole("button", { name: "Acknowledge alert" }),
    );
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Close alert details" }),
    );
    await waitFor(() =>
      expect(
        requests.find((request) => request.method === "PATCH")?.signal.aborted,
      ).toBe(true),
    );
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await act(async () =>
      finish(
        JSON.stringify({ ...workflow, status: "acknowledged", version: 2 }),
      ),
    );
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(document.body.textContent).not.toContain("Alert acknowledged.");
    expect(window.localStorage.length + window.sessionStorage.length).toBe(0);
    expect(window.location.href).not.toContain("detail-test-credential");
  });

  it("revokes the actual session on PATCH401 without retry", async () => {
    const { requests } = await start(async (request) =>
      request.method === "PATCH"
        ? new Response(null, { status: 401 })
        : Response.json(detail()),
    );
    const { dialog } = await open();
    fireEvent.click(
      await within(dialog).findByRole("button", { name: "Acknowledge alert" }),
    );
    await screen.findByRole("heading", { name: "Sign in to Sentinel" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      requests.filter((request) => request.method === "PATCH"),
    ).toHaveLength(1);
  });

  it.each(["caller", "session"])(
    "guards parsed PATCH bodies after %s cancellation",
    async (kind) => {
      let finish!: (text: string) => void;
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => {
          const response = Response.json({
            ...workflow,
            status: "acknowledged",
            version: 2,
          });
          response.text = () =>
            new Promise((resolve) => {
              finish = resolve;
            });
          return response;
        }),
      );
      const lifetime = new AbortController(),
        caller = new AbortController();
      let current = true;
      const unauthorized = vi.fn();
      const client = createAPIClient(
        () => "guard-test-credential",
        lifetime.signal,
        () => current,
        unauthorized,
      );
      const result = client
        .transitionAlert(
          id,
          { status: "acknowledged", version: 1 },
          caller.signal,
        )
        .catch((failure: unknown) => failure);
      await waitFor(() => expect(finish).toBeTypeOf("function"));
      if (kind === "caller") caller.abort();
      else {
        current = false;
        lifetime.abort();
      }
      finish(
        JSON.stringify({ ...workflow, status: "acknowledged", version: 2 }),
      );
      expect(await result).toMatchObject({ name: "AbortError" });
      expect(unauthorized).not.toHaveBeenCalled();
    },
  );

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

  it("retains the selected deep-link URL during StrictMode effect replay", async () => {
    window.history.replaceState(null, "", `/alerts?alert_id=${id}`);
    await start(undefined, true);
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText("Some evidence is no longer retained.");
    expect(new URLSearchParams(window.location.search).get("alert_id")).toBe(
      id,
    );
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
    expect(dialog.textContent).toContain(
      JSON.stringify(malicious).slice(1, -1),
    );
  });

  it("aborts detail when closed and discards a body that arrives after sign-out", async () => {
    let release!: (value: unknown) => void;
    const { requests, cache } = await start(async () => {
      const response = Response.json(detail());
      response.text = () =>
        new Promise((resolve) => {
          release = (value) => resolve(JSON.stringify(value));
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

  it("revokes the current session on detail401 and rejects invalid deep-link IDs without a detail request", async () => {
    window.history.replaceState(
      null,
      "",
      "/alerts?alert_id=" + "x".repeat(4096),
    );
    const { requests } = await start(
      async () => new Response(null, { status: 401 }),
    );
    expect(requests).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
    await open();
    await screen.findByRole("heading", { name: "Sign in to Sentinel" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(new URLSearchParams(window.location.search).has("alert_id")).toBe(
      false,
    );
  });

  it.each(["caller", "session"])(
    "guards parsed detail bodies after %s cancellation",
    async (kind) => {
      let finish!: (text: string) => void;
      let parsing = false;
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => {
          const response = Response.json(detail());
          response.text = () => {
            parsing = true;
            return new Promise((resolve) => {
              finish = resolve;
            });
          };
          return response;
        }),
      );
      const lifetime = new AbortController(),
        caller = new AbortController();
      let current = true;
      const unauthorized = vi.fn();
      const client = createAPIClient(
        () => "guard-test-credential",
        lifetime.signal,
        () => current,
        unauthorized,
      );
      const result = client
        .alertDetail(id, caller.signal)
        .catch((failure: unknown) => failure);
      await waitFor(() => expect(parsing).toBe(true));
      if (kind === "caller") caller.abort();
      else {
        current = false;
        lifetime.abort();
      }
      finish(JSON.stringify(detail()));
      expect(await result).toMatchObject({ name: "AbortError" });
      expect(unauthorized).not.toHaveBeenCalled();
    },
  );
});
