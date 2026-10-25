import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import App from "./App";

function identity(tenant = "cache-tenant", lifetime = 600000) {
  return {
    subject: "verified-operator",
    tenant_id: tenant,
    role: "viewer",
    expires_at: new Date(Date.now() + lifetime).toISOString(),
  };
}

function workflowPage(summary = "Cached tenant alert") {
  return {
    items: [
      {
        alert_id: "b".repeat(64),
        tenant_id: "cache-tenant",
        created_at: "2026-10-21T12:00:00Z",
        updated_at: "2026-10-21T12:00:00Z",
        rule_id: "AUTH_BRUTE_FORCE",
        rule_version: 1,
        severity: "high",
        entity_key: "192.0.2.10",
        summary,
        event_count: 10,
        window_start: "2026-10-21T11:59:00Z",
        window_end: "2026-10-21T12:00:00Z",
        status: "open",
        assignee: null,
        version: 1,
      },
    ],
    next_cursor: null,
    has_more: false,
  };
}

function signIn(credential = "memory-credential") {
  fireEvent.change(screen.getByLabelText("Development access token"), {
    target: { value: credential },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}

const backfillRequests: Request[] = [];
function stubFetch(responder: (request: Request) => Promise<Response>) {
  vi.stubGlobal("fetch", vi.fn((request: Request) => {
    if (new URL(request.url).pathname === "/api/v1/alerts/backfill") {
      backfillRequests.push(request);
      return Promise.resolve(Response.json({items: [], next_seq: "0", has_more: false}));
    }
    return responder(request);
  }));
}
function defaultAlertFilters(requestIndex: number) {
  const request = vi.mocked(fetch).mock.calls.filter(([request]) => new URL((request as Request).url).pathname !== "/api/v1/alerts/backfill")[requestIndex][0];
  expect(request).toBeInstanceOf(Request);
  const url = new URL((request as Request).url);
  expect(url.pathname).toBe("/api/v1/alerts");
  expect([...url.searchParams.keys()].sort()).toEqual(["from", "limit", "to"]);
  expect(url.searchParams.get("limit")).toBe("50");
  const from = url.searchParams.get("from")!;
  const to = url.searchParams.get("to")!;
  for (const value of [from, to]) {
    expect(value).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
    expect(Number.isFinite(Date.parse(value))).toBe(true);
    expect(new Date(value).toISOString()).toBe(value);
  }
  expect(Date.parse(to) - Date.parse(from)).toBe(86400000);
  return { from, to, limit: 50 };
}

describe("App shell", () => {
  beforeEach(() => { backfillRequests.length = 0; window.history.replaceState(null, "", "/alerts"); });
  afterEach(() => {
    window.history.replaceState(null, "", "/");
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("dispatches authenticated catch-up separately from list fixtures", async () => {
    const fetchSpy = vi.fn().mockResolvedValueOnce(Response.json(identity())).mockResolvedValueOnce(Response.json(workflowPage()));
    stubFetch(fetchSpy);
    render(<App />);
    signIn();
    await screen.findByText("Cached tenant alert");
    await waitFor(() => expect(backfillRequests).toHaveLength(1));
    expect(fetchSpy).toHaveBeenCalledTimes(2);
    expect(new URL(backfillRequests[0].url).searchParams.get("after_seq")).toBe("0");
    expect(backfillRequests[0].headers.get("Authorization")).toBe("Bearer memory-credential");
    fireEvent.click(screen.getByRole("button", {name: "Sign out"}));
    expect(backfillRequests[0].signal.aborted).toBe(true);
  });

  it("shows the development sign-in form with a masked token", () => {
    render(<App />);
    expect(
      screen.getByRole("heading", { name: "Sign in to Sentinel" }),
    ).toBeTruthy();
    expect(
      screen.getByLabelText("Development access token").getAttribute("type"),
    ).toBe("password");
  });

  it("verifies server identity before reading typed workflows and clears data on sign out", async () => {
    let verify: (response: Response) => void = () => {};
    const identity = new Promise<Response>((resolve) => {
      verify = resolve;
    });
    const fetchSpy = vi
      .fn()
      .mockReturnValueOnce(identity)
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            items: [
              {
                alert_id: "a".repeat(64),
                tenant_id: "verified-tenant",
                created_at: "2026-10-21T12:00:00Z",
                updated_at: "2026-10-21T12:00:00Z",
                rule_id: "AUTH_BRUTE_FORCE",
                rule_version: 1,
                severity: "high",
                entity_key: "192.0.2.10",
                summary: "Real workflow alert",
                event_count: 10,
                window_start: "2026-10-21T11:59:00Z",
                window_end: "2026-10-21T12:00:00Z",
                status: "open",
                assignee: null,
                version: 1,
              },
            ],
            next_cursor: null,
            has_more: false,
          }),
        ),
      );
    stubFetch(fetchSpy);
    render(<App />);
    const input = screen.getByLabelText(
      "Development access token",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "operator-token" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(new URL(fetchSpy.mock.calls[0][0].url).pathname).toBe(
      "/api/v1/session",
    );
    expect(screen.queryByText("verified-tenant")).toBeNull();
    verify(
      new Response(
        JSON.stringify({
          subject: "server-operator",
          tenant_id: "verified-tenant",
          role: "viewer",
          expires_at: new Date(Date.now() + 600000).toISOString(),
        }),
      ),
    );
    expect(await screen.findByText("Real workflow alert")).toBeTruthy();
    expect(screen.getByText(/verified-tenant/)).toBeTruthy();
    expect(input.value).toBe("");
    expect(new URL(fetchSpy.mock.calls[1][0].url).pathname).toBe(
      "/api/v1/alerts",
    );
    for (const [request] of fetchSpy.mock.calls) {
      expect(request.headers.get("Authorization")).toBe(
        "Bearer operator-token",
      );
      expect(request.signal).toBeInstanceOf(AbortSignal);
      expect(request.url).not.toContain("operator-token");
      expect(request.cache).toBe("no-store");
      expect(request.credentials).toBe("omit");
    }
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    expect(document.body.textContent).not.toContain("operator-token");
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(screen.queryByText("Real workflow alert")).toBeNull();
    expect(screen.getByLabelText("Development access token")).toBeTruthy();
    expect(fetchSpy.mock.calls[1][0].signal.aborted).toBe(true);
  });

  it("rejects an unverified identity and never loads alerts", async () => {
    const fetchSpy = vi
      .fn()
      .mockResolvedValue(new Response("{}", { status: 401 }));
    stubFetch(fetchSpy);
    render(<App />);
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: "invalid-token" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "This access token could not be verified. Check it or generate a new token.",
    );
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });

  it.each(["503", "network", "HTML", "invalid identity"])(
    "retries %s sign-in availability failures with the same memory-only credential",
    async (failure) => {
      const fetchSpy = vi.fn();
      if (failure === "network")
        fetchSpy.mockRejectedValueOnce(new TypeError("offline"));
      else
        fetchSpy.mockResolvedValueOnce(
          new Response(failure === "HTML" ? "<html>upstream</html>" : "{}", {
            status: failure === "503" ? 503 : 200,
          }),
        );
      fetchSpy
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              subject: "operator",
              tenant_id: "retry-tenant",
              role: "viewer",
              expires_at: new Date(Date.now() + 600000).toISOString(),
            }),
          ),
        )
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              items: [],
              next_cursor: null,
              has_more: false,
            }),
          ),
        );
      stubFetch(fetchSpy);
      render(<App />);
      const credential = "memory-only-retry-credential";
      const input = screen.getByLabelText(
        "Development access token",
      ) as HTMLInputElement;
      fireEvent.change(input, { target: { value: credential } });
      fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
      expect(await screen.findByRole("alert")).toHaveProperty(
        "textContent",
        "Could not sign in. The service is unavailable. Try again.",
      );
      expect(input.value).toBe("");
      expect(document.body.innerHTML).not.toContain(credential);
      expect(localStorage.length + sessionStorage.length).toBe(0);
      fireEvent.click(screen.getByRole("button", { name: "Retry sign in" }));
      expect(
        await screen.findByText("No alerts match these filters"),
      ).toBeTruthy();
      expect(screen.getByText(/retry-tenant/)).toBeTruthy();
      expect(fetchSpy.mock.calls[1][0].headers.get("Authorization")).toBe(
        `Bearer ${credential}`,
      );
      expect(
        screen.queryByRole("button", { name: "Retry sign in" }),
      ).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
      expect(screen.queryByText(/retry-tenant/)).toBeNull();
      expect(
        screen.queryByRole("button", { name: "Retry sign in" }),
      ).toBeNull();
    },
  );

  it("cancels pending verification and ignores a stale identity after another sign-in", async () => {
    let finish: (response: Response) => void = () => {};
    const old = new Promise<Response>((resolve) => {
      finish = resolve;
    });
    const fetchSpy = vi
      .fn()
      .mockReturnValueOnce(old)
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            subject: "new",
            tenant_id: "new-tenant",
            role: "analyst",
            expires_at: new Date(Date.now() + 600000).toISOString(),
          }),
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ items: [], next_cursor: null, has_more: false }),
        ),
      );
    stubFetch(fetchSpy);
    render(<App />);
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: "old-credential" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel sign in" }));
    expect(fetchSpy.mock.calls[0][0].signal.aborted).toBe(true);
    expect(screen.queryByRole("button", { name: "Retry sign in" })).toBeNull();
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: "new-credential" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(
      await screen.findByText("No alerts match these filters"),
    ).toBeTruthy();
    await act(async () =>
      finish(
        new Response(
          JSON.stringify({
            subject: "old",
            tenant_id: "stale-tenant",
            role: "admin",
            expires_at: new Date(Date.now() + 600000).toISOString(),
          }),
        ),
      ),
    );
    expect(screen.queryByText(/stale-tenant/)).toBeNull();
    expect(screen.getByText(/new-tenant/)).toBeTruthy();
    expect(fetchSpy).toHaveBeenCalledTimes(3);
  });

  it("discards retry credentials at token expiry", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-22T01:00:00Z"));
    stubFetch(
      vi.fn().mockResolvedValue(new Response("{}", { status: 503 })),
    );
    render(<App />);
    const payload = btoa(JSON.stringify({ exp: Date.now() / 1000 + 2 }));
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: `header.${payload}.signature` },
    });
    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: "Sign in" })),
    );
    expect(screen.getByRole("button", { name: "Retry sign in" })).toBeTruthy();
    await act(async () => vi.advanceTimersByTime(3000));
    expect(screen.getByRole("alert")).toHaveProperty(
      "textContent",
      "Your session expired. Sign in again to continue.",
    );
    expect(screen.queryByRole("button", { name: "Retry sign in" })).toBeNull();
    expect(
      (screen.getByLabelText("Development access token") as HTMLInputElement)
        .value,
    ).toBe("");
  });

  it("discards an in-flight response after sign out", async () => {
    let finish: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      finish = resolve;
    });
    stubFetch(
      vi
        .fn()
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              subject: "operator",
              tenant_id: "tenant",
              role: "analyst",
              expires_at: new Date(Date.now() + 600000).toISOString(),
            }),
          ),
        )
        .mockReturnValueOnce(pending),
    );
    render(<App />);
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: "token" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    fireEvent.click(await screen.findByRole("button", { name: "Sign out" }));
    finish(
      new Response(
        JSON.stringify({
          items: [{ summary: "Leaked stale alert" }],
          next_cursor: null,
          has_more: false,
        }),
      ),
    );
    await waitFor(() =>
      expect(screen.queryByText("Leaked stale alert")).toBeNull(),
    );
    expect(screen.getByRole("button", { name: "Sign in" })).toBeTruthy();
  });

  it("exposes a main landmark and a Sentinel heading", () => {
    render(<App />);
    expect(screen.getByRole("main")).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Sentinel", level: 1 }),
    ).toBeTruthy();
  });

  it("makes no network calls and renders no alert data", () => {
    const fetchSpy = vi.fn();
    stubFetch(fetchSpy);
    render(<App />);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(screen.queryAllByRole("row")).toHaveLength(0);
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
    vi.unstubAllGlobals();
  });

  it("caches the actual alert response under verified identity and clears it on sign out", async () => {
    const client = new QueryClient();
    stubFetch(
      vi
        .fn()
        .mockResolvedValueOnce(Response.json(identity()))
        .mockResolvedValueOnce(Response.json(workflowPage())),
    );
    render(<App queryClient={client} />);
    signIn();
    expect(await screen.findByText("Cached tenant alert")).toBeTruthy();
    const cached = client
      .getQueryCache()
      .getAll()
      .filter((query) => query.state.data !== undefined);
    expect(cached).toHaveLength(1);
    expect(cached[0].queryKey).toEqual([
      "session",
      "verified-operator",
      "cache-tenant",
      "viewer",
      expect.any(Number),
      "alerts",
      defaultAlertFilters(1),
    ]);
    expect(cached[0].state.data).toEqual(workflowPage());
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((query) => query.queryKey),
      ),
    ).not.toContain("memory-credential");
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    expect(screen.queryByText("Cached tenant alert")).toBeNull();
  });

  it("expires verified authority and its actual cached data without waiting for a 401", async () => {
    vi.useFakeTimers();
    const client = new QueryClient();
    stubFetch(
      vi
        .fn()
        .mockResolvedValueOnce(Response.json(identity("cache-tenant", 2000)))
        .mockResolvedValueOnce(Response.json(workflowPage())),
    );
    render(<App queryClient={client} />);
    await act(async () => signIn());
    await act(async () => vi.advanceTimersByTime(1));
    expect(screen.getByText("Cached tenant alert")).toBeTruthy();
    expect(
      client
        .getQueryCache()
        .getAll()
        .some((query) => query.state.data !== undefined),
    ).toBe(true);
    await act(async () => vi.advanceTimersByTime(2000));
    expect(screen.queryByText("Cached tenant alert")).toBeNull();
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    expect(screen.getByRole("alert").textContent).toBe(
      "Your session expired. Sign in again to continue.",
    );
    expect(screen.queryByText(/cache-tenant/)).toBeNull();
  });

  it("never repopulates the old tenant cache when an uncooperative request finishes after new login", async () => {
    const client = new QueryClient();
    let finish: (response: Response) => void = () => {};
    const delayed = new Promise<Response>((resolve) => {
      finish = resolve;
    });
    const fetchSpy = vi
      .fn()
      .mockResolvedValueOnce(Response.json(identity()))
      .mockReturnValueOnce(delayed)
      .mockResolvedValueOnce(Response.json(identity("new-tenant")))
      .mockResolvedValueOnce(
        Response.json({ items: [], next_cursor: null, has_more: false }),
      );
    stubFetch(fetchSpy);
    render(<App queryClient={client} />);
    signIn("old-credential");
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2));
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    signIn("new-credential");
    expect(
      await screen.findByText("No alerts match these filters"),
    ).toBeTruthy();
    await act(async () =>
      finish(Response.json(workflowPage("Old tenant secret"))),
    );
    expect(screen.queryByText("Old tenant secret")).toBeNull();
    const cached = client.getQueryCache().getAll();
    expect(cached).toHaveLength(1);
    expect(cached[0].queryKey).toEqual([
      "session",
      "verified-operator",
      "new-tenant",
      "viewer",
      expect.any(Number),
      "alerts",
      defaultAlertFilters(3),
    ]);
    expect(
      JSON.stringify(cached.map((query) => query.state.data)),
    ).not.toContain("Old tenant secret");
  });

  it("revokes the authenticated cache on an alert 401 without retrying credentials", async () => {
    const client = new QueryClient();
    const fetchSpy = vi
      .fn()
      .mockResolvedValueOnce(Response.json(identity()))
      .mockResolvedValueOnce(new Response("unauthorized", { status: 401 }));
    stubFetch(fetchSpy);
    render(<App queryClient={client} />);
    signIn();
    expect((await screen.findByRole("alert")).textContent).toBe(
      "Your session expired. Sign in again to continue.",
    );
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    expect(screen.queryByRole("button", { name: "Retry sign in" })).toBeNull();
    expect(fetchSpy).toHaveBeenCalledTimes(2);
    expect((fetchSpy.mock.calls[1][0] as Request).signal.aborted).toBe(true);
  });

  it("offers an explicit alert retry after a service failure and renders only the real result", async () => {
    const fetchSpy = vi
      .fn()
      .mockResolvedValueOnce(Response.json(identity()))
      .mockResolvedValueOnce(
        new Response("service internals must not display", { status: 503 }),
      )
      .mockResolvedValueOnce(Response.json(workflowPage("Recovered workflow")));
    stubFetch(fetchSpy);
    render(<App />);
    signIn();
    expect((await screen.findByRole("alert")).textContent).toBe(
      "Could not load alerts. Try again.",
    );
    expect(document.body.textContent).not.toContain("service internals");
    expect(fetchSpy).toHaveBeenCalledTimes(2);
    fireEvent.click(
      screen.getByRole("button", { name: "Retry loading alerts" }),
    );
    expect(await screen.findByText("Recovered workflow")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("uses verified expiry rather than unverified claims for the authenticated lifetime", async () => {
    vi.useFakeTimers();
    const payload = btoa(
      JSON.stringify({
        sub: "forged-subject",
        tenant: "forged-tenant",
        role: "admin",
        exp: Date.now() / 1000 + 1,
      }),
    );
    stubFetch(
      vi
        .fn()
        .mockResolvedValueOnce(Response.json(identity("cache-tenant", 3000)))
        .mockResolvedValueOnce(Response.json(workflowPage())),
    );
    render(<App />);
    await act(async () => signIn(`header.${payload}.signature`));
    await act(async () => vi.advanceTimersByTime(1));
    expect(screen.getByText("Cached tenant alert")).toBeTruthy();
    expect(document.body.textContent).not.toContain("forged-");
    await act(async () => vi.advanceTimersByTime(1500));
    expect(screen.getByRole("button", { name: "Sign out" })).toBeTruthy();
    await act(async () => vi.advanceTimersByTime(1500));
    expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
  });

  it("caps opaque retry credentials at fifteen minutes and does not extend them on retry", async () => {
    vi.useFakeTimers();
    stubFetch(
      vi
        .fn()
        .mockImplementation(() =>
          Promise.resolve(new Response("{}", { status: 503 })),
        ),
    );
    render(<App />);
    await act(async () => signIn("opaque-credential"));
    await act(async () => vi.advanceTimersByTime(14 * 60000));
    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: "Retry sign in" })),
    );
    await act(async () => vi.advanceTimersByTime(60000));
    expect(screen.queryByRole("button", { name: "Retry sign in" })).toBeNull();
    expect(screen.getByRole("alert").textContent).toBe(
      "Your session expired. Sign in again to continue.",
    );
  });

  it("drops old data and uses a fresh generation when the same verified identity signs in again", async () => {
    const client = new QueryClient();
    stubFetch(
      vi
        .fn()
        .mockResolvedValueOnce(Response.json(identity()))
        .mockResolvedValueOnce(Response.json(workflowPage("First session")))
        .mockResolvedValueOnce(Response.json(identity()))
        .mockResolvedValueOnce(Response.json(workflowPage("Second session"))),
    );
    const view = render(<App queryClient={client} />);
    signIn("first-token");
    expect(await screen.findByText("First session")).toBeTruthy();
    const original = client.getQueryCache().getAll()[0].queryKey;
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    signIn("second-token");
    expect(await screen.findByText("Second session")).toBeTruthy();
    expect(client.getQueryCache().getAll()).toHaveLength(1);
    expect(client.getQueryCache().getAll()[0].queryKey).not.toEqual(original);
    expect(screen.queryByText("First session")).toBeNull();
    view.unmount();
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    render(<App queryClient={client} />);
    expect(screen.getByRole("button", { name: "Sign in" })).toBeTruthy();
    expect(screen.queryByText("Second session")).toBeNull();
  });

  it("renders a labelled mobile list from the same real query result", async () => {
    vi.stubGlobal("matchMedia", () => ({
      matches: true,
      addEventListener: () => {},
      removeEventListener: () => {},
    }));
    stubFetch(
      vi
        .fn()
        .mockResolvedValueOnce(Response.json(identity()))
        .mockResolvedValueOnce(Response.json(workflowPage("Mobile workflow"))),
    );
    render(<App />);
    signIn();
    expect(
      await screen.findByRole("heading", { name: "Mobile workflow" }),
    ).toBeTruthy();
    expect(screen.getByRole("list", { name: "Tenant alerts" })).toBeTruthy();
    expect(screen.getByText("AUTH_BRUTE_FORCE")).toBeTruthy();
    expect(screen.getByText("open")).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it.each(["sign out", "verified expiry", "authenticated 401"])(
    "clears a draft entered during verification after bootstrap and %s",
    async (revocation) => {
      vi.useFakeTimers();
      const client = new QueryClient();
      let finish: (response: Response) => void = () => {};
      const verification = new Promise<Response>((resolve) => {
        finish = resolve;
      });
      const fetchSpy = vi
        .fn()
        .mockReturnValueOnce(verification)
        .mockResolvedValueOnce(Response.json(workflowPage()))
        .mockResolvedValueOnce(new Response("{}", { status: 401 }));
      stubFetch(fetchSpy);
      render(<App queryClient={client} />);
      signIn("submitted-credential");
      fireEvent.change(screen.getByLabelText("Development access token"), {
        target: { value: "disconnected-draft" },
      });
      expect(screen.getByRole("status").textContent).toBe(
        "Verifying access token…",
      );
      await act(async () =>
        finish(Response.json(identity("cache-tenant", 2000))),
      );
      await act(async () => vi.advanceTimersByTime(1));
      expect(screen.getByText("Cached tenant alert")).toBeTruthy();
      expect(client.getQueryCache().getAll()).toHaveLength(1);
      expect(screen.queryByLabelText("Development access token")).toBeNull();
      expect(document.body.innerHTML).not.toContain("disconnected-draft");
      if (revocation === "sign out")
        fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
      else if (revocation === "verified expiry")
        await act(async () => vi.advanceTimersByTime(2000));
      else await act(async () => client.refetchQueries());
      expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
      expect(screen.queryByText("Cached tenant alert")).toBeNull();
      expect(client.getQueryCache().getAll()).toHaveLength(0);
      expect((fetchSpy.mock.calls[1][0] as Request).signal.aborted).toBe(true);
      expect(
        screen.queryByRole("button", { name: "Retry sign in" }),
      ).toBeNull();
      expect(localStorage.length + sessionStorage.length).toBe(0);
      expect(window.location.href).not.toContain("credential");
      expect(screen.getByLabelText("Development access token")).toHaveProperty(
        "value",
        "",
      );
    },
  );

  it("clears a retained-retry draft when its automatic deadline revokes the attempt", async () => {
    vi.useFakeTimers();
    const client = new QueryClient();
    const fetchSpy = vi
      .fn()
      .mockResolvedValue(new Response("{}", { status: 503 }));
    stubFetch(fetchSpy);
    render(<App queryClient={client} />);
    await act(async () => signIn("opaque-submitted-credential"));
    expect(screen.getByRole("button", { name: "Retry sign in" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: "disconnected-retry-draft" },
    });
    await act(async () => vi.advanceTimersByTime(15 * 60000));
    expect(screen.getByRole("alert").textContent).toBe(
      "Your session expired. Sign in again to continue.",
    );
    expect(screen.queryByRole("button", { name: "Retry sign in" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect((fetchSpy.mock.calls[0][0] as Request).signal.aborted).toBe(true);
    expect(localStorage.length + sessionStorage.length).toBe(0);
    expect(screen.getByLabelText("Development access token")).toHaveProperty(
      "value",
      "",
    );
  });

  it.each(["Retry sign in", "Sign in", "Cancel sign in"])(
    "clears a retained-retry draft on %s without leaking or reusing the draft",
    async (action) => {
      const client = new QueryClient();
      const fetchSpy = vi
        .fn()
        .mockResolvedValueOnce(new Response("{}", { status: 503 }))
        .mockImplementation(() => new Promise<Response>(() => {}));
      stubFetch(fetchSpy);
      render(<App queryClient={client} />);
      signIn("original-submitted-credential");
      expect(await screen.findByRole("alert")).toBeTruthy();
      fireEvent.change(screen.getByLabelText("Development access token"), {
        target: { value: "replacement-draft" },
      });
      fireEvent.click(screen.getByRole("button", { name: action }));
      expect(client.getQueryCache().getAll()).toHaveLength(0);
      expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
      expect((fetchSpy.mock.calls[0][0] as Request).signal.aborted).toBe(true);
      if (action === "Cancel sign in") {
        expect(fetchSpy).toHaveBeenCalledTimes(1);
        expect(screen.queryByRole("status")).toBeNull();
        expect(
          screen.queryByRole("button", { name: "Retry sign in" }),
        ).toBeNull();
      } else {
        expect(fetchSpy).toHaveBeenCalledTimes(2);
        expect(
          (fetchSpy.mock.calls[1][0] as Request).headers.get("Authorization"),
        ).toBe(
          action === "Retry sign in"
            ? "Bearer original-submitted-credential"
            : "Bearer replacement-draft",
        );
      }
      expect(localStorage.length + sessionStorage.length).toBe(0);
      expect(window.location.href).not.toContain("draft");
      expect(screen.getByLabelText("Development access token")).toHaveProperty(
        "value",
        "",
      );
    },
  );

  it("clears a pending draft when verification itself returns 401", async () => {
    const client = new QueryClient();
    let finish: (response: Response) => void = () => {};
    const fetchSpy = vi.fn().mockReturnValue(
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
    );
    stubFetch(fetchSpy);
    render(<App queryClient={client} />);
    signIn("rejected-credential");
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: "pending-draft" },
    });
    await act(async () => finish(new Response("{}", { status: 401 })));
    expect(screen.getByRole("alert").textContent).toBe(
      "This access token could not be verified. Check it or generate a new token.",
    );
    expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Retry sign in" })).toBeNull();
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect((fetchSpy.mock.calls[0][0] as Request).signal.aborted).toBe(true);
    expect(screen.getByLabelText("Development access token")).toHaveProperty(
      "value",
      "",
    );
  });
});
