import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIError } from "../api/client";
import { useSession } from "../auth/session";

function ResolveConfirmation({
  pending,
  onReturn,
  onResolve,
}: {
  pending: boolean;
  onReturn: () => void;
  onResolve: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const cancelButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const node = dialog.current!;
    const trigger = document.activeElement;
    node.showModal();
    cancelButton.current?.focus();
    return () => {
      node.close();
      if (trigger instanceof HTMLElement && trigger.isConnected)
        trigger.focus();
    };
  }, []);
  return (
    <dialog
      ref={dialog}
      aria-labelledby="resolve-alert-title"
      aria-describedby="resolve-alert-description"
      onCancel={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (!pending) onReturn();
      }}
      style={{
        width: "min(calc(100% - 32px), 28rem)",
        padding: "24px",
        border: "1px solid var(--border)",
        color: "var(--text)",
        background: "var(--surface)",
      }}
    >
      <h2 id="resolve-alert-title">Resolve alert?</h2>
      <p id="resolve-alert-description">
        This closes the alert. Reopening is not supported in v1.
      </p>
      <button
        ref={cancelButton}
        className="secondary"
        disabled={pending}
        onClick={onReturn}
      >
        Return to alert
      </button>{" "}
      <button disabled={pending} onClick={onResolve}>
        Resolve alert
      </button>
      {pending && <p role="status">Resolving alert…</p>}
    </dialog>
  );
}

export default function AlertDetails({
  id,
  onClose,
}: {
  id: string;
  onClose: () => void;
}) {
  const { session, client } = useSession();
  const dialog = useRef<HTMLDialogElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const mutation = useRef<AbortController | null>(null);
  const cache = useQueryClient();
  const [pending, setPending] = useState(false);
  const [confirmResolve, setConfirmResolve] = useState(false);
  const [message, setMessage] = useState("");
  useEffect(() => {
    const node = dialog.current!;
    const trigger = document.activeElement;
    node.showModal();
    closeButton.current?.focus();
    return () => {
      mutation.current?.abort();
      node.close();
      if (trigger instanceof HTMLElement && trigger.isConnected)
        trigger.focus();
      else document.getElementById("alerts-title")?.focus();
    };
  }, []);
  const detail = useQuery({
    queryKey: [
      "session",
      session?.subject,
      session?.tenant_id,
      session?.role,
      session?.generation,
      "alert-detail",
      id,
    ],
    queryFn: ({ signal }) => {
      if (!client) throw new Error("Sign in is required.");
      return client.alertDetail(id, signal);
    },
    enabled: session !== null,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const data = detail.data;
  async function transition(status: "acknowledged" | "resolved") {
    if (!client || !data || pending || mutation.current) return;
    const operation = new AbortController();
    mutation.current = operation;
    setPending(true);
    setMessage("");
    try {
      const workflow = await client.transitionAlert(
        id,
        {
          status,
          version: data.workflow.version,
        },
        operation.signal,
      );
      // Retain committed PG state if the independent detail refresh fails.
      cache.setQueryData(
        [
          "session",
          session?.subject,
          session?.tenant_id,
          session?.role,
          session?.generation,
          "alert-detail",
          id,
        ],
        { ...data, workflow },
      );
      setConfirmResolve(false);
      setMessage(
        status === "resolved" ? "Alert resolved." : "Alert acknowledged.",
      );
      await Promise.all([
        detail.refetch(),
        cache.invalidateQueries({
          predicate: (query) =>
            query.queryKey[0] === "session" &&
            query.queryKey.includes("alerts"),
        }),
      ]);
    } catch (failure) {
      if (operation.signal.aborted) return;
      setConfirmResolve(false);
      if (failure instanceof APIError && failure.status === 409) {
        setMessage(
          "This alert changed. Review the latest details before acting again.",
        );
        await detail.refetch();
      } else {
        setMessage(
          status === "resolved"
            ? "Could not resolve this alert. Refresh details before trying again."
            : "Could not acknowledge this alert. Refresh details before trying again.",
        );
      }
    } finally {
      if (!operation.signal.aborted) {
        mutation.current = null;
        setPending(false);
      }
    }
  }
  const jsonStyle = {
    whiteSpace: "pre-wrap",
    overflowWrap: "anywhere",
    fontSize: "16px",
  } as const;
  return (
    <dialog
      ref={dialog}
      aria-labelledby="alert-details-title"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      style={{
        width: "min(100%, 48rem)",
        maxWidth: "100vw",
        height: "100dvh",
        maxHeight: "100dvh",
        margin: "0 0 0 auto",
        padding: "24px",
        border: "1px solid var(--border)",
        color: "var(--text)",
        background: "var(--surface)",
        overflowWrap: "anywhere",
      }}
    >
      <h2 id="alert-details-title">Alert details</h2>
      <button ref={closeButton} className="secondary" onClick={onClose}>
        Close alert details
      </button>
      {detail.isPending && <p role="status">Loading alert details…</p>}
      {detail.isError && (
        <div className="error-state">
          <p role="alert">
            {detail.error instanceof APIError && detail.error.status === 404
              ? "This alert was not found for your tenant."
              : "Could not load alert details. Try again."}
          </p>
          <button
            disabled={detail.isFetching}
            onClick={() => void detail.refetch()}
          >
            Retry loading alert details
          </button>
        </div>
      )}
      {data && (
        <>
          <h3 style={{ marginTop: "24px" }}>Workflow</h3>
          <p>
            Status: {data.workflow.status} · Version: {data.workflow.version}
          </p>
          {(session?.role === "analyst" || session?.role === "admin") &&
            data.workflow.status === "open" && (
              <button
                disabled={pending || detail.isFetching || detail.isError}
                onClick={() => void transition("acknowledged")}
              >
                Acknowledge alert
              </button>
            )}
          {(session?.role === "analyst" || session?.role === "admin") &&
            data.workflow.status === "acknowledged" && (
              <button
                disabled={pending || detail.isFetching || detail.isError}
                onClick={() => setConfirmResolve(true)}
              >
                Resolve alert
              </button>
            )}
          {confirmResolve && (
            <ResolveConfirmation
              pending={pending}
              onReturn={() => setConfirmResolve(false)}
              onResolve={() => void transition("resolved")}
            />
          )}
          {pending && !confirmResolve && (
            <p role="status">Acknowledging alert…</p>
          )}
          {message && <p role="status">{message}</p>}
          <pre style={jsonStyle}>{JSON.stringify(data.workflow, null, 2)}</pre>
          <h3>Immutable detection</h3>
          <pre style={jsonStyle}>{JSON.stringify(data.detection, null, 2)}</pre>
          <h3>Recorded rule explanation</h3>
          <p>{data.rule_explanation}</p>
          <h3>Evidence</h3>
          {data.missing_evidence_ids.length > 0 && (
            <>
              <p role="status">Some evidence is no longer retained.</p>
              <p>Missing sample event IDs:</p>
              <ul>
                {data.missing_evidence_ids.map((missing) => (
                  <li key={missing} className="identifier">
                    {missing}
                  </li>
                ))}
              </ul>
            </>
          )}
          {data.evidence.length === 0 ? (
            <p>No retained sample events.</p>
          ) : (
            data.evidence.map((event) => (
              <pre key={event.event_id} style={jsonStyle}>
                {JSON.stringify(event, null, 2)}
              </pre>
            ))
          )}
          <h3>Status history</h3>
          {data.history.length === 0 ? (
            <p>No status changes recorded.</p>
          ) : (
            <ul>
              {data.history.map((change) => (
                <li key={change.to_status}>
                  {change.from_status} → {change.to_status} · {change.actor} ·{" "}
                  {change.changed_at}
                </li>
              ))}
            </ul>
          )}
          <button
            className="secondary"
            disabled={detail.isFetching || pending}
            onClick={() => void detail.refetch()}
          >
            Refresh alert details
          </button>
        </>
      )}
    </dialog>
  );
}
