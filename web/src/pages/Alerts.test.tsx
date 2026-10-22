import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import type { AlertPage } from "../api/client";

function page(
  summary = "First filtered page",
  next: string | null = "page-2",
): AlertPage {
  return {
    items: [
      {
        alert_id: "a".repeat(64),
        tenant_id: "filters-tenant",
        created_at: "2026-10-21T12:00:00.000Z",
        updated_at: "2026-10-21T12:00:00.000Z",
        rule_id: "AUTH_BRUTE_FORCE",
        rule_version: 1,
        severity: "high",
        entity_key: "192.0.2.44",
        summary,
        event_count: 10,
        window_start: "2026-10-21T11:59:00.000Z",
        window_end: "2026-10-21T12:00:00.000Z",
        status: "open",
        assignee: null,
        version: 1,
      },
    ],
    next_cursor: next,
    has_more: next !== null,
  };
}

function signIn() {
  fireEvent.change(screen.getByLabelText("Development access token"), {
    target: { value: "filter-test-credential" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}

async function start(
  list: (request: Request) => Promise<Response> = async () =>
    Response.json(page()),
  lifetime = 600000,
) {
  const requests: Request[] = [];
  let tenant = "filters-tenant";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (request: Request) => {
      if (new URL(request.url).pathname === "/api/v1/session")
        return Response.json({
          subject: "filters-operator",
          tenant_id: tenant,
          role: "viewer",
          expires_at: new Date(Date.now() + lifetime).toISOString(),
        });
      requests.push(request);
      return list(request);
    }),
  );
  const cache = new QueryClient();
  render(<App queryClient={cache} />);
  signIn();
  await waitFor(() => expect(requests.length).toBeGreaterThan(0));
  return {
    requests,
    cache,
    switchTenant: () => {
      tenant = "second-tenant";
    },
  };
}

describe("filtered alert pages", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    window.history.replaceState(null, "", "/alerts");
  });

  it("applies labelled filters to actual requests and normalized identity cache keys", async () => {
    const { requests, cache } = await start();
    await screen.findByText("First filtered page");
    fireEvent.change(screen.getByLabelText("Status"), {
      target: { value: "resolved" },
    });
    fireEvent.change(screen.getByLabelText("Severity"), {
      target: { value: "critical" },
    });
    fireEvent.change(screen.getByLabelText("Rule ID"), {
      target: { value: " DENY_SPIKE " },
    });
    expect(requests).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() => expect(requests).toHaveLength(2));
    const query = new URL(requests[1].url).searchParams;
    expect(query.get("status")).toBe("resolved");
    expect(query.get("severity")).toBe("critical");
    expect(query.get("rule_id")).toBe("DENY_SPIKE");
    expect(Date.parse(query.get("to")!) - Date.parse(query.get("from")!)).toBe(
      86400000,
    );
    expect(query.get("cursor")).toBeNull();
    const keys = cache
      .getQueryCache()
      .getAll()
      .map((entry) => entry.queryKey);
    expect(
      keys.some(
        (key) =>
          key[1] === "filters-operator" &&
          key[2] === "filters-tenant" &&
          typeof key[4] === "number" &&
          JSON.stringify(key).includes('"status":"resolved"'),
      ),
    ).toBe(true);
    expect(window.location.search).toContain("status=resolved");
  });

  it("debounces search for 300ms, cancels superseded requests and resets the cursor", async () => {
    let pending = false;
    const { requests } = await start(async () =>
      pending ? new Promise(() => {}) : Response.json(page()),
    );
    await screen.findByText("First filtered page");
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(requests).toHaveLength(2));
    pending = true;
    vi.useFakeTimers();
    fireEvent.change(screen.getByLabelText("Search alerts"), {
      target: { value: "first" },
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(299);
    });
    expect(requests).toHaveLength(2);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(requests).toHaveLength(3);
    expect(new URL(requests[2].url).searchParams.get("search")).toBe("first");
    expect(new URL(requests[2].url).searchParams.has("cursor")).toBe(false);
    fireEvent.change(screen.getByLabelText("Search alerts"), {
      target: { value: "second" },
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(requests[2].signal.aborted).toBe(true);
    expect(new URL(requests[3].url).searchParams.get("search")).toBe("second");
    expect(
      screen.getByRole("button", { name: "Previous page" }),
    ).toHaveProperty("disabled", true);
  });

  it("keeps a frozen range through next/previous and browser back/forward", async () => {
    const { requests } = await start(async (request) =>
      Response.json(
        new URL(request.url).searchParams.has("cursor")
          ? page("Second filtered page", null)
          : page(),
      ),
    );
    await screen.findByText("First filtered page");
    const firstURL = requests[0].url;
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    await screen.findByText("Second filtered page");
    expect(new URL(requests[1].url).searchParams.get("from")).toBe(
      new URL(firstURL).searchParams.get("from"),
    );
    await act(async () => {
      window.history.back();
    });
    await waitFor(() => expect(window.location.search).not.toContain("cursor"));
    await screen.findByText("First filtered page");
    await act(async () => {
      window.history.forward();
    });
    await screen.findByText("Second filtered page");
    // Traversal resets the bounded explicit trail. Rebuild it with Next, then
    // test Previous independently of native Back/Forward history entries.
    await act(async () => {
      window.history.back();
    });
    await screen.findByText("First filtered page");
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Next page" })).toHaveProperty(
        "disabled",
        false,
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    await screen.findByText("Second filtered page");
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Previous page" }),
      ).toHaveProperty("disabled", false),
    );
    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
    await screen.findByText("First filtered page");
  });

  it("reads filter/cursor deep links and clears pagination when filters reset", async () => {
    window.history.replaceState(
      null,
      "",
      "/alerts?status=open&severity=high&rule_id=AUTH_BRUTE_FORCE&from=2026-10-21T00%3A00%3A00.000Z&to=2026-10-22T00%3A00%3A00.000Z&cursor=deep-link&limit=3",
    );
    const { requests } = await start();
    await screen.findByText("First filtered page");
    expect(new URL(requests[0].url).searchParams.get("cursor")).toBe(
      "deep-link",
    );
    expect(screen.getByLabelText("Status")).toHaveProperty("value", "open");
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(new URL(requests[1].url).searchParams.has("cursor")).toBe(false);
    expect(screen.getByLabelText("Status")).toHaveProperty("value", "");
  });

  it("bounds explicit previous history to 20 pages", async () => {
    await start(async (request) => {
      const n = Number(new URL(request.url).searchParams.get("cursor") ?? "0");
      return Response.json(page("Page " + n, String(n + 1)));
    });
    await screen.findByText("Page 0");
    for (let n = 1; n <= 22; n++) {
      await waitFor(() =>
        expect(
          screen.getByRole("button", { name: "Next page" }),
        ).toHaveProperty("disabled", false),
      );
      fireEvent.click(screen.getByRole("button", { name: "Next page" }));
      await screen.findByText("Page " + n);
    }
    for (let n = 21; n >= 2; n--) {
      await waitFor(() =>
        expect(
          screen.getByRole("button", { name: "Previous page" }),
        ).toHaveProperty("disabled", false),
      );
      fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
      await screen.findByText("Page " + n);
    }
    expect(
      screen.getByRole("button", { name: "Previous page" }),
    ).toHaveProperty("disabled", true);
  });

  it.each(["signout", "expiry", "unauthorized"])(
    "clears URL and in-memory cursors on %s and cannot restore old-session history",
    async (reason) => {
      // Install the fake clock before SessionProvider schedules its expiry.
      if (reason === "expiry") vi.useFakeTimers({ shouldAdvanceTime: true });
      let unauthorized = false;
      const { requests, cache, switchTenant } = await start(
        async () =>
          unauthorized
            ? new Response("{}", { status: 401 })
            : Response.json(page()),
        30000,
      );
      await screen.findByText("First filtered page");
      fireEvent.click(screen.getByRole("button", { name: "Next page" }));
      await waitFor(() => expect(requests).toHaveLength(2));
      const oldLocation = window.location.href;
      const oldState: unknown = window.history.state;
      if (reason === "expiry") {
        await act(async () => {
          await vi.advanceTimersByTimeAsync(30001);
        });
        vi.useRealTimers();
      } else if (reason === "unauthorized") {
        unauthorized = true;
        await act(async () => {
          await cache.invalidateQueries();
        });
        unauthorized = false;
      } else fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
      await screen.findByRole("heading", { name: "Sign in to Sentinel" });
      expect(window.location.search).not.toContain("cursor");
      expect(cache.getQueryCache().getAll()).toHaveLength(0);
      switchTenant();
      const previous = requests.length;
      signIn();
      await waitFor(() => expect(requests.length).toBeGreaterThan(previous));
      expect(new URL(requests.at(-1)!.url).searchParams.has("cursor")).toBe(
        false,
      );
      await act(async () => {
        window.history.replaceState(oldState, "", oldLocation);
        window.dispatchEvent(new PopStateEvent("popstate"));
      });
      expect(window.location.search).not.toContain("cursor");
      expect(
        screen.getByRole("button", { name: "Previous page" }),
      ).toHaveProperty("disabled", true);
    },
  );

  it("preserves server-frozen ranges for cursor-only deep links and subsequent pages", async () => {
    window.history.replaceState(
      null,
      "",
      "/alerts?cursor=frozen-server-cursor&limit=3",
    );
    const { requests, cache } = await start();
    await screen.findByText("First filtered page");
    expect([...new URL(requests[0].url).searchParams.keys()].sort()).toEqual([
      "cursor",
      "limit",
    ]);
    expect(cache.getQueryCache().getAll()[0].queryKey[6]).toEqual({
      cursor: "frozen-server-cursor",
      limit: 3,
    });
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(requests).toHaveLength(2));
    expect([...new URL(requests[1].url).searchParams.keys()].sort()).toEqual([
      "cursor",
      "limit",
    ]);
    expect(new URL(requests[1].url).searchParams.get("cursor")).toBe("page-2");
    fireEvent.change(screen.getByLabelText("Status"), {
      target: { value: "resolved" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() => expect(requests).toHaveLength(3));
    const changed = new URL(requests[2].url).searchParams;
    expect(changed.has("cursor")).toBe(false);
    expect(
      Date.parse(changed.get("to")!) - Date.parse(changed.get("from")!),
    ).toBe(86400000);
  });

  it("announces loading/empty/errors and retries the same current filters", async () => {
    let resolve: (response: Response) => void = () => {};
    let fail = false;
    const { requests } = await start(async () =>
      fail
        ? new Response("raw SQL must not render", { status: 503 })
        : new Promise((done) => {
            resolve = done;
          }),
    );
    expect(screen.getByRole("status").textContent).toBe("Loading alerts…");
    await act(async () => {
      resolve(Response.json({ items: [], has_more: false, next_cursor: null }));
    });
    await screen.findByText("No alerts match these filters");
    fail = true;
    fireEvent.change(screen.getByLabelText("Status"), {
      target: { value: "resolved" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await screen.findByText("Could not load alerts. Try again.");
    expect(document.body.textContent).not.toContain("raw SQL");
    fail = false;
    fireEvent.click(
      screen.getByRole("button", { name: "Retry loading alerts" }),
    );
    await waitFor(() => expect(requests).toHaveLength(3));
    expect(new URL(requests[2].url).search).toBe(
      new URL(requests[1].url).search,
    );
    await act(async () => {
      resolve(Response.json(page("Recovered page", null)));
    });
    await screen.findByText("Recovered page");
  });

  it("rejects an invalid UTC range before fetching", async () => {
    const { requests } = await start();
    await screen.findByText("First filtered page");
    fireEvent.change(screen.getByLabelText("From (UTC)"), {
      target: { value: "2026-10-22T00:00" },
    });
    fireEvent.change(screen.getByLabelText("To (UTC)"), {
      target: { value: "2026-10-21T00:00" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await screen.findByText("Choose a valid UTC time range of at most 7 days.");
    expect(requests).toHaveLength(1);
  });

  it("keeps malformed time deep links recoverable without sending a list request", async () => {
    window.history.replaceState(null, "", "/alerts?to=invalid");
    const fetchSpy = vi.fn(async () =>
      Response.json({
        subject: "filters-operator",
        tenant_id: "filters-tenant",
        role: "viewer",
        expires_at: new Date(Date.now() + 600000).toISOString(),
      }),
    );
    vi.stubGlobal("fetch", fetchSpy);
    render(<App />);
    signIn();
    await screen.findByText("Choose a valid UTC time range of at most 7 days.");
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    expect(screen.getByRole("heading", { name: "Alerts" })).toBeTruthy();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});
