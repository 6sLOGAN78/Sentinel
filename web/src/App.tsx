import { useState, useSyncExternalStore } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SessionProvider, useSession } from "./auth/session";
import Alerts from "./pages/Alerts";
import Events from "./pages/Events";
import Rules from "./pages/Rules";
import Overview from "./pages/Overview";
import "./styles.css";

function SignInPanel() {
  const { pending, canRetry, signInRevision, signIn, retrySignIn, signOut } =
    useSession();
  const [draft, setDraft] = useState({ revision: signInRevision, value: "" });
  // Reset before committing stale input, preserving the field and its focus.
  if (draft.revision !== signInRevision)
    setDraft({ revision: signInRevision, value: "" });
  const suppliedToken = draft.value;
  const setSuppliedToken = (value: string) =>
    setDraft({ revision: signInRevision, value });
  return (
    <section className="sign-in-panel" aria-labelledby="sign-in-title">
      <h2 id="sign-in-title">Sign in to Sentinel</h2>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const credential = suppliedToken;
          setSuppliedToken("");
          void signIn(credential);
        }}
      >
        <label htmlFor="development-token">Development access token</label>
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
          Generate a short-lived token with the local development CLI. Tokens
          stay in memory.
        </p>
        <button type="submit" disabled={pending}>
          Sign in
        </button>
      </form>
      <div className="sign-in-actions">
        {canRetry && (
          <button disabled={pending} onClick={() => void retrySignIn()}>
            Retry sign in
          </button>
        )}
        {(pending || canRetry) && (
          <button className="secondary" onClick={() => signOut()}>
            Cancel sign in
          </button>
        )}
      </div>
      {pending && <p role="status">Verifying access token…</p>}
    </section>
  );
}

function subscribeNavigation(notify: () => void) {
  window.addEventListener("popstate", notify);
  window.addEventListener("sentinel:navigate", notify);
  return () => {
    window.removeEventListener("popstate", notify);
    window.removeEventListener("sentinel:navigate", notify);
  };
}

function AnalystShell() {
  const { session, error, signOut } = useSession();
  const path = useSyncExternalStore(
    subscribeNavigation,
    () => window.location.pathname,
  );
  return (
    <div className={session ? "app-shell authenticated" : "app-shell"}>
      <a className="skip-link" href="#main-content">
        Skip to content
      </a>
      <header className="app-header">
        <h1>Sentinel</h1>
        {session && (
          <div className="session-identity">
            <p>
              {session.tenant_id} · {session.role}
            </p>
            <button className="secondary" onClick={() => signOut()}>
              Sign out
            </button>
          </div>
        )}
      </header>
      {session && (
        <nav className="app-nav" aria-label="Main navigation">
          {["overview", "alerts", "events", "rules"].map((page) => (
            <a
              key={page}
              href={page === "overview" ? "/" : `/${page}`}
              style={
                (
                  path === "/events" || path === "/rules" || path === "/alerts"
                    ? path === `/${page}`
                    : page === "overview"
                )
                  ? undefined
                  : {
                      background: "transparent",
                      borderLeftColor: "transparent",
                    }
              }
              aria-current={
                (
                  path === "/events" || path === "/rules" || path === "/alerts"
                    ? path === `/${page}`
                    : page === "overview"
                )
                  ? "page"
                  : undefined
              }
              onClick={(event) => {
                if (
                  event.button !== 0 ||
                  event.metaKey ||
                  event.ctrlKey ||
                  event.shiftKey ||
                  event.altKey
                )
                  return;
                event.preventDefault();
                const target = page === "overview" ? "/" : `/${page}`;
                if (window.location.pathname !== target) {
                  window.history.pushState(null, "", target);
                  window.dispatchEvent(new Event("sentinel:navigate"));
                }
                document.getElementById("main-content")?.focus();
              }}
            >
              {page === "overview"
                ? "Overview"
                : page === "alerts"
                  ? "Alerts"
                  : page === "events"
                    ? "Events"
                    : "Rules"}
            </a>
          ))}
        </nav>
      )}
      <main id="main-content" tabIndex={-1}>
        {!session ? (
          <SignInPanel />
        ) : path === "/events" ? (
          <Events key={session.generation} />
        ) : path === "/rules" ? (
          <Rules key={session.generation} />
        ) : path === "/alerts" ? (
          <Alerts key={session.generation} />
        ) : (
          <Overview key={session.generation} />
        )}
        {error && (
          <p className="error-state" role="alert">
            {error}
          </p>
        )}
      </main>
    </div>
  );
}

export default function App({ queryClient }: { queryClient?: QueryClient }) {
  const [ownedClient] = useState(() => queryClient ?? new QueryClient());
  return (
    <QueryClientProvider client={ownedClient}>
      <SessionProvider>
        <AnalystShell />
      </SessionProvider>
    </QueryClientProvider>
  );
}
