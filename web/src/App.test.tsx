import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";

describe("App shell", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
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
    vi.stubGlobal("fetch", fetchSpy);
    render(<App />);
    const input = screen.getByLabelText(
      "Development access token",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "operator-token" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/v1/session");
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
    expect(fetchSpy.mock.calls[1][0]).toBe("/api/v1/alerts");
    for (const [, init] of fetchSpy.mock.calls) {
      expect(init.headers.Authorization).toBe("Bearer operator-token");
      expect(init.signal).toBeInstanceOf(AbortSignal);
    }
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    expect(document.body.textContent).not.toContain("operator-token");
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(screen.queryByText("Real workflow alert")).toBeNull();
    expect(screen.getByLabelText("Development access token")).toBeTruthy();
    expect(fetchSpy.mock.calls[1][1].signal.aborted).toBe(true);
  });

  it("rejects an unverified identity and never loads alerts", async () => {
    const fetchSpy = vi
      .fn()
      .mockResolvedValue(new Response("{}", { status: 401 }));
    vi.stubGlobal("fetch", fetchSpy);
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
      vi.stubGlobal("fetch", fetchSpy);
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
      expect(fetchSpy.mock.calls[1][1].headers.Authorization).toBe(
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
    vi.stubGlobal("fetch", fetchSpy);
    render(<App />);
    fireEvent.change(screen.getByLabelText("Development access token"), {
      target: { value: "old-credential" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel sign in" }));
    expect(fetchSpy.mock.calls[0][1].signal.aborted).toBe(true);
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
    vi.stubGlobal(
      "fetch",
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
    vi.stubGlobal(
      "fetch",
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
    vi.stubGlobal("fetch", fetchSpy);
    render(<App />);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(screen.queryAllByRole("row")).toHaveLength(0);
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
    vi.unstubAllGlobals();
  });
});
