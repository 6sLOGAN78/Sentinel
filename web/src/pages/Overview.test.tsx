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
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { createAPIClient } from "../api/client";

const summary = {
  event_rate_per_sec: 0.05,
  open_alerts: 3,
  alerts_by_severity: [
    { severity: "critical", count: 1 },
    { severity: "high", count: 2 },
    { severity: "medium", count: 0 },
    { severity: "low", count: 0 },
  ],
  top_rules: [{ rule_id: "AUTH_BRUTE_FORCE", count: 5 }],
  trend_24h: Array.from({ length: 288 }, (_, i) => ({
    bucket_start: new Date(Date.UTC(2026, 9, 24, 0, i * 5)).toISOString(),
    event_count: i === 0 ? 7 : 0,
    alert_count: i === 0 ? 5 : 0,
  })),
};
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});
async function start(
  read: (request: Request) => Promise<Response>,
  role = "viewer",
) {
  window.history.replaceState(null, "", "/");
  const calls: Request[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (request: Request) => {
      if (new URL(request.url).pathname === "/api/v1/session")
        return Response.json({
          subject: "operator",
          tenant_id: "overview-tenant",
          role,
          expires_at: new Date(Date.now() + 600000).toISOString(),
        });
      calls.push(request);
      return read(request);
    }),
  );
  const cache = new QueryClient();
  render(<App queryClient={cache} />);
  fireEvent.change(screen.getByLabelText("Development access token"), {
    target: { value: "overview-private-credential" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await screen.findByRole("heading", { name: "Overview" });
  return { calls, cache };
}
describe("Overview through actual App root, session and guarded transport", () => {
  it.each(["caller", "session"])(
    "rejects late summary bodies after %s cancellation",
    async (which) => {
      let release!: (value: string) => void;
      const response = Response.json(summary);
      response.text = () =>
        new Promise((resolve) => {
          release = resolve;
        });
      const fetch = vi.fn(async () => response);
      vi.stubGlobal("fetch", fetch);
      const lifetime = new AbortController(),
        caller = new AbortController();
      let current = true;
      const credential = vi.fn(() => "private-summary-credential");
      const api = createAPIClient(
        credential,
        lifetime.signal,
        () => current,
        vi.fn(),
      );
      const pending = api.summary(caller.signal);
      const rejected = expect(pending).rejects.toMatchObject({
        name: "AbortError",
      });
      await waitFor(() => expect(release).toBeTypeOf("function"));
      if (which === "caller") caller.abort();
      else {
        current = false;
        lifetime.abort();
      }
      release(JSON.stringify(summary));
      await rejected;
      current = false;
      const calls = credential.mock.calls.length;
      await expect(api.summary()).rejects.toMatchObject({ name: "AbortError" });
      expect(credential).toHaveBeenCalledTimes(calls);
      expect(fetch).toHaveBeenCalledTimes(1);
    },
  );
  it("renders actual metric values, SVG and equivalent complete table", async () => {
    const { calls, cache } = await start(async () => Response.json(summary));
    fireEvent.click(await screen.findByText("View all 288 activity buckets"));
    const table = await screen.findByRole("table", {
      name: "24 hour activity",
    });
    expect(within(table).getAllByRole("row")).toHaveLength(289);
    expect(within(table).getAllByRole("row")[1].textContent).toContain("75");
    expect(
      screen.getByRole("img", { name: "24 hour tenant activity" }),
    ).toBeTruthy();
    expect(screen.getByLabelText("Open alerts").textContent).toBe("3");
    expect(screen.getByLabelText("Events per second").textContent).toBe("0.05");
    expect(
      screen.getByRole("table", { name: "Top rules" }).textContent,
    ).toContain("AUTH_BRUTE_FORCE5");
    expect(
      within(screen.getByRole("region", { name: "Consumer lag" })).getByText(
        "Consumer lag requires the analyst role.",
      ),
    ).toBeTruthy();
    expect(document.body.textContent).not.toContain("available in Phase 5");
    expect(
      screen
        .getByRole("link", { name: "Overview" })
        .getAttribute("aria-current"),
    ).toBe("page");
    expect(calls.map((c) => new URL(c.url).pathname)).toEqual([
      "/api/v1/metrics/summary",
    ]);
    expect(calls[0].cache).toBe("no-store");
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.queryKey),
      ),
    ).not.toContain("credential");
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(calls[0].signal.aborted).toBe(true);
    expect(localStorage.length + sessionStorage.length).toBe(0);
  });
  it("announces loading and never fabricates zeros on failure; retry uses real result", async () => {
    let finish!: (response: Response) => void;
    let n = 0;
    const { calls } = await start(() =>
      ++n === 1
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : Promise.resolve(Response.json(summary)),
    );
    expect(screen.getByRole("status").textContent).toBe("Loading overview…");
    expect(screen.queryByLabelText("Open alerts")).toBeNull();
    await act(async () =>
      finish(new Response("private internals", { status: 503 })),
    );
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Could not load overview. Try again.",
    );
    expect(screen.queryByLabelText("Open alerts")).toBeNull();
    expect(document.body.textContent).not.toContain("private internals");
    fireEvent.click(
      screen.getByRole("button", { name: "Retry loading overview" }),
    );
    fireEvent.click(await screen.findByText("View all 288 activity buckets"));
    await screen.findByRole("table", { name: "24 hour activity" });
    expect(calls).toHaveLength(2);
  });
  it("shows genuine empty activity and navigates with history without losing root", async () => {
    await start(async (request) =>
      new URL(request.url).pathname === "/api/v1/alerts"
        ? Response.json({ items: [], next_cursor: null, has_more: false })
        : Response.json({
            ...summary,
            open_alerts: 0,
            event_rate_per_sec: 0,
            top_rules: [],
            trend_24h: summary.trend_24h.map((p) => ({
              ...p,
              event_count: 0,
              alert_count: 0,
            })),
          }),
    );
    expect(await screen.findByText("No activity in this period")).toBeTruthy();
    fireEvent.click(screen.getByRole("link", { name: "Alerts" }));
    expect(window.location.pathname).toBe("/alerts");
    expect(
      await screen.findByText("No alerts match these filters"),
    ).toBeTruthy();
    act(() => window.history.back());
    await waitFor(() =>
      expect(
        screen
          .getByRole("link", { name: "Overview" })
          .getAttribute("aria-current"),
      ).toBe("page"),
    );
    act(() => window.history.forward());
    await waitFor(() =>
      expect(
        screen
          .getByRole("link", { name: "Alerts" })
          .getAttribute("aria-current"),
      ).toBe("page"),
    );
  });
  it("revokes summary data on 401 without retries", async () => {
    window.history.replaceState(null, "", "/");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (request: Request) =>
        new URL(request.url).pathname === "/api/v1/session"
          ? Response.json({
              subject: "operator",
              tenant_id: "tenant",
              role: "viewer",
              expires_at: new Date(Date.now() + 600000).toISOString(),
            })
          : new Response("{}", { status: 401 }),
      ),
    );
    const cache = new QueryClient();
    render(<App queryClient={cache} />);
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: "opaque" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect((await screen.findByRole("alert")).textContent).toBe(
      "Your session expired. Sign in again to continue.",
    );
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(fetch).toHaveBeenCalledTimes(2);
  });
});

const available = (value: number) => ({ state: "available", value });
const lagBody = (
  lag: unknown[] = [available(12345), available(0), available(7)],
) => ({
  observed_at: "2026-11-18T12:34:56Z",
  consumer_lag: ["event-writer", "detector", "alert-writer"].map(
    (group, i) => ({ group, lag: lag[i] }),
  ),
});
const pathOf = (request: Request) => new URL(request.url).pathname;
const lagRegion = () => screen.getByRole("region", { name: "Consumer lag" });
function routes(lag: () => Promise<Response>, summaryOk = true) {
  return async (request: Request) =>
    pathOf(request) === "/api/v1/metrics/lag"
      ? lag()
      : pathOf(request) === "/api/v1/metrics/summary"
        ? summaryOk
          ? Response.json(summary)
          : new Response("{}", { status: 503 })
        : new Response("{}", { status: 503 });
}

describe("Overview consumer lag panel", () => {
  it("shows viewers the analyst-role notice without requesting lag", async () => {
    const { calls } = await start(routes(async () => Response.json(lagBody())));
    await screen.findByLabelText("Open alerts");
    const region = lagRegion();
    const note = within(region).getByText(
      "Consumer lag requires the analyst role.",
    );
    expect(note.className).toContain("access-note");
    expect(region.textContent).not.toMatch(/\d/);
    expect(calls.map(pathOf)).not.toContain("/api/v1/metrics/lag");
    expect(calls.map(pathOf)).not.toContain("/api/v1/system");
    expect(within(region).queryByRole("link")).toBeNull();
    expect(document.body.textContent).not.toContain("requires the admin role");
    expect(document.body.textContent).not.toContain("available in Phase 5");
  });

  it.each(["analyst", "admin"])(
    "shows real per-group lag to %s, with the System link only for admins",
    async (role) => {
      const { calls } = await start(
        routes(async () => Response.json(lagBody())),
        role,
      );
      const region = lagRegion();
      await within(region).findByText("12,345 records");
      expect(within(region).getByRole("heading", { level: 3 }).textContent).toBe(
        "Consumer lag",
      );
      const terms = [...region.querySelectorAll("dl dt")].map(
        (dt) => dt.textContent,
      );
      const values = [...region.querySelectorAll("dl dd")].map(
        (dd) => dd.textContent,
      );
      expect(terms).toEqual(["event-writer", "detector", "alert-writer"]);
      expect(values).toEqual(["12,345 records", "0 records", "7 records"]);
      const lagCall = calls.find((c) => pathOf(c) === "/api/v1/metrics/lag")!;
      expect(lagCall.method).toBe("GET");
      expect(new URL(lagCall.url).search).toBe("");
      expect(document.body.textContent).not.toContain("requires the admin role");
      expect(document.body.textContent).not.toContain("available in Phase 5");
      expect(document.body.textContent).not.toContain(
        "Consumer lag requires the analyst role.",
      );
      expect(calls.map(pathOf)).not.toContain("/api/v1/system");
      const link = within(region).queryByRole("link", {
        name: "View system health",
      });
      if (role !== "admin") {
        expect(link).toBeNull();
        return;
      }
      expect(link!.getAttribute("href")).toBe("/system");
      fireEvent.click(link!);
      expect(window.location.pathname).toBe("/system");
      expect(
        await screen.findByRole("heading", { name: "System", level: 2 }),
      ).toBeTruthy();
    },
  );

  it.each(["analyst", "admin"])(
    "labels operator-only rows for a non-operator %s without errors or zeros",
    async (role) => {
      const operatorOnly = { state: "unavailable", reason: "operator_only" };
      await start(
        routes(async () =>
          Response.json(lagBody([operatorOnly, operatorOnly, operatorOnly])),
        ),
        role,
      );
      const region = lagRegion();
      const badges = await within(region).findAllByText("Operator only");
      expect(badges).toHaveLength(3);
      const tip =
        "Consumer lag is platform-wide and visible only to operator tenants.";
      for (const badge of badges) {
        expect(badge.getAttribute("title")).toBe(tip);
        const note = document.getElementById(
          badge.getAttribute("aria-describedby")!,
        );
        expect(note?.textContent).toBe(tip);
      }
      expect(within(region).queryByRole("alert")).toBeNull();
      expect(region.textContent).not.toContain("Consumer lag unavailable");
      expect(region.textContent).not.toContain("No data");
      expect(region.textContent).not.toMatch(/\d/);
      expect(
        within(region).queryByRole("button", {
          name: "Retry loading consumer lag",
        }),
      ).toBeNull();
    },
  );

  it.each(["prometheus_unavailable", "no_data"])(
    "shows the unavailable copy and retry for %s rows, never 0",
    async (reason) => {
      let n = 0;
      const { calls } = await start(
        routes(async () =>
          ++n === 1
            ? Response.json(
                lagBody([
                  { state: "unavailable", reason },
                  { state: "unavailable", reason },
                  { state: "unavailable", reason },
                ]),
              )
            : Response.json(lagBody()),
        ),
        "analyst",
      );
      const region = lagRegion();
      expect(
        await within(region).findByText(
          "Consumer lag unavailable. The metrics source is not responding.",
        ),
      ).toBeTruthy();
      expect(region.textContent).not.toMatch(/\d/);
      expect(within(region).queryByRole("alert")).toBeNull();
      const retry = within(region).getByRole("button", {
        name: "Retry loading consumer lag",
      });
      expect(retry.className).toContain("secondary");
      fireEvent.click(retry);
      await within(region).findByText("12,345 records");
      expect(
        calls.filter((c) => pathOf(c) === "/api/v1/metrics/lag"),
      ).toHaveLength(2);
    },
  );

  it("reports a lag request error in an alert without hiding the summary", async () => {
    await start(
      routes(async () => new Response("private internals", { status: 503 })),
      "analyst",
    );
    const region = lagRegion();
    const alert = await within(region).findByRole("alert");
    expect(alert.textContent).toContain(
      "Consumer lag unavailable. The metrics source is not responding.",
    );
    expect(
      within(alert).getByRole("button", { name: "Retry loading consumer lag" }),
    ).toBeTruthy();
    expect((await screen.findByLabelText("Open alerts")).textContent).toBe("3");
    expect(document.body.textContent).not.toContain("private internals");
  });

  it("keeps lag visible when the summary fails", async () => {
    await start(
      routes(async () => Response.json(lagBody()), false),
      "analyst",
    );
    expect(
      await screen.findByText("Could not load overview. Try again."),
    ).toBeTruthy();
    expect(await within(lagRegion()).findByText("12,345 records")).toBeTruthy();
  });

  it("falls back to the analyst-role notice on a server 403", async () => {
    await start(
      routes(async () =>
        Response.json(
          {
            error: {
              code: "FORBIDDEN",
              message: "Consumer lag requires the analyst role.",
              request_id: "r",
            },
          },
          { status: 403 },
        ),
      ),
      "analyst",
    );
    const region = lagRegion();
    expect(
      await within(region).findByText(
        "Consumer lag requires the analyst role.",
      ),
    ).toBeTruthy();
    expect(within(region).queryByRole("alert")).toBeNull();
  });

  it("rejects a lag body without exactly the three pipeline groups", async () => {
    const body = lagBody();
    body.consumer_lag = body.consumer_lag.slice(0, 2);
    await start(routes(async () => Response.json(body)), "analyst");
    expect(await within(lagRegion()).findByRole("alert")).toBeTruthy();
    expect(lagRegion().textContent).not.toMatch(/\d/);
  });
});
