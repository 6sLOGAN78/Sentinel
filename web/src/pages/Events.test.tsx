import { StrictMode } from "react";
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
) {
  window.history.replaceState(null, "", "/events");
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
});
