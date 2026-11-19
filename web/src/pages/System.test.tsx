import { StrictMode } from "react";
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

type Metric =
  | { state: "available"; value: number }
  | { state: "unavailable"; reason: string };
const avail = (value: number): Metric => ({ state: "available", value });
const unavail = (reason = "prometheus_unavailable"): Metric => ({
  state: "unavailable",
  reason,
});
const names = [
  "ingest",
  "event-writer",
  "detector",
  "alert-writer",
  "query-api",
];
type Readiness =
  | {
      state: "ready" | "degraded" | "not_ready";
      ready_replicas: number;
      probed_replicas: number;
    }
  | { state: "unavailable"; reason: string };
const ready = (n = 1): Readiness => ({
  state: "ready",
  ready_replicas: n,
  probed_replicas: n,
});

function status(over: Record<string, unknown> = {}) {
  return {
    observed_at: "2026-11-18T12:34:56Z",
    metrics_source: { state: "available" },
    services: names.map((name) => ({
      name,
      expected_replicas: name === "ingest" ? 2 : 1,
      replicas_up: avail(name === "ingest" ? 2 : 1),
      restarts_15m: avail(name === "detector" ? 1 : 0),
      readiness: ready(name === "ingest" ? 2 : 1),
    })),
    ingest: {
      total: avail(1234.54),
      by_result: {
        accepted: avail(1200),
        rejected: avail(20.2),
        rate_limited: avail(10),
        shed: avail(4.29),
        unavailable: avail(0.1),
      },
    },
    http_errors: {
      ratio: avail(0.0042),
      status_5xx: avail(0.5),
      status_429: avail(1.25),
      status_503: avail(0.2),
    },
    dlq_rate: avail(0.05),
    consumer_lag: [
      { group: "event-writer", lag: avail(12345) },
      { group: "detector", lag: avail(0) },
      { group: "alert-writer", lag: avail(7) },
    ],
    latency: {
      detection_delay_p95_seconds: avail(0.245),
      clickhouse_write_p95_seconds: avail(1.25),
    },
    ...over,
  };
}

// Every Prometheus-derived field replaced by one unavailable reason.
function allMetrics(reason: string, source: unknown = { state: "available" }) {
  const base = status();
  return {
    ...base,
    metrics_source: source,
    services: base.services.map((s) => ({
      ...s,
      replicas_up: unavail(reason),
      restarts_15m: unavail(reason),
    })),
    ingest: {
      total: unavail(reason),
      by_result: {
        accepted: unavail(reason),
        rejected: unavail(reason),
        rate_limited: unavail(reason),
        shed: unavail(reason),
        unavailable: unavail(reason),
      },
    },
    http_errors: {
      ratio: unavail(reason),
      status_5xx: unavail(reason),
      status_429: unavail(reason),
      status_503: unavail(reason),
    },
    dlq_rate: unavail(reason),
    consumer_lag: base.consumer_lag.map((r) => ({
      ...r,
      lag: unavail(reason),
    })),
    latency: {
      detection_delay_p95_seconds: unavail(reason),
      clickhouse_write_p95_seconds: unavail(reason),
    },
  };
}

const forbidden = (message: string) =>
  Response.json(
    { error: { code: "FORBIDDEN", message, request_id: "req-1" } },
    { status: 403 },
  );

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});

async function start(
  read: (request: Request) => Promise<Response>,
  role = "admin",
  path = "/system",
  strict = false,
) {
  window.history.replaceState(null, "", path);
  const requests: Request[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (request: Request) => {
      if (new URL(request.url).pathname === "/api/v1/session")
        return Response.json({
          subject: "system-operator",
          tenant_id: "demo",
          role,
          expires_at: new Date(Date.now() + 600000).toISOString(),
        });
      // Like real fetch, an already-aborted request (StrictMode remount
      // cancels the first query fetch) rejects without reaching the server.
      if (request.signal.aborted)
        throw new DOMException("The operation was aborted.", "AbortError");
      requests.push(request);
      return read(request);
    }),
  );
  const cache = new QueryClient();
  // StrictMode replays effects, which cancels and repeats the first fetch;
  // tests that count requests or sequence responses render without it.
  render(
    strict ? (
      <StrictMode>
        <App queryClient={cache} />
      </StrictMode>
    ) : (
      <App queryClient={cache} />
    ),
  );
  fireEvent.change(screen.getByLabelText("Development access token"), {
    target: { value: "system-test-credential" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  return { cache, requests };
}

const systemCalls = (requests: Request[]) =>
  requests.filter((r) => new URL(r.url).pathname === "/api/v1/system");

// Fake-timer friendly settle: React Query notifies through setTimeout(0).
async function settle(rounds = 20) {
  for (let i = 0; i < rounds; i++)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
}

function expectNoFabricatedZeros() {
  for (const element of document.querySelectorAll("main *")) {
    const text = (element.textContent ?? "").trim();
    expect(text).not.toBe("0");
    expect(text.startsWith("0 ")).toBe(false);
    expect(text.startsWith("0.0")).toBe(false);
  }
}

const liveRegion = () => document.getElementById("system-live")!;

describe("System page through the real App, session and guarded client", () => {
  it.each(["viewer", "analyst"])(
    "hides System from %s, never requests it and shows the forbidden state on deep link",
    async (role) => {
      const { requests } = await start(
        async () => Response.json(status()),
        role,
      );
      expect(
        await screen.findByRole("heading", { name: "Admin access required" }),
      ).toBeTruthy();
      expect(screen.getByRole("heading", { name: "System" })).toBeTruthy();
      expect(
        screen.getByText(
          "System health shows pipeline activity across all tenants. Ask an admin to review it.",
        ),
      ).toBeTruthy();
      expect(screen.queryByRole("link", { name: "System" })).toBeNull();
      expect(
        screen.queryByRole("button", { name: "Refresh system status" }),
      ).toBeNull();
      expect(screen.queryByText("Loading system status…")).toBeNull();
      expect(systemCalls(requests)).toHaveLength(0);
    },
  );

  it("renders the same forbidden state for a server role 403 to an admin", async () => {
    const { requests } = await start(async () =>
      forbidden("System health requires the admin role."),
    );
    expect(
      await screen.findByRole("heading", { name: "Admin access required" }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Refresh system status" }),
    ).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(systemCalls(requests)).toHaveLength(1);
  });

  it("explains the operator-tenant 403 separately from the role 403", async () => {
    const { requests } = await start(async () =>
      forbidden("System status is limited to operator administrators."),
    );
    expect(
      await screen.findByRole("heading", { name: "Operator access required" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "System status is limited to operator administrators. Your tenant is not an operator tenant.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("Admin access required")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Refresh system status" }),
    ).toBeNull();
    expect(
      screen.queryByRole("link", { name: /Open Grafana dashboards/ }),
    ).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(systemCalls(requests)).toHaveLength(1);
  });

  it("renders every section with exact copy and number formats for an admin", async () => {
    const { requests } = await start(
      async () => Response.json(status()),
      "admin",
      "/system",
      true,
    );
    const services = await screen.findByRole("table", { name: "Services" });
    const rows = within(services).getAllByRole("row");
    expect(rows).toHaveLength(6);
    expect(
      rows.slice(1).map((r) => within(r).getByRole("rowheader").textContent),
    ).toEqual(names);
    expect(rows[1].textContent).toContain("2 / 2");
    expect(within(rows[1]).getByText("Ready")).toBeTruthy();
    expect(rows[3].textContent).toContain("1");
    expect(
      screen.getByText(
        "Pipeline health, throughput, consumer lag and errors across all services.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Updated 12:34:56 UTC")).toBeTruthy();
    expect(screen.getByText("1 restart in the last 15 minutes")).toBeTruthy();
    for (const name of [
      "Services",
      "Throughput and errors",
      "Consumer lag",
      "Latency",
    ])
      expect(screen.getByRole("region", { name })).toBeTruthy();
    const throughput = screen.getByRole("region", {
      name: "Throughput and errors",
    });
    expect(within(throughput).getByText("Ingest events")).toBeTruthy();
    expect(within(throughput).getByText("1,234.5")).toBeTruthy();
    expect(within(throughput).getByText("events/s, 1 min rate")).toBeTruthy();
    for (const label of [
      "Accepted",
      "Rejected",
      "Rate limited",
      "Shed",
      "Dependency unavailable",
    ])
      expect(within(throughput).getByText(label)).toBeTruthy();
    expect(within(throughput).queryByText("Unavailable")).toBeNull();
    expect(within(throughput).getByText("1,200.0 events/s")).toBeTruthy();
    expect(within(throughput).getByText("4.3 events/s")).toBeTruthy();
    expect(within(throughput).getByText("HTTP error ratio")).toBeTruthy();
    expect(within(throughput).getByText("0.42%")).toBeTruthy();
    expect(within(throughput).getByText("5 min")).toBeTruthy();
    for (const label of ["5xx", "429 rate limited", "503 unavailable"])
      expect(within(throughput).getByText(label)).toBeTruthy();
    expect(within(throughput).getByText("1.3 requests/s")).toBeTruthy();
    expect(within(throughput).getByText("DLQ events")).toBeTruthy();
    expect(within(throughput).getByText("0.1")).toBeTruthy();
    expect(within(throughput).getByText("events/s, 5 min rate")).toBeTruthy();
    const lag = screen.getByRole("table", { name: "Consumer lag" });
    const lagRows = within(lag).getAllByRole("row");
    expect(lagRows.map((r) => r.textContent)).toEqual([
      "Consumer groupLag (records)",
      "event-writer12,345",
      "detector0",
      "alert-writer7",
    ]);
    expect(
      screen.getByText(
        "Position-based: end offset minus next unprocessed offset, maximum across partitions.",
      ),
    ).toBeTruthy();
    const latency = screen.getByRole("region", { name: "Latency" });
    expect(within(latency).getByText("Detection delay p95")).toBeTruthy();
    expect(within(latency).getByText("245 ms")).toBeTruthy();
    expect(within(latency).getByText("ClickHouse write p95")).toBeTruthy();
    expect(within(latency).getByText("1.25 s")).toBeTruthy();
    expect(within(latency).getAllByText("5 min window")).toHaveLength(2);
    expect(
      screen.getByText(
        "Values come from Prometheus with fixed server-side queries. Use Grafana for history.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/No metrics recorded yet/)).toBeNull();
    expect(screen.queryByText(/Metrics source unavailable/)).toBeNull();
    const call = systemCalls(requests).at(-1)!;
    expect(new URL(call.url).search).toBe("");
    expect(call.method).toBe("GET");
    expect(call.cache).toBe("no-store");
  });

  it.each([
    [
      "all ready",
      () => names.map(() => ready()),
      "All services ready",
      "Ready",
      "badge-success",
    ],
    [
      "degraded and not ready",
      () =>
        names.map((_, i): Readiness =>
          i === 1
            ? { state: "degraded", ready_replicas: 1, probed_replicas: 2 }
            : i === 2
              ? { state: "not_ready", ready_replicas: 0, probed_replicas: 1 }
              : ready(),
        ),
      "2 of 5 services not ready",
      "Not ready",
      "badge-critical",
    ],
    [
      "only degraded",
      () =>
        names.map((_, i): Readiness =>
          i === 0
            ? { state: "degraded", ready_replicas: 1, probed_replicas: 2 }
            : ready(),
        ),
      "1 of 5 services not ready",
      "Degraded",
      "badge-warning",
    ],
    [
      "degraded plus unavailable",
      () =>
        names.map((_, i): Readiness =>
          i === 0
            ? { state: "degraded", ready_replicas: 1, probed_replicas: 2 }
            : i === 4
              ? { state: "unavailable", reason: "not_reachable" }
              : ready(),
        ),
      "2 of 5 services not ready",
      "Degraded",
      "badge-warning",
    ],
    [
      "all unavailable",
      () =>
        names.map((): Readiness => ({
          state: "unavailable",
          reason: "not_reachable",
        })),
      "Service health unavailable",
      "Unavailable",
      "badge",
    ],
  ])(
    "maps readiness (%s) to one headline and its badge",
    async (_, readiness, headline, badge, className) => {
      const states = readiness();
      const body = status();
      body.services = body.services.map((s, i) => ({
        ...s,
        readiness: states[i] as ReturnType<typeof ready>,
      }));
      await start(async () => Response.json(body));
      const summary = await screen.findByRole("region", { name: headline });
      const element = within(summary).getByText(badge, {
        selector: ".badge",
      });
      expect(element.className.split(" ")).toContain(className);
      expect(
        screen.getAllByRole("heading", {
          name: /services ready|services not ready|Service health unavailable/,
        }),
      ).toHaveLength(1);
    },
  );

  it("shows the metrics-source notice and readiness but never a zero when Prometheus is down", async () => {
    await start(async () =>
      Response.json(
        allMetrics("prometheus_unavailable", {
          state: "unavailable",
          reason: "prometheus_unavailable",
        }),
      ),
    );
    expect(
      await screen.findByText(
        "Metrics source unavailable. Service readiness is shown where available. Try refreshing in a moment.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("region", { name: "All services ready" }),
    ).toBeTruthy();
    expect(screen.getByText("Restart count unavailable")).toBeTruthy();
    const services = screen.getByRole("table", { name: "Services" });
    expect(within(services).getAllByText("Unavailable")).toHaveLength(10);
    expect(within(services).getAllByText("Ready")).toHaveLength(5);
    expect(screen.queryByText("No metrics recorded yet")).toBeNull();
    expectNoFabricatedZeros();
  });

  it("shows No data yet badges and the empty state when nothing has been recorded", async () => {
    await start(async () => Response.json(allMetrics("no_data")));
    expect(
      await screen.findByRole("heading", { name: "No metrics recorded yet" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Services have not reported metrics yet. Start the observability stack with the documented command, send synthetic events, then refresh.",
      ),
    ).toBeTruthy();
    expect(screen.getAllByText("No data yet").length).toBeGreaterThan(10);
    expect(screen.queryByText(/Metrics source unavailable/)).toBeNull();
    expectNoFabricatedZeros();
  });

  it("renders partial data: measured zeros as 0, absent counters as No data yet, no page empty state", async () => {
    const body = status({
      http_errors: {
        ratio: unavail("no_data"),
        status_5xx: unavail("no_data"),
        status_429: unavail("no_data"),
        status_503: unavail("no_data"),
      },
      dlq_rate: unavail("no_data"),
    });
    await start(async () => Response.json(body));
    const throughput = await screen.findByRole("region", {
      name: "Throughput and errors",
    });
    expect(within(throughput).getAllByText("No data yet")).toHaveLength(5);
    expect(screen.queryByText("No metrics recorded yet")).toBeNull();
    const lag = screen.getByRole("table", { name: "Consumer lag" });
    expect(within(lag).getAllByRole("row")[2].textContent).toBe("detector0");
    expect(within(throughput).getByText("1,234.5")).toBeTruthy();
  });

  it("renders a partial_coverage lag row as Unavailable, never 0", async () => {
    const body = status({
      consumer_lag: [
        { group: "event-writer", lag: avail(12345) },
        { group: "detector", lag: unavail("partial_coverage") },
        { group: "alert-writer", lag: avail(7) },
      ],
    });
    await start(async () => Response.json(body));
    const lag = await screen.findByRole("table", { name: "Consumer lag" });
    const detector = within(lag).getAllByRole("row")[2];
    expect(detector.textContent).toBe("detectorUnavailable");
    expect(detector.textContent).not.toMatch(/\d/);
    expect(within(lag).getAllByRole("row")[1].textContent).toBe(
      "event-writer12,345",
    );
    expect(within(lag).getAllByRole("row")[3].textContent).toBe(
      "alert-writer7",
    );
  });

  it("keeps the header while loading and shows no numbers or toolbar", async () => {
    await start(() => new Promise<Response>(() => {}));
    expect(await screen.findByText("Loading system status…")).toBeTruthy();
    expect(
      screen.getByText("Loading system status…").getAttribute("role"),
    ).toBe("status");
    expect(screen.getByRole("heading", { name: "System" })).toBeTruthy();
    expect(
      screen.getByText(
        "Pipeline health, throughput, consumer lag and errors across all services.",
      ),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Refresh system status" }),
    ).toBeNull();
    expect(document.querySelector("main")!.textContent).not.toMatch(/\d/);
  });

  it("shows the error state without data and retries with the real result", async () => {
    let n = 0;
    const { requests } = await start(async () =>
      ++n === 1
        ? new Response("private prometheus internals", { status: 503 })
        : Response.json(status()),
    );
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain(
      "Could not load system status. Try again.",
    );
    expect(document.body.textContent).not.toContain("private prometheus");
    expect(screen.queryByRole("table", { name: "Services" })).toBeNull();
    fireEvent.click(
      within(alert).getByRole("button", {
        name: "Retry loading system status",
      }),
    );
    expect(await screen.findByRole("table", { name: "Services" })).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(systemCalls(requests)).toHaveLength(2);
  });

  it("rejects a malformed body instead of rendering partial values", async () => {
    const body = status();
    body.services = body.services.slice(0, 4);
    await start(async () => Response.json(body));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Could not load system status. Try again.",
    );
    expect(screen.queryByText("1,234.5")).toBeNull();
  });

  it("rejects an unavailable metric that smuggles a value", async () => {
    const body = status({
      dlq_rate: { state: "unavailable", reason: "no_data", value: 0 },
    });
    await start(async () => Response.json(body));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Could not load system status. Try again.",
    );
  });

  it("polls every 15 s, keeps values when a background refetch fails and announces transitions", async () => {
    vi.useFakeTimers();
    let n = 0;
    const { requests } = await start(async () =>
      ++n === 2 ? new Response("{}", { status: 503 }) : Response.json(status()),
    );
    await settle();
    expect(screen.getByText("1,234.5")).toBeTruthy();
    expect(systemCalls(requests)).toHaveLength(1);
    expect(liveRegion().textContent).toBe("");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(14000);
    });
    expect(systemCalls(requests)).toHaveLength(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    await settle();
    expect(systemCalls(requests)).toHaveLength(2);
    expect(
      screen.getByText("Showing data from 12:34:56 UTC. Live refresh failed."),
    ).toBeTruthy();
    expect(screen.getByText("1,234.5")).toBeTruthy();
    expect(screen.getByText("Updated 12:34:56 UTC")).toBeTruthy();
    expect(liveRegion().textContent).toBe("Live refresh failed");
    expect(liveRegion().getAttribute("role")).toBe("status");
    fireEvent.click(
      screen.getByRole("button", { name: "Retry loading system status" }),
    );
    await settle();
    expect(systemCalls(requests)).toHaveLength(3);
    expect(screen.queryByText(/Live refresh failed\./)).toBeNull();
    expect(liveRegion().textContent).toBe("Live refresh restored");
  });

  it("stops polling after a 403 so refusals are not re-audited every 15 s", async () => {
    vi.useFakeTimers();
    const { requests } = await start(async () =>
      forbidden("System status is limited to operator administrators."),
    );
    await settle();
    expect(
      screen.getByRole("heading", { name: "Operator access required" }),
    ).toBeTruthy();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(46000);
    });
    expect(systemCalls(requests)).toHaveLength(1);
  });

  it("marks data stale 60 s after the last successful fetch even when observed_at is newer", async () => {
    vi.useFakeTimers();
    let n = 0;
    await start(() =>
      ++n === 1
        ? Promise.resolve(
            Response.json(status({ observed_at: "2099-01-01T08:00:00Z" })),
          )
        : new Promise<Response>(() => {}),
    );
    await settle();
    expect(screen.getByText("1,234.5")).toBeTruthy();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(55000);
    });
    expect(screen.queryByText(/Live refresh failed\./)).toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(11000);
    });
    await settle();
    expect(
      screen.getByText("Showing data from 08:00:00 UTC. Live refresh failed."),
    ).toBeTruthy();
    expect(screen.getByText("1,234.5")).toBeTruthy();
    expect(liveRegion().textContent).toBe("Live refresh failed");
  });

  it("uses a primary Refresh button that is disabled while fetching and announces the update", async () => {
    let n = 0;
    let finish!: (response: Response) => void;
    const { requests } = await start(() =>
      ++n === 1
        ? Promise.resolve(Response.json(status()))
        : new Promise<Response>((resolve) => {
            finish = resolve;
          }),
    );
    const refresh = (await screen.findByRole("button", {
      name: "Refresh system status",
    })) as HTMLButtonElement;
    expect(refresh.className).not.toContain("secondary");
    expect(refresh.disabled).toBe(false);
    fireEvent.click(refresh);
    await waitFor(() => expect(refresh.disabled).toBe(true));
    await act(async () =>
      finish(
        Response.json(
          status({ ingest: { ...status().ingest, total: avail(99) } }),
        ),
      ),
    );
    await waitFor(() => expect(refresh.disabled).toBe(false));
    expect(screen.getByText("99.0")).toBeTruthy();
    expect(liveRegion().textContent).toBe("System status updated");
    expect(systemCalls(requests)).toHaveLength(2);
  });

  it.each([
    ["https://grafana.example.test/d/sentinel", true],
    ["http://127.0.0.1:3000/", true],
    [undefined, false],
    ["javascript:alert(1)", false],
  ])(
    "renders the Grafana link for %s only when http(s)",
    async (url, shown) => {
      await start(async () =>
        Response.json(
          url === undefined ? status() : status({ grafana_url: url }),
        ),
      );
      await screen.findByRole("table", { name: "Services" });
      const link = screen.queryByRole("link", {
        name: "Open Grafana dashboards (opens in a new tab)",
      });
      if (!shown) {
        expect(link).toBeNull();
        expect(document.querySelector('main a[href^="javascript"]')).toBeNull();
        return;
      }
      expect(link!.getAttribute("href")).toBe(url);
      expect(link!.getAttribute("target")).toBe("_blank");
      expect(link!.getAttribute("rel")).toBe("noopener noreferrer");
      expect(
        within(link!).getByText("(opens in a new tab)").className,
      ).toContain("visually-hidden");
    },
  );

  it("renders services as a labelled card list below 600px", async () => {
    vi.stubGlobal("matchMedia", (query: string) => ({
      matches: query === "(max-width: 599px)",
      addEventListener() {},
      removeEventListener() {},
    }));
    await start(async () => Response.json(status()));
    const list = await screen.findByRole("list", { name: "Services" });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(5);
    expect(items[0].textContent).toContain("ingest");
    expect(items[0].textContent).toContain("2 / 2");
    expect(items[0].querySelector("dl")).toBeTruthy();
    expect(screen.queryByRole("table", { name: "Services" })).toBeNull();
    expect(screen.getByRole("table", { name: "Consumer lag" })).toBeTruthy();
  });

  it("clears System data on sign out", async () => {
    const { cache, requests } = await start(
      async () => Response.json(status()),
      "admin",
      "/system",
      true,
    );
    await screen.findByRole("table", { name: "Services" });
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(systemCalls(requests).every((r) => r.signal.aborted)).toBe(true);
    expect(screen.queryByText("1,234.5")).toBeNull();
  });
});
