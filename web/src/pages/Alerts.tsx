import { useQuery } from "@tanstack/react-query";
import { useSyncExternalStore } from "react";
import { APIError } from "../api/client";
import { useSession } from "../auth/session";

function isNarrow() {
  return window.matchMedia?.("(max-width: 767px)").matches ?? false;
}

function subscribeToWidth(notify: () => void) {
  const media = window.matchMedia?.("(max-width: 767px)");
  media?.addEventListener("change", notify);
  return () => media?.removeEventListener("change", notify);
}

export default function Alerts() {
  const narrow = useSyncExternalStore(subscribeToWidth, isNarrow, () => false);
  const { session, client } = useSession();
  const alerts = useQuery({
    queryKey: [
      "session",
      session?.subject,
      session?.tenant_id,
      session?.role,
      session?.generation,
      "alerts",
      {},
    ],
    queryFn: ({ signal }) => {
      if (!client) throw new Error("Sign in is required.");
      return client.alerts({}, signal);
    },
    enabled: session !== null,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const page = alerts.data;
  return (
    <section aria-labelledby="alerts-title">
      <h2 id="alerts-title">Alerts</h2>
      <p className="page-description">Investigate your tenant’s detections.</p>
      {session?.role === "viewer" && (
        <p className="access-note">
          Read-only access. An analyst or admin can change alert status.
        </p>
      )}
      {alerts.isPending && <p role="status">Loading alerts…</p>}
      {page && page.items.length === 0 && (
        <div className="empty-state">
          <h3>No alerts match these filters</h3>
          <p>Broaden the time range or clear filters to search again.</p>
        </div>
      )}
      {page &&
        page.items.length > 0 &&
        (narrow ? (
          <ul className="mobile-alerts" aria-label="Tenant alerts">
            {page.items.map((alert) => (
              <li key={alert.alert_id}>
                <h3>{alert.summary}</h3>
                <dl>
                  <dt>Created</dt>
                  <dd>
                    <time dateTime={alert.created_at}>{alert.created_at}</time>
                  </dd>
                  <dt>Severity</dt>
                  <dd>
                    <span className={`badge severity-${alert.severity}`}>
                      {alert.severity}
                    </span>
                  </dd>
                  <dt>Rule</dt>
                  <dd className="identifier">{alert.rule_id}</dd>
                  <dt>Status</dt>
                  <dd>{alert.status}</dd>
                </dl>
              </li>
            ))}
          </ul>
        ) : (
          <div
            className="table-region"
            role="region"
            aria-label="Tenant alerts table"
            tabIndex={0}
          >
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
                    <td>
                      <span className={`badge severity-${alert.severity}`}>
                        {alert.severity}
                      </span>
                    </td>
                    <td className="identifier">{alert.rule_id}</td>
                    <td>{alert.summary}</td>
                    <td>{alert.status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ))}
      {alerts.isError && (
        <div className="error-state">
          <p role="alert">
            {alerts.error instanceof APIError && alerts.error.status === 403
              ? "You do not have permission to read these alerts."
              : "Could not load alerts. Try again."}
          </p>
          <button
            disabled={alerts.isFetching}
            onClick={() => void alerts.refetch()}
          >
            Retry loading alerts
          </button>
        </div>
      )}
    </section>
  );
}
