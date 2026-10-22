import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { APIError, createAPIClient } from "../api/client";
import type { APIClient, Session } from "../api/client";

const invalidToken =
  "This access token could not be verified. Check it or generate a new token.";
const expired = "Your session expired. Sign in again to continue.";
const unavailable = "Could not sign in. The service is unavailable. Try again.";

export type VerifiedSession = Session & { generation: number };
type Authority = { session: VerifiedSession; client: APIClient };
type SessionContextValue = {
  session: VerifiedSession | null;
  client: APIClient | null;
  pending: boolean;
  error: string;
  canRetry: boolean;
  signInRevision: number;
  signIn: (credential: string) => Promise<void>;
  retrySignIn: () => Promise<void>;
  signOut: (message?: string) => void;
};
const SessionContext = createContext<SessionContextValue | null>(null);

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
      // Unverified expiry only shortens retry retention. The server supplies
      // verified identity and the separate authenticated session lifetime.
      return Math.min(maximum, claims.exp * 1000);
    }
  } catch {
    /* Opaque credentials still have a finite retry ceiling. */
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

export function SessionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [authority, setAuthority] = useState<Authority | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [canRetry, setCanRetry] = useState(false);
  const [signInRevision, setSignInRevision] = useState(0);
  const [deadline, setDeadline] = useState(0);
  const generation = useRef(0);
  const active = useRef<{
    controller: AbortController;
    credential: string;
    deadline: number;
  } | null>(null);

  const dispose = useCallback(() => {
    // Revoke authority before abort/cancellation callbacks can settle.
    generation.current++;
    if (active.current) {
      active.current.credential = "";
      active.current.controller.abort();
      active.current = null;
    }
    void queryClient.cancelQueries();
    queryClient.clear();
  }, [queryClient]);

  const signOut = useCallback(
    (message = "") => {
      dispose();
      setSignInRevision((revision) => revision + 1);
      setAuthority(null);
      setPending(false);
      setCanRetry(false);
      setDeadline(0);
      setError(message);
    },
    [dispose],
  );

  useEffect(() => dispose, [dispose]);
  useEffect(() => {
    if (!deadline) return;
    const timer = window.setTimeout(
      () => signOut(expired),
      Math.max(0, deadline - Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [deadline, signOut]);

  const verify = useCallback(
    async (credential: string, retryUntil: number) => {
      dispose();
      setSignInRevision((revision) => revision + 1);
      setAuthority(null);
      if (!credential || retryUntil <= Date.now()) {
        signOut(expired);
        return;
      }
      const controller = new AbortController();
      const attempt = generation.current;
      active.current = { controller, credential, deadline: retryUntil };
      setDeadline(retryUntil);
      setCanRetry(false);
      setPending(true);
      setError("");
      const isCurrent = () =>
        generation.current === attempt && !controller.signal.aborted;
      let authenticated = false;
      const client = createAPIClient(
        () => active.current?.credential ?? "",
        controller.signal,
        () => isCurrent() && Date.now() < (active.current?.deadline ?? 0),
        () => {
          if (authenticated && isCurrent()) signOut(expired);
        },
      );
      try {
        const verified: unknown = await client.session();
        if (!isCurrent()) return;
        if (!isSession(verified)) throw new APIError(503);
        const expiresAt = Date.parse(verified.expires_at);
        if (expiresAt <= Date.now()) {
          signOut(expired);
          return;
        }
        authenticated = true;
        active.current!.deadline = expiresAt;
        setDeadline(expiresAt);
        setAuthority({ session: { ...verified, generation: attempt }, client });
      } catch (failure) {
        if (!isCurrent()) return;
        if (failure instanceof APIError && failure.status === 401) {
          signOut(invalidToken);
          return;
        }
        setCanRetry(true);
        setError(unavailable);
      } finally {
        if (isCurrent()) setPending(false);
      }
    },
    [dispose, signOut],
  );

  return (
    <SessionContext.Provider
      value={{
        session: authority?.session ?? null,
        client: authority?.client ?? null,
        pending,
        error,
        canRetry,
        signInRevision,
        signIn: (credential) => {
          const raw = credential.trim();
          return verify(raw, retryExpiry(raw));
        },
        retrySignIn: () =>
          verify(
            active.current?.credential ?? "",
            active.current?.deadline ?? 0,
          ),
        signOut,
      }}
    >
      {children}
    </SessionContext.Provider>
  );
}

export function useSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error("SessionProvider is required.");
  return session;
}
