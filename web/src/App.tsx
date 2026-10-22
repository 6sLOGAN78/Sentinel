import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SessionProvider, useSession } from "./auth/session";
import Alerts from "./pages/Alerts";
import "./styles.css";

function AnalystShell() {
  const { session, pending, error, canRetry, signIn, retrySignIn, signOut } =
    useSession();
  const [suppliedToken, setSuppliedToken] = useState("");
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
          <a
            href="/alerts"
            aria-current="page"
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
              if (window.location.pathname !== "/alerts")
                window.history.pushState(null, "", "/alerts");
              document.getElementById("main-content")?.focus();
            }}
          >
            Alerts
          </a>
        </nav>
      )}
      <main id="main-content" tabIndex={-1}>
        {!session ? (
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
            <div className="sign-in-actions">
              {canRetry && (
                <button disabled={pending} onClick={() => void retrySignIn()}>
                  Retry sign in
                </button>
              )}
              {(pending || canRetry) && (
                <button
                  className="secondary"
                  onClick={() => {
                    setSuppliedToken("");
                    signOut();
                  }}
                >
                  Cancel sign in
                </button>
              )}
            </div>
            {pending && <p role="status">Verifying access token…</p>}
          </section>
        ) : (
          <Alerts key={session.generation} />
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
