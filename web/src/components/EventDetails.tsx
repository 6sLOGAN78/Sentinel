import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { APIError } from "../api/client";
import { useSession } from "../auth/session";

export default function EventDetails({
  id,
  onClose,
}: {
  id: string;
  onClose: () => void;
}) {
  const { session, client } = useSession();
  const dialog = useRef<HTMLDialogElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const node = dialog.current!;
    const trigger = document.activeElement;
    node.showModal();
    closeButton.current?.focus();
    return () => {
      node.close();
      if (trigger instanceof HTMLElement && trigger.isConnected)
        trigger.focus();
      else document.getElementById("events-title")?.focus();
    };
  }, []);
  const detail = useQuery({
    queryKey: [
      "session",
      session?.subject,
      session?.tenant_id,
      session?.role,
      session?.generation,
      "event-detail",
      id,
    ],
    queryFn: ({ signal }) => {
      if (!client) throw new Error("Sign in is required.");
      return client.eventDetail(id, signal);
    },
    enabled: session !== null,
    retry: false,
    refetchOnWindowFocus: false,
  });
  return (
    <dialog
      ref={dialog}
      aria-labelledby="event-details-title"
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
      <h2 id="event-details-title">Event details</h2>
      <button ref={closeButton} className="secondary" onClick={onClose}>
        Close event details
      </button>
      {detail.isLoading && <p role="status">Loading event details…</p>}
      {detail.isError && (
        <div className="error-state">
          <p role="alert">
            {detail.error instanceof APIError && detail.error.status === 404
              ? "This event was not found for your tenant."
              : "Could not load event details. Try again."}
          </p>
          <button
            disabled={detail.isFetching}
            onClick={() => void detail.refetch()}
          >
            Retry loading event details
          </button>
        </div>
      )}
      {detail.data && (
        <pre
          style={{
            whiteSpace: "pre-wrap",
            overflowWrap: "anywhere",
            fontSize: "16px",
          }}
        >
          <code>{JSON.stringify(detail.data, null, 2)}</code>
        </pre>
      )}
    </dialog>
  );
}
