import { useEffect, useRef, useState } from "react";
import type { paths } from "./api/schema";

type Session =
  paths["/api/v1/session"]["get"]["responses"][200]["content"]["application/json"];
type AlertPage =
  paths["/api/v1/alerts"]["get"]["responses"][200]["content"]["application/json"];

const invalidToken =
  "This access token could not be verified. Check it or generate a new token.";
const expired = "Your session expired. Sign in again to continue.";

export default function App() {
  const [suppliedToken, setSuppliedToken] = useState("");
  const [session, setSession] = useState<Session | null>(null);
  const [page, setPage] = useState<AlertPage | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const token = useRef("");
  const active = useRef<AbortController | null>(null);

  useEffect(() => () => active.current?.abort(), []);

  function signOut(message = "") {
    active.current?.abort();
    token.current = "";
    setSuppliedToken("");
    setSession(null);
    setPage(null);
    setPending(false);
    setError(message);
  }

  useEffect(() => {
    if (!session) return;
    const timer = window.setTimeout(
      () => signOut(expired),
      Math.max(0, Date.parse(session.expires_at) - Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [session]);

  async function loadAlerts(controller: AbortController) {
    setPending(true);
    setError("");
    try {
      const response = await fetch("/api/v1/alerts", {
        headers: { Authorization: `Bearer ${token.current}` },
        signal: controller.signal,
        cache: "no-store",
      });
      if (controller.signal.aborted) return;
      if (response.status === 401) {
        signOut(expired);
        return;
      }
      if (!response.ok) throw new Error("read");
      const result: AlertPage = await response.json();
      if (!controller.signal.aborted) setPage(result);
    } catch {
      if (!controller.signal.aborted)
        setError("Could not load alerts. Try again.");
    } finally {
      if (!controller.signal.aborted) setPending(false);
    }
  }

  async function signIn(event: React.SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    active.current?.abort();
    const controller = new AbortController();
    active.current = controller;
    setPending(true);
    setError("");
    const credential = suppliedToken.trim();
    setSuppliedToken("");
    try {
      const response = await fetch("/api/v1/session", {
        headers: { Authorization: `Bearer ${credential}` },
        signal: controller.signal,
        cache: "no-store",
      });
      if (!response.ok) throw new Error("identity");
      const verified: Session = await response.json();
      if (controller.signal.aborted) return;
      token.current = credential;
      setSession(verified);
      await loadAlerts(controller);
    } catch {
      if (!controller.signal.aborted) {
        token.current = "";
        setError(invalidToken);
        setPending(false);
      }
    }
  }

  return (
    <>
      <header>
        <h1>Sentinel</h1>
        {session && (
          <>
            <p>
              {session.tenant_id} · {session.role}
            </p>
            <button onClick={() => signOut()}>Sign out</button>
          </>
        )}
      </header>
      <main>
        {!session ? (
          <section aria-labelledby="sign-in-title">
            <h2 id="sign-in-title">Sign in to Sentinel</h2>
            <form onSubmit={signIn}>
              <label htmlFor="development-token">
                Development access token
              </label>
              <input
                id="development-token"
                type="password"
                autoComplete="off"
                spellCheck={false}
                maxLength={8192}
                aria-describedby="token-help"
                value={suppliedToken}
                onChange={(event) => setSuppliedToken(event.target.value)}
                required
              />
              <p id="token-help">
                Generate a short-lived token with the local development CLI.
                Tokens stay in memory.
              </p>
              <button type="submit" disabled={pending}>
                Sign in
              </button>
            </form>
          </section>
        ) : (
          <section aria-labelledby="alerts-title">
            <h2 id="alerts-title">Alerts</h2>
            {pending && <p role="status">Loading alerts…</p>}
            {page && page.items.length === 0 && (
              <>
                <p>No alerts match these filters</p>
                <p>Broaden the time range or clear filters to search again.</p>
              </>
            )}
            {page && page.items.length > 0 && (
              <table>
                <caption>Tenant alerts</caption>
                <thead>
                  <tr>
                    <th scope="col">Created</th>
                    <th scope="col">Severity</th>
                    <th scope="col">Rule</th>
                    <th scope="col">Summary</th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {page.items.map((alert) => (
                    <tr key={alert.alert_id}>
                      <td>
                        <time dateTime={alert.created_at}>
                          {alert.created_at}
                        </time>
                      </td>
                      <td>{alert.severity}</td>
                      <td>{alert.rule_id}</td>
                      <td>{alert.summary}</td>
                      <td>{alert.status}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {error && (
              <button
                disabled={pending}
                onClick={() => {
                  if (active.current) void loadAlerts(active.current);
                }}
              >
                Retry loading alerts
              </button>
            )}
          </section>
        )}
        {error && <p role="alert">{error}</p>}
      </main>
    </>
  );
}
