import { useEffect, useRef, useState } from "react";
import type { paths } from "./api/schema";

type Session =
  paths["/api/v1/session"]["get"]["responses"][200]["content"]["application/json"];
type AlertPage =
  paths["/api/v1/alerts"]["get"]["responses"][200]["content"]["application/json"];

const invalidToken =
  "This access token could not be verified. Check it or generate a new token.";
const expired = "Your session expired. Sign in again to continue.";
const unavailable = "Could not sign in. The service is unavailable. Try again.";

function retryExpiry(credential: string) {
  const maximum = Date.now() + 15 * 60 * 1000;
  try {
    const payload = credential
      .split(".")[1]
      .replace(/-/g, "+")
      .replace(/_/g, "/");
    const claims: unknown = JSON.parse(atob(payload));
    if (
      typeof claims === "object" &&
      claims !== null &&
      "exp" in claims &&
      typeof claims.exp === "number" &&
      Number.isFinite(claims.exp)
    ) {
      // An unverified expiry can only shorten credential retention. Identity
      // and authority still come exclusively from the verified server session.
      return Math.min(maximum, claims.exp * 1000);
    }
  } catch {
    // An opaque credential still has a finite in-memory retry lifetime.
  }
  return maximum;
}

function isSession(value: unknown): value is Session {
  return (
    typeof value === "object" &&
    value !== null &&
    "subject" in value &&
    typeof value.subject === "string" &&
    value.subject.trim() !== "" &&
    "tenant_id" in value &&
    typeof value.tenant_id === "string" &&
    value.tenant_id !== "" &&
    "role" in value &&
    (value.role === "viewer" ||
      value.role === "analyst" ||
      value.role === "admin") &&
    "expires_at" in value &&
    typeof value.expires_at === "string" &&
    Number.isFinite(Date.parse(value.expires_at))
  );
}

export default function App() {
  const [suppliedToken, setSuppliedToken] = useState("");
  const [session, setSession] = useState<Session | null>(null);
  const [page, setPage] = useState<AlertPage | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [canRetry, setCanRetry] = useState(false);
  const [retryUntil, setRetryUntil] = useState(0);
  const token = useRef("");
  const retryCredential = useRef("");
  const active = useRef<AbortController | null>(null);

  useEffect(
    () => () => {
      active.current?.abort();
      token.current = "";
      retryCredential.current = "";
    },
    [],
  );

  function signOut(message = "") {
    active.current?.abort();
    active.current = null;
    token.current = "";
    retryCredential.current = "";
    setCanRetry(false);
    setRetryUntil(0);
    setSuppliedToken("");
    setSession(null);
    setPage(null);
    setPending(false);
    setError(message);
  }

  useEffect(() => {
    const deadline = session ? Date.parse(session.expires_at) : retryUntil;
    if (!deadline) return;
    const timer = window.setTimeout(
      () => signOut(expired),
      Math.max(0, deadline - Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [session, retryUntil]);

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
    const credential = suppliedToken.trim();
    setSuppliedToken("");
    await verifySession(credential, retryExpiry(credential));
  }

  async function verifySession(credential: string, deadline: number) {
    active.current?.abort();
    setSuppliedToken("");
    if (!credential || deadline <= Date.now()) {
      signOut(expired);
      return;
    }
    const controller = new AbortController();
    active.current = controller;
    retryCredential.current = credential;
    setRetryUntil(deadline);
    setCanRetry(false);
    setPending(true);
    setError("");
    try {
      const response = await fetch("/api/v1/session", {
        headers: { Authorization: `Bearer ${credential}` },
        signal: controller.signal,
        cache: "no-store",
      });
      if (controller.signal.aborted) return;
      if (response.status === 401) {
        signOut(invalidToken);
        return;
      }
      if (!response.ok) throw new Error("availability");
      const verified: unknown = await response.json();
      if (controller.signal.aborted) return;
      if (!isSession(verified)) throw new Error("availability");
      if (Date.parse(verified.expires_at) <= Date.now()) {
        signOut(expired);
        return;
      }
      retryCredential.current = "";
      setRetryUntil(0);
      token.current = credential;
      setSession(verified);
      await loadAlerts(controller);
    } catch {
      if (!controller.signal.aborted) {
        setCanRetry(true);
        setError(unavailable);
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
            {canRetry && (
              <button
                disabled={pending}
                onClick={() =>
                  void verifySession(retryCredential.current, retryUntil)
                }
              >
                Retry sign in
              </button>
            )}
            {(pending || canRetry) && (
              <button onClick={() => signOut()}>Cancel sign in</button>
            )}
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
