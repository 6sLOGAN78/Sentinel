import { StrictMode } from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../App";

const rules = {
  rules: [true, false].map((enabled, i) => ({
    rule_id: i ? "PORT_SCAN" : "AUTH_BRUTE_FORCE",
    version: 12,
    name: i ? "Port scan" : "Authentication brute force",
    description: "Recorded startup definition <img src=x>",
    severity: "high",
    enabled,
    parameters: { threshold: 17, window_ms: 301500, cooldown_ms: 60000 },
  })),
};
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});
async function start(
  list: (request: Request) => Promise<Response>,
  role = "viewer",
) {
  window.history.replaceState(null, "", "/rules");
  const requests: Request[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (request: Request) => {
      if (new URL(request.url).pathname === "/api/v1/session")
        return Response.json({
          subject: "rules-operator",
          tenant_id: "rules-tenant",
          role,
          expires_at: new Date(Date.now() + 600000).toISOString(),
        });
      requests.push(request);
      if (new URL(request.url).pathname === "/api/v1/alerts")
        return Response.json({ items: [], next_cursor: null, has_more: false });
      return list(request);
    }),
  );
  const cache = new QueryClient();
  render(
    <StrictMode>
      <App queryClient={cache} />
    </StrictMode>,
  );
  fireEvent.change(screen.getByLabelText("Development access token"), {
    target: { value: "rules-test-credential" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  return { cache, requests };
}
describe("read-only startup Rules through the real App/session/client", () => {
  it.each(["viewer", "analyst", "admin"])(
    "renders exact definitions for %s with keyboard navigation",
    async (role) => {
      const { requests } = await start(async () => Response.json(rules), role);
      await screen.findByRole("heading", { name: "Rules" });
      await screen.findByText("Authentication brute force");
      expect(screen.getByText("Enabled")).toBeTruthy();
      expect(screen.getByText("Disabled")).toBeTruthy();
      expect(screen.getAllByText(/301500/)).toHaveLength(2);
      expect(screen.getAllByText(/60000/)).toHaveLength(2);
      expect(screen.getAllByText(/Version: 12/)).toHaveLength(2);
      expect(screen.getAllByText(/Threshold: 17/)).toHaveLength(2);
      expect(document.querySelector("main img")).toBeNull();
      const link = screen.getByRole("link", { name: "Rules" });
      link.focus();
      expect(document.activeElement).toBe(link);
      expect(link.getAttribute("aria-current")).toBe("page");
      fireEvent.click(screen.getByRole("link", { name: "Alerts" }));
      await screen.findByRole("heading", { name: "Alerts" });
      await act(async () => {
        await new Promise<void>((resolve) => {
          window.addEventListener("popstate", () => resolve(), { once: true });
          window.history.back();
        });
      });
      await screen.findByRole("heading", { name: "Rules" });
      await screen.findByText("Authentication brute force");
      expect(requests.every((r) => r.method === "GET")).toBe(true);
      expect(
        screen.queryByRole("button", { name: /create|delete|edit|save/i }),
      ).toBeNull();
      expect(localStorage.length).toBe(0);
      expect(sessionStorage.length).toBe(0);
      expect(window.location.href).not.toContain("credential");
    },
  );
  it("shows loading, dependency error, retry and the exact empty copy", async () => {
    let release!: (response: Response) => void;
    let first = true;
    await start(async () =>
      first
        ? new Promise((resolve) => {
            release = resolve;
          })
        : Response.json({ rules: [] }),
    );
    await screen.findByText("Loading rules…");
    await act(async () => release(new Response("", { status: 503 })));
    await screen.findByText("Could not load rules. Try again.");
    first = false;
    fireEvent.click(
      screen.getByRole("button", { name: "Retry loading rules" }),
    );
    await screen.findByText("No active rules are available");
    expect(
      screen.getByText(
        "Check the configured rule file and restart the service.",
      ),
    ).toBeTruthy();
  });
  it("aborts and clears private results on logout, including a late parsed body", async () => {
    let release!: (value: unknown) => void;
    const { cache, requests } = await start(async () => {
      const response = Response.json(rules);
      response.json = () =>
        new Promise((resolve) => {
          release = resolve;
        });
      return response;
    });
    await waitFor(() => expect(release).toBeTypeOf("function"));
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await screen.findByRole("heading", { name: "Sign in to Sentinel" });
    expect(requests[0].signal.aborted).toBe(true);
    await act(async () => release(rules));
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
    expect(screen.queryByText("Authentication brute force")).toBeNull();
  });
  it("disposes a current 401 session without retry", async () => {
    const { requests, cache } = await start(
      async () => new Response("", { status: 401 }),
    );
    await screen.findByText("Your session expired. Sign in again to continue.");
    expect(requests).toHaveLength(1);
    expect(cache.getQueryCache().getAll()).toHaveLength(0);
  });
});
