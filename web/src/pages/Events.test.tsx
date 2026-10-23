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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { createAPIClient } from "../api/client";
import type { paths } from "../api/schema";

type EventPage =
  paths["/api/v1/events"]["get"]["responses"][200]["content"]["application/json"];
function page(source = "Native event", next: string | null = null): EventPage {
  return {
    items: [
      {
        schema_version: 1,
        event_id: "00000000-0000-4000-8000-000000000001",
        tenant_id: "events-tenant",
        occurred_at: "2026-10-23T03:00:00.123Z",
        source,
        event_type: "auth.failure",
        source_ip: "2001:db8::44",
        outcome: "failure",
      },
    ],
    next_cursor: next,
    has_more: next !== null,
  };
}
async function start(
  list: (r: Request) => Promise<Response> = async () => Response.json(page()),
  strict = false,
  initialURL = "/events?limit=1",
) {
  window.history.replaceState(null, "", initialURL);
  const requests: Request[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (r: Request) => {
      if (new URL(r.url).pathname === "/api/v1/session")
        return Response.json({
          subject: "events-operator",
          tenant_id: "events-tenant",
          role: "viewer",
          expires_at: new Date(Date.now() + 600000).toISOString(),
        });
      if (new URL(r.url).pathname === "/api/v1/alerts")
        return Response.json({ items: [], next_cursor: null, has_more: false });
      requests.push(r);
      return list(r);
    }),
  );
  const cache = new QueryClient();
  const app = <App queryClient={cache} />;
  render(strict ? <StrictMode>{app}</StrictMode> : app);
  fireEvent.change(screen.getByLabelText("Development access token"), {
    target: { value: "event-test-credential" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await screen.findByRole("heading", { name: "Events" });
  return { requests, cache };
}
describe("event investigation through the actual App", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    window.history.replaceState(null, "", "/alerts");
  });
  it.each([
    ["source_ip=bad", "Enter a valid IP address."],
    ["limit=201", "Choose a valid UTC time range of at most 7 days."],
    [
      "from=2026-10-23T00:00:00Z&to=2026-10-22T00:00:00Z",
      "Choose a valid UTC time range of at most 7 days.",
    ],
  ])(
    "validates initial deep link %s without permanent loading",
    async (q, error) => {
      const { requests } = await start(undefined, false, `/events?${q}`);
      expect(screen.getByRole("alert").textContent).toBe(error);
      expect(screen.queryByText("Loading events…")).toBeNull();
      expect(requests).toHaveLength(0);
      fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
      await screen.findByText("Native event");
      expect(screen.queryByRole("alert")).toBeNull();
    },
  );
  it.each([
    ["source_ip=bad", "Enter a valid IP address."],
    ["limit=201", "Choose a valid UTC time range of at most 7 days."],
    [
      "from=2026-10-23T00:00:00Z&to=2026-10-22T00:00:00Z",
      "Choose a valid UTC time range of at most 7 days.",
    ],
  ])("validates history deep link %s without a request", async (q, error) => {
    const { requests } = await start();
    await screen.findByText("Native event");
    act(() => {
      window.history.pushState(null, "", `/events?${q}`);
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(screen.getByRole("alert").textContent).toBe(error);
    expect(screen.queryByText("Loading events…")).toBeNull();
    expect(requests).toHaveLength(1);
    expect(screen.queryByText("Native event")).toBeNull();
  });
  it("loads real event rows through the guarded client and safe text", async () => {
    const payload = "<img src=x onerror=alert(1)>";
    const { requests } = await start(async () => Response.json(page(payload)));
    await screen.findByText(payload);
    expect(document.querySelector("img")).toBeNull();
    expect(requests[0].url).toContain("/api/v1/events?");
    expect(requests[0].headers.get("Authorization")).toBe(
      "Bearer event-test-credential",
    );
    expect(requests[0].cache).toBe("no-store");
    expect(screen.getByText("2001:db8::44")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Events" }).getAttribute("aria-current"),
    ).toBe("page");
    expect(screen.getByRole("link", { name: "Alerts" }).style.background).toBe(
      "transparent",
    );
    expect(
      screen.getByRole("link", { name: "Events" }).style.background,
    ).not.toBe("transparent");
  });
  it("shows loading and then the exact empty state", async () => {
    let release!: (r: Response) => void;
    await start(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    expect(screen.getByText("Loading events…")).toBeTruthy();
    await act(async () =>
      release(Response.json({ items: [], next_cursor: null, has_more: false })),
    );
    expect(
      await screen.findByText("No events match these filters"),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Broaden the time range or clear filters to search again.",
      ),
    ).toBeTruthy();
  });
  it("shows dependency errors and retries without fabricated empty data", async () => {
    let fail = true;
    await start(async () =>
      fail ? new Response("", { status: 503 }) : Response.json(page()),
    );
    await screen.findByText("Could not load events. Try again.");
    expect(screen.queryByText("No events match these filters")).toBeNull();
    fail = false;
    fireEvent.click(
      screen.getByRole("button", { name: "Retry loading events" }),
    );
    await screen.findByText("Native event");
  });
  it("applies labelled UTC type and outcome filters with minute precision", async () => {
    const { requests } = await start();
    await screen.findByText("Native event");
    fireEvent.change(screen.getByLabelText("From (UTC)"), {
      target: { value: "2026-10-22T00:00" },
    });
    fireEvent.change(screen.getByLabelText("To (UTC)"), {
      target: { value: "2026-10-23T00:00" },
    });
    fireEvent.change(screen.getByLabelText("Event type"), {
      target: { value: "net.request" },
    });
    fireEvent.change(screen.getByLabelText("Outcome"), {
      target: { value: "success" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() => expect(requests).toHaveLength(2));
    const q = new URL(requests[1].url).searchParams;
    expect(q.get("from")).toBe("2026-10-22T00:00:00.000Z");
    expect(q.get("to")).toBe("2026-10-23T00:00:00.000Z");
    expect(q.get("event_type")).toBe("net.request");
    expect(q.get("outcome")).toBe("success");
  });
  it("debounces IP search for 300ms and cancels the obsolete request", async () => {
    const { requests } = await start(async () => new Promise(() => {}));
    await waitFor(() => expect(requests).toHaveLength(1));
    fireEvent.change(screen.getByLabelText("Source IP"), {
      target: { value: "2001:db8::44" },
    });
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(requests).toHaveLength(1);
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(new URL(requests[1].url).searchParams.get("source_ip")).toBe(
      "2001:db8::44",
    );
    expect(requests[0].signal.aborted).toBe(true);
  });
  it("resets page cursors on filters and supports bounded previous pages", async () => {
    const { requests } = await start(async (r) =>
      Response.json(
        new URL(r.url).searchParams.has("cursor")
          ? page("Second event")
          : page("First event", "opaque-next"),
      ),
    );
    await screen.findByText("First event");
    expect(
      (
        screen.getByRole("button", {
          name: "Previous page",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    await screen.findByText("Second event");
    expect(new URL(requests.at(-1)!.url).searchParams.get("cursor")).toBe(
      "opaque-next",
    );
    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
    await screen.findByText("First event");
    await waitFor(() =>
      expect(
        (screen.getByRole("button", { name: "Next page" }) as HTMLButtonElement)
          .disabled,
      ).toBe(false),
    );
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    await screen.findByText("Second event");
    fireEvent.change(screen.getByLabelText("Event type"), {
      target: { value: "auth.success" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await screen.findByText("First event");
    expect(new URL(requests.at(-1)!.url).searchParams.has("cursor")).toBe(
      false,
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Previous page",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
  it("clears cache and aborts pending work on sign out", async () => {
    const { requests, cache } = await start(async () => new Promise(() => {}));
    await waitFor(() => expect(requests).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await screen.findByRole("heading", { name: "Sign in to Sentinel" });
    expect(requests[0].signal.aborted).toBe(true);
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(window.location.search).not.toContain("cursor");
  });
  it("keeps canonical UTC filter URL during StrictMode replay", async () => {
    await start(undefined, true);
    await screen.findByText("Native event");
    expect(new URLSearchParams(window.location.search).get("to")).toMatch(/Z$/);
    expect(screen.getByRole("heading", { name: "Events" })).toBeTruthy();
  });
  it("navigates between Alerts and Events and follows Back", async () => {
    await start();
    await screen.findByText("Native event");
    fireEvent.click(screen.getByRole("link", { name: "Alerts" }));
    await screen.findByRole("heading", { name: "Alerts" });
    act(() => {
      window.history.replaceState(null, "", "/events");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await screen.findByRole("heading", { name: "Events" });
  });
  it("retains the last real page if its refresh fails", async () => {
    let fail = false;
    const { cache } = await start(async () =>
      fail ? new Response("", { status: 503 }) : Response.json(page()),
    );
    await screen.findByText("Native event");
    fail = true;
    await act(async () => {
      await cache.invalidateQueries();
    });
    await screen.findByText("Could not load events. Try again.");
    expect(screen.getByText("Native event")).toBeTruthy();
    expect(screen.queryByText("No events match these filters")).toBeNull();
  });
  it("rejects invalid IP and ranges before changing the current page", async () => {
    const { requests } = await start();
    await screen.findByText("Native event");
    fireEvent.change(screen.getByLabelText("Source IP"), {
      target: { value: "invalid-ip" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    expect(screen.getByText("Enter a valid IP address.")).toBeTruthy();
    expect(requests).toHaveLength(1);
    fireEvent.change(screen.getByLabelText("Source IP"), {
      target: { value: "" },
    });
    fireEvent.change(screen.getByLabelText("From (UTC)"), {
      target: { value: "2026-10-01T00:00" },
    });
    fireEvent.change(screen.getByLabelText("To (UTC)"), {
      target: { value: "2026-10-23T00:00" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    expect(
      screen.getByText("Choose a valid UTC time range of at most 7 days."),
    ).toBeTruthy();
    expect(requests).toHaveLength(1);
  });
});

describe("guarded event client boundary", () => {
  afterEach(() => vi.unstubAllGlobals());
  function client() {
    const lifetime = new AbortController();
    const credential = vi.fn(() => "client-unit-credential");
    const unauthorized = vi.fn();
    let current = true;
    return {
      api: createAPIClient(
        credential,
        lifetime.signal,
        () => current,
        unauthorized,
      ),
      lifetime,
      credential,
      unauthorized,
      expire: () => {
        current = false;
      },
    };
  }
  const detailID = "00000000-0000-4000-8000-000000000001";
  it("gets a typed event detail with the existing private transport", async () => {
    const fetcher = vi.fn<(request: Request) => Promise<Response>>(async () =>
      Response.json(page().items[0]),
    );
    vi.stubGlobal("fetch", fetcher);
    const active = client();
    expect(await active.api.eventDetail(detailID)).toEqual(page().items[0]);
    const request = fetcher.mock.calls[0][0] as Request;
    expect(new URL(request.url).pathname).toBe(`/api/v1/events/${detailID}`);
    expect(new URL(request.url).search).toBe("");
    expect(request.headers.get("Authorization")).toBe(
      "Bearer client-unit-credential",
    );
    expect(request.cache).toBe("no-store");
  });
  it("guards event detail credentials before expired session or caller access", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    const ended = client();
    ended.expire();
    await expect(ended.api.eventDetail(detailID)).rejects.toMatchObject({
      name: "AbortError",
    });
    expect(ended.credential).not.toHaveBeenCalled();
    const active = client(),
      caller = new AbortController();
    caller.abort();
    await expect(
      active.api.eventDetail(detailID, caller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(active.credential).not.toHaveBeenCalled();
    expect(fetcher).not.toHaveBeenCalled();
  });
  it.each(["caller", "session"])(
    "guards event detail parsed bodies after %s cancellation",
    async (which) => {
      let release!: (text: string) => void;
      const response = Response.json(page().items[0]);
      vi.spyOn(response, "text").mockImplementation(
        () =>
          new Promise((resolve) => {
            release = resolve;
          }),
      );
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response),
      );
      const active = client(),
        caller = new AbortController();
      const rejected = expect(
        active.api.eventDetail(detailID, caller.signal),
      ).rejects.toMatchObject({ name: "AbortError" });
      await waitFor(() => expect(release).toBeTypeOf("function"));
      if (which === "caller") caller.abort();
      else {
        active.expire();
        active.lifetime.abort();
      }
      release(JSON.stringify(page().items[0]));
      await rejected;
    },
  );
  it.each([401, 404, 503])(
    "keeps detail status %i and revokes only current401",
    async (status) => {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => new Response("", { status })),
      );
      const active = client();
      await expect(active.api.eventDetail(detailID)).rejects.toMatchObject({
        status,
      });
      expect(active.unauthorized).toHaveBeenCalledTimes(status === 401 ? 1 : 0);
    },
  );
  it.each([
    null,
    {},
    { ...page().items[0], event_id: "wrong" },
    { ...page().items[0], tenant_id: "" },
  ])("rejects malformed event detail %j", async (body) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(body)),
    );
    await expect(client().api.eventDetail(detailID)).rejects.toMatchObject({
      status: 503,
    });
  });
  it("checks old session and caller cancellation before obtaining credentials", async () => {
    const transport = vi.fn(async () => Response.json(page()));
    vi.stubGlobal("fetch", transport);
    const ended = client();
    ended.expire();
    await expect(ended.api.events()).rejects.toMatchObject({
      name: "AbortError",
    });
    expect(ended.credential).not.toHaveBeenCalled();
    expect(transport).not.toHaveBeenCalled();
    const active = client(),
      caller = new AbortController();
    caller.abort();
    await expect(active.api.events({}, caller.signal)).rejects.toMatchObject({
      name: "AbortError",
    });
    expect(active.credential).not.toHaveBeenCalled();
  });
  it.each(["caller", "session"])(
    "rejects stale data after body parsing when %s ends",
    async (which) => {
      let release!: (text: string) => void;
      const response = Response.json(page());
      vi.spyOn(response, "text").mockImplementation(
        () =>
          new Promise((resolve) => {
            release = resolve;
          }),
      );
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response),
      );
      const active = client(),
        caller = new AbortController();
      const pending = active.api.events({}, caller.signal);
      const rejected = expect(pending).rejects.toMatchObject({
        name: "AbortError",
      });
      await waitFor(() => expect(release).toBeTypeOf("function"));
      if (which === "caller") caller.abort();
      else {
        active.expire();
        active.lifetime.abort();
      }
      release(JSON.stringify(page()));
      await rejected;
    },
  );
  it("revokes the current session for401 and preserves503 as a dependency error", async () => {
    for (const status of [401, 503]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => new Response("", { status })),
      );
      const active = client();
      await expect(active.api.events()).rejects.toMatchObject({ status });
      expect(active.unauthorized).toHaveBeenCalledTimes(status === 401 ? 1 : 0);
    }
  });
  it.each([
    { items: null, has_more: false, next_cursor: null },
    {
      items: Array.from({ length: 201 }, () => page().items[0]),
      has_more: false,
      next_cursor: null,
    },
    { ...page(), has_more: true, next_cursor: null },
    { ...page(), next_cursor: "unexpected" },
    { ...page(), has_more: true, next_cursor: "x".repeat(2049) },
  ])("rejects malformed or unbounded event page %j", async (body) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(body)),
    );
    await expect(client().api.events()).rejects.toMatchObject({ status: 503 });
  });
});

describe("safe event detail dialog through actual App", () => {
  const id = "00000000-0000-4000-8000-000000000001";
  const payload =
    '<img src=x onerror="globalThis.__eventXSS=1"><script>globalThis.__eventXSS=1</script>';
  function event() {
    return {
      ...page().items[0],
      attributes: { payload, long: "recorded-" + "x".repeat(2200) },
    };
  }
  const showDescriptor = Object.getOwnPropertyDescriptor(
    HTMLDialogElement.prototype,
    "showModal",
  );
  const closeDescriptor = Object.getOwnPropertyDescriptor(
    HTMLDialogElement.prototype,
    "close",
  );
  beforeEach(() => {
    Object.defineProperties(HTMLDialogElement.prototype, {
      showModal: {
        configurable: true,
        value() {
          this.setAttribute("open", "");
        },
      },
      close: {
        configurable: true,
        value() {
          this.removeAttribute("open");
        },
      },
    });
  });
  afterEach(() => {
    cleanup();
    if (showDescriptor)
      Object.defineProperty(
        HTMLDialogElement.prototype,
        "showModal",
        showDescriptor,
      );
    else Reflect.deleteProperty(HTMLDialogElement.prototype, "showModal");
    if (closeDescriptor)
      Object.defineProperty(
        HTMLDialogElement.prototype,
        "close",
        closeDescriptor,
      );
    else Reflect.deleteProperty(HTMLDialogElement.prototype, "close");
    vi.unstubAllGlobals();
    window.history.replaceState(null, "", "/alerts");
  });
  async function open(
    detail: (r: Request) => Promise<Response> = async () =>
      Response.json(event()),
    strict = false,
    deep = false,
  ) {
    const result = await start(
      async (r) =>
        new URL(r.url).pathname === `/api/v1/events/${id}`
          ? detail(r)
          : Response.json(page()),
      strict,
      `/events?limit=1${deep ? `&event_id=${id}` : ""}`,
    );
    await screen.findByText("Native event");
    let trigger: HTMLElement | undefined;
    if (!deep) {
      trigger = screen.getByRole("button", { name: `View event ${id}` });
      trigger.focus();
      fireEvent.click(trigger);
    }
    return {
      ...result,
      trigger,
      dialog: await screen.findByRole("dialog", { name: "Event details" }),
    };
  }
  it("renders malicious attributes as pre/code text and restores exact trigger on Escape", async () => {
    const { dialog, trigger, requests } = await open();
    await within(dialog).findByText(/recorded-xxx/);
    const code = dialog.querySelector("pre code")!;
    expect(JSON.parse(code.textContent!)).toEqual(event());
    expect(JSON.parse(code.textContent!).attributes.payload).toBe(payload);
    expect(code.textContent).toContain("2001:db8::44");
    expect(dialog.querySelector("img,script")).toBeNull();
    expect((globalThis as Record<string, unknown>).__eventXSS).toBeUndefined();
    expect((code.parentElement as HTMLElement).style.whiteSpace).toBe(
      "pre-wrap",
    );
    expect((code.parentElement as HTMLElement).style.overflowWrap).toBe(
      "anywhere",
    );
    expect(document.activeElement).toBe(
      within(dialog).getByRole("button", { name: "Close event details" }),
    );
    expect(new URLSearchParams(window.location.search).get("event_id")).toBe(
      id,
    );
    expect(requests.some((r) => new URL(r.url).pathname.endsWith(id))).toBe(
      true,
    );
    fireEvent(dialog, new Event("cancel", { cancelable: true }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(trigger);
    expect(new URLSearchParams(window.location.search).has("event_id")).toBe(
      false,
    );
  });
  it("keeps initial and StrictMode event deep links and follows back/forward", async () => {
    const { dialog } = await open(undefined, true, true);
    await within(dialog).findByText(/recorded-xxx/);
    expect(new URLSearchParams(window.location.search).get("event_id")).toBe(
      id,
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Close event details" }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    act(() => {
      window.history.pushState(null, "", `/events?limit=1&event_id=${id}`);
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await screen.findByRole("dialog", { name: "Event details" });
    act(() => {
      window.history.pushState(null, "", "/events?limit=1");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  async function signIn(credential = "accepted") {
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: credential },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  }
  it.each([false, true])(
    "keeps destination Alerts filters on real Back/Forward (selected=%s)",
    async (selected) => {
      const alertID = "a".repeat(64);
      const filters = {
        severity: "critical",
        status: "open",
        search: "needle",
        rule_id: "AUTH_BRUTE_FORCE",
        limit: "5",
      };
      const params = new URLSearchParams(filters);
      if (selected) params.set("alert_id", alertID);
      window.history.replaceState(null, "", "/alerts?" + params);
      const requests: Request[] = [];
      vi.stubGlobal(
        "fetch",
        vi.fn(async (request: Request) => {
          const path = new URL(request.url).pathname;
          if (path === "/api/v1/session")
            return Response.json({
              subject: "events-operator",
              tenant_id: "events-tenant",
              role: "viewer",
              expires_at: new Date(Date.now() + 600000).toISOString(),
            });
          requests.push(request);
          if (path === "/api/v1/alerts")
            return Response.json({
              items: [],
              next_cursor: null,
              has_more: false,
            });
          if (path.startsWith("/api/v1/alerts/"))
            return new Response("", { status: 404 });
          return Response.json(page());
        }),
      );
      render(
        <StrictMode>
          <App queryClient={new QueryClient()} />
        </StrictMode>,
      );
      await signIn();
      await screen.findByRole("heading", { name: "Alerts" });
      if (selected)
        await screen.findByRole("dialog", { name: "Alert details" });
      fireEvent.click(screen.getByRole("link", { name: "Events" }));
      await screen.findByRole("heading", { name: "Events" });
      await screen.findByText("Native event");
      const eventURL = window.location.href;
      const before = requests.length;
      async function travel(direction: "back" | "forward") {
        await act(async () => {
          await new Promise<void>((resolve) => {
            window.addEventListener("popstate", () => resolve(), {
              once: true,
            });
            window.history[direction]();
          });
        });
      }
      await travel("back");
      await screen.findByRole("heading", { name: "Alerts" });
      const restored = new URLSearchParams(window.location.search);
      for (const [key, value] of Object.entries(filters))
        expect(restored.get(key)).toBe(value);
      expect(
        (screen.getByLabelText("Severity") as HTMLInputElement).value,
      ).toBe("critical");
      expect((screen.getByLabelText("Status") as HTMLInputElement).value).toBe(
        "open",
      );
      expect(
        (screen.getByLabelText("Search alerts") as HTMLInputElement).value,
      ).toBe("needle");
      await waitFor(() =>
        expect(
          requests
            .slice(before)
            .some(
              (request) => new URL(request.url).pathname === "/api/v1/alerts",
            ),
        ).toBe(true),
      );
      for (const request of requests
        .slice(before)
        .filter(
          (request) => new URL(request.url).pathname === "/api/v1/alerts",
        )) {
        const query = new URL(request.url).searchParams;
        for (const [key, value] of Object.entries(filters))
          expect(query.get(key)).toBe(value);
        expect(query.has("alert_id")).toBe(false);
      }
      expect(restored.get("alert_id")).toBe(selected ? alertID : null);
      if (selected) {
        await screen.findByRole("dialog", { name: "Alert details" });
        expect(
          requests
            .slice(before)
            .some(
              (request) =>
                new URL(request.url).pathname === `/api/v1/alerts/${alertID}`,
            ),
        ).toBe(true);
      }
      await travel("forward");
      await screen.findByRole("heading", { name: "Events" });
      expect(window.location.href).toBe(eventURL);
      await screen.findByText("Native event");
    },
  );
  async function freshPage() {
    // A hard reload creates fresh modules/provider counters, but preserves the
    // browser URL/history. React cleanup is restored away by the caller below.
    vi.resetModules();
    const FreshApp = (await import("../App")).default;
    const cache = new QueryClient();
    render(
      <StrictMode>
        <FreshApp queryClient={cache} />
      </StrictMode>,
    );
    return cache;
  }
  async function failedLoginThenDetail() {
    window.history.replaceState(null, "", "/events?limit=1");
    const fixture = {
      identity: {
        subject: "events-operator",
        tenant_id: "events-tenant",
        role: "viewer",
      },
      requests: [] as Request[],
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (request: Request) => {
        if (new URL(request.url).pathname === "/api/v1/session") {
          if (request.headers.get("Authorization") === "Bearer rejected")
            return new Response("", { status: 401 });
          return Response.json({
            ...fixture.identity,
            expires_at: new Date(Date.now() + 600000).toISOString(),
          });
        }
        fixture.requests.push(request);
        return Response.json(
          new URL(request.url).pathname.endsWith(id) ? event() : page(),
        );
      }),
    );
    const cache = await freshPage();
    await signIn("rejected");
    await screen.findByText(
      "This access token could not be verified. Check it or generate a new token.",
    );
    await signIn();
    await screen.findByText("Native event");
    fireEvent.click(screen.getByRole("button", { name: `View event ${id}` }));
    await within(await screen.findByRole("dialog")).findByText(/recorded-xxx/);
    return {
      fixture,
      cache,
      url: window.location.href,
      state: structuredClone(window.history.state),
    };
  }
  it("restores the same verified event deep link after failed login and a fresh-page reload", async () => {
    const saved = await failedLoginThenDetail();
    const before = saved.fixture.requests.length;
    cleanup();
    // Real browser reload does not run the old React unmount URL cleanup.
    window.history.replaceState(saved.state, "", saved.url);
    await freshPage();
    await signIn();
    await screen.findByText("Native event");
    expect(new URLSearchParams(window.location.search).get("event_id")).toBe(
      id,
    );
    const dialog = await screen.findByRole("dialog", { name: "Event details" });
    await within(dialog).findByText(/recorded-xxx/);
    expect(
      saved.fixture.requests
        .slice(before)
        .some((request) => new URL(request.url).pathname.endsWith(id)),
    ).toBe(true);
  });
  it.each([
    { subject: "another-operator" },
    { tenant_id: "another-tenant" },
    { role: "analyst" },
  ])(
    "rejects a persisted event deep link for a different verified identity %j",
    async (changed) => {
      const saved = await failedLoginThenDetail();
      cleanup();
      window.history.replaceState(saved.state, "", saved.url);
      Object.assign(saved.fixture.identity, changed);
      const before = saved.fixture.requests.length;
      await freshPage();
      await signIn();
      await screen.findByText("Native event");
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(new URLSearchParams(window.location.search).has("event_id")).toBe(
        false,
      );
      expect(
        saved.fixture.requests
          .slice(before)
          .some((request) => new URL(request.url).pathname.endsWith(id)),
      ).toBe(false);
    },
  );
  it("rejects old event history after same-page logout and same-identity sign-in", async () => {
    const saved = await failedLoginThenDetail();
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await screen.findByRole("heading", { name: "Sign in to Sentinel" });
    expect(saved.cache.getQueryCache().getAll()).toHaveLength(0);
    expect(new URLSearchParams(window.location.search).has("event_id")).toBe(
      false,
    );
    await signIn();
    await screen.findByText("Native event");
    const before = saved.fixture.requests.length;
    act(() => {
      window.history.pushState(saved.state, "", saved.url);
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(new URLSearchParams(window.location.search).has("event_id")).toBe(
      false,
    );
    expect(
      saved.fixture.requests
        .slice(before)
        .some((request) => new URL(request.url).pathname.endsWith(id)),
    ).toBe(false);
  });
  it("rejects prior-document event history after the restored session signs out", async () => {
    const saved = await failedLoginThenDetail();
    cleanup();
    window.history.replaceState(saved.state, "", saved.url);
    const restoredCache = await freshPage();
    await signIn();
    await within(await screen.findByRole("dialog")).findByText(/recorded-xxx/);
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await screen.findByRole("heading", { name: "Sign in to Sentinel" });
    expect(restoredCache.getQueryCache().getAll()).toHaveLength(0);
    await signIn();
    await screen.findByText("Native event");
    const before = saved.fixture.requests.length;
    act(() => {
      window.history.pushState(saved.state, "", saved.url);
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(new URLSearchParams(window.location.search).has("event_id")).toBe(
      false,
    );
    expect(
      saved.fixture.requests
        .slice(before)
        .some((request) => new URL(request.url).pathname.endsWith(id)),
    ).toBe(false);
  });
  it("shows loading, honest dependency error and retry", async () => {
    let release!: (r: Response) => void;
    let first = true;
    const { dialog } = await open(async () =>
      first
        ? new Promise((resolve) => {
            release = resolve;
          })
        : Response.json(event()),
    );
    expect(within(dialog).getByText("Loading event details…")).toBeTruthy();
    await act(async () => release(new Response("", { status: 503 })));
    await within(dialog).findByText("Could not load event details. Try again.");
    first = false;
    fireEvent.click(
      within(dialog).getByRole("button", {
        name: "Retry loading event details",
      }),
    );
    await within(dialog).findByText(/recorded-xxx/);
  });
  it("shows a tenant-safe missing-event state", async () => {
    const { dialog } = await open(
      async () => new Response("", { status: 404 }),
    );
    await within(dialog).findByText(
      "This event was not found for your tenant.",
    );
    expect(dialog.querySelector("pre")).toBeNull();
  });
  it("aborts detail and clears its URL and cached JSON on logout", async () => {
    const { requests, cache } = await open(async () => new Promise(() => {}));
    const request = requests.find((r) => new URL(r.url).pathname.endsWith(id))!;
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await screen.findByRole("heading", { name: "Sign in to Sentinel" });
    expect(request.signal.aborted).toBe(true);
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(new URLSearchParams(window.location.search).has("event_id")).toBe(
      false,
    );
  });
});
