import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SessionProvider, useSession } from "./session";
import { createAPIClient } from "../api/client";

function verified(tenant = "verified-tenant", lifetime = 600000) {
  return {
    subject: "verified-subject",
    tenant_id: tenant,
    role: "analyst",
    expires_at: new Date(Date.now() + lifetime).toISOString(),
  };
}

function Probe() {
  const { session, client, pending, error, signIn, signOut } = useSession();
  return (
    <>
      <p>
        {session
          ? `${session.subject} / ${session.tenant_id} / ${session.role}`
          : "No authority"}
      </p>
      {pending && <p role="status">Verifying</p>}
      {error && <p role="alert">{error}</p>}
      <button onClick={() => void signIn("first-credential")}>
        First identity
      </button>
      <button onClick={() => void signIn("second-credential")}>
        Second identity
      </button>
      <button onClick={() => signOut()}>Sign out</button>
      <button
        onClick={() => {
          void client?.alerts().catch(() => {});
          void client?.alerts().catch(() => {});
        }}
      >
        Start two reads
      </button>
    </>
  );
}

function mount() {
  const client = new QueryClient();
  const view = render(
    <QueryClientProvider client={client}>
      <SessionProvider>
        <Probe />
      </SessionProvider>
    </QueryClientProvider>,
  );
  return { client, ...view };
}

describe("session authority", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("uses only the verified server principal, never unverified credential claims", async () => {
    let finish: (response: Response) => void = () => {};
    vi.stubGlobal(
      "fetch",
      vi.fn().mockReturnValue(
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
      ),
    );
    mount();
    fireEvent.click(screen.getByRole("button", { name: "First identity" }));
    expect(screen.getByText("No authority")).toBeTruthy();
    await act(async () => finish(Response.json(verified())));
    expect(
      screen.getByText("verified-subject / verified-tenant / analyst"),
    ).toBeTruthy();
    expect(document.body.innerHTML).not.toContain("first-credential");
    expect(localStorage.length + sessionStorage.length).toBe(0);
    expect(window.location.href).not.toContain("credential");
  });

  it("aborts every concurrent read on switch and never accepts an old parsed identity", async () => {
    let finish: (value: unknown) => void = () => {};
    const body = new Promise((resolve) => {
      finish = resolve;
    });
    const oldResponse = Response.json(verified("old-tenant"));
    oldResponse.json = () => body;
    oldResponse.text = async () => JSON.stringify(await body);
    const fetchSpy = vi
      .fn()
      .mockResolvedValueOnce(oldResponse)
      .mockResolvedValueOnce(Response.json(verified("new-tenant")))
      .mockImplementation(() => new Promise<Response>(() => {}));
    vi.stubGlobal("fetch", fetchSpy);
    const { unmount } = mount();
    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: "First identity" })),
    );
    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: "Second identity" })),
    );
    await act(async () => finish(verified("old-tenant")));
    expect(screen.queryByText(/old-tenant/)).toBeNull();
    expect(screen.getByText(/new-tenant/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Start two reads" }));
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(4));
    const requests = fetchSpy.mock.calls.map(([request]) => request as Request);
    expect(requests[2].signal.aborted).toBe(false);
    expect(requests[3].signal.aborted).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "First identity" }));
    expect(requests[2].signal.aborted).toBe(true);
    expect(requests[3].signal.aborted).toBe(true);
    expect(screen.getByText("No authority")).toBeTruthy();
    const pending = fetchSpy.mock.calls[4][0] as Request;
    unmount();
    expect(pending.signal.aborted).toBe(true);
  });

  it("clears authenticated authority on expiry and starts a remount signed out", async () => {
    vi.useFakeTimers();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json(verified("expiring", 1000))),
    );
    const view = mount();
    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: "First identity" })),
    );
    expect(screen.getByText(/verified-subject/)).toBeTruthy();
    await act(async () => vi.advanceTimersByTime(1000));
    expect(screen.getByText("No authority")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toBe(
      "Your session expired. Sign in again to continue.",
    );
    view.unmount();
    mount();
    expect(screen.getByText("No authority")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("rejects an aborted individual query even when response body parsing ignores cancellation", async () => {
    let finish: (value: string) => void = () => {};
    const body = new Promise<string>((resolve) => {
      finish = resolve;
    });
    const response = new Response();
    response.text = () => body;
    const fetchSpy = vi.fn().mockResolvedValue(response);
    vi.stubGlobal("fetch", fetchSpy);
    const lifetime = new AbortController();
    const query = new AbortController();
    const client = createAPIClient(
      () => "header-only-credential",
      lifetime.signal,
      () => true,
      () => {},
    );
    const read = client.alerts({}, query.signal);
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    query.abort();
    finish(JSON.stringify({ items: [], next_cursor: null, has_more: false }));
    await expect(read).rejects.toHaveProperty("name", "AbortError");
    expect(lifetime.signal.aborted).toBe(false);
  });
});
