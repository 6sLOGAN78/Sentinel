import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { APIError } from "../api/client";
import type { AlertFilters } from "../api/client";
import { useSession } from "../auth/session";
import AlertDetails from "../components/AlertDetails";

function readSelected(owner?: string) {
  if (
    owner &&
    window.history.state?.sentinelAlertsOwner &&
    window.history.state.sentinelAlertsOwner !== owner
  )
    return "";
  const id = new URLSearchParams(window.location.search).get("alert_id") ?? "";
  return /^[0-9a-f]{64}$/.test(id) ? id : "";
}

function isNarrow() {
  return window.matchMedia?.("(max-width: 767px)").matches ?? false;
}

function subscribeToWidth(notify: () => void) {
  const media = window.matchMedia?.("(max-width: 767px)");
  media?.addEventListener("change", notify);
  return () => media?.removeEventListener("change", notify);
}

type Filters = {
  from: string;
  to: string;
  limit: number;
  status: NonNullable<AlertFilters["status"]> | "";
  severity: NonNullable<AlertFilters["severity"]> | "";
  rule_id: string;
  search: string;
  cursor: string;
};

function readFilters(allowCursor = true): Filters {
  const q = new URLSearchParams(window.location.search);
  const cursor = allowCursor ? (q.get("cursor") ?? "") : "";
  // A cursor-only URL delegates omitted bounds to its authenticated server
  // payload. Inventing Date.now() here would invalidate the frozen range.
  const to = q.get("to") ?? (cursor ? "" : new Date().toISOString());
  const from =
    q.get("from") ??
    (cursor || !Number.isFinite(Date.parse(to))
      ? ""
      : new Date(Date.parse(to) - 86400000).toISOString());
  const status = q.get("status");
  const severity = q.get("severity");
  return {
    from,
    to,
    limit: Number(q.get("limit") ?? 50),
    status:
      status === "open" || status === "acknowledged" || status === "resolved"
        ? status
        : "",
    severity:
      severity === "low" ||
      severity === "medium" ||
      severity === "high" ||
      severity === "critical"
        ? severity
        : "",
    rule_id: (q.get("rule_id") ?? "").trim(),
    search: (q.get("search") ?? "").trim(),
    cursor,
  };
}

function queryFilters(filters: Filters): AlertFilters {
  return {
    ...(filters.from && { from: filters.from }),
    ...(filters.to && { to: filters.to }),
    limit: filters.limit,
    ...(filters.status && { status: filters.status }),
    ...(filters.severity && { severity: filters.severity }),
    ...(filters.rule_id && { rule_id: filters.rule_id }),
    ...(filters.search && { search: filters.search }),
    ...(filters.cursor && { cursor: filters.cursor }),
  };
}

function writeLocation(
  filters: Filters,
  owner: string,
  replace = false,
  selected = readSelected(owner),
) {
  const q = new URLSearchParams();
  for (const [key, value] of Object.entries(queryFilters(filters)))
    q.set(key, String(value));
  if (selected) q.set("alert_id", selected);
  const url = new URL(window.location.href);
  url.search = q.toString();
  window.history[replace ? "replaceState" : "pushState"](
    { sentinelAlertsOwner: owner },
    "",
    url,
  );
}

function timeInput(value: string) {
  return Number.isFinite(Date.parse(value))
    ? new Date(value).toISOString().slice(0, 23)
    : "";
}

function utcInput(value: string) {
  if (!value) return "";
  const instant = new Date(value + "Z");
  return Number.isFinite(instant.getTime()) ? instant.toISOString() : "";
}

function validFilters(filters: Filters) {
  const from = Date.parse(filters.from),
    to = Date.parse(filters.to);
  return (
    ((!!filters.cursor && (!filters.from || !filters.to)) ||
      (Number.isFinite(from) &&
        Number.isFinite(to) &&
        to > from &&
        to - from <= 7 * 86400000)) &&
    Number.isInteger(filters.limit) &&
    filters.limit >= 1 &&
    filters.limit <= 200 &&
    new TextEncoder().encode(filters.search).length <= 128 &&
    !/\p{Cc}/u.test(filters.search) &&
    (!filters.rule_id || /^[A-Z][A-Z0-9_]{0,63}$/.test(filters.rule_id))
  );
}

function newFilterRange(filters: Filters): Filters {
  const to = filters.to || new Date().toISOString();
  return {
    ...filters,
    to,
    from:
      filters.from ||
      (Number.isFinite(Date.parse(to))
        ? new Date(Date.parse(to) - 86400000).toISOString()
        : ""),
    cursor: "",
  };
}

export default function Alerts() {
  const narrow = useSyncExternalStore(subscribeToWidth, isNarrow, () => false);
  const { session, client } = useSession();
  const owner = JSON.stringify([
    session?.subject,
    session?.tenant_id,
    session?.role,
    session?.generation,
  ]);
  const [filters, setFilters] = useState(() =>
    readFilters(
      !window.history.state?.sentinelAlertsOwner ||
        window.history.state.sentinelAlertsOwner === owner,
    ),
  );
  const [draft, setDraft] = useState(filters);
  const [validation, setValidation] = useState("");
  const [selected, setSelected] = useState(() => readSelected(owner));
  const selectedRef = useRef(selected);
  const [previous, setPrevious] = useState<Filters[]>([]);
  const filtersRef = useRef(filters);
  const query = queryFilters(filters);

  function navigate(next: Filters, trail: Filters[], replace = false) {
    setFilters(next);
    filtersRef.current = next;
    setDraft(next);
    setPrevious(trail);
    setValidation("");
    writeLocation(next, owner, replace);
  }

  function selectAlert(id: string) {
    const url = new URL(window.location.href);
    if (id) url.searchParams.set("alert_id", id);
    else url.searchParams.delete("alert_id");
    window.history.pushState({ sentinelAlertsOwner: owner }, "", url);
    selectedRef.current = id;
    setSelected(id);
  }

  useEffect(() => {
    writeLocation(filtersRef.current, owner, true, selectedRef.current);
    const onPopState = () => {
      const ownEntry =
        !window.history.state?.sentinelAlertsOwner ||
        window.history.state.sentinelAlertsOwner === owner;
      const next = readFilters(ownEntry);
      if (!ownEntry) writeLocation(next, owner, true);
      filtersRef.current = next;
      setFilters(next);
      setDraft(next);
      setPrevious([]);
      setValidation("");
      selectedRef.current = readSelected(owner);
      setSelected(selectedRef.current);
    };
    window.addEventListener("popstate", onPopState);
    return () => {
      window.removeEventListener("popstate", onPopState);
      // App keys this subtree by verified generation. Remove the current URL
      // cursor on logout/expiry/switch; old entries keep an owner marker so a
      // later session cannot resurrect their page position through Back.
      if (window.history.state?.sentinelAlertsOwner === owner) {
        const url = new URL(window.location.href);
        url.searchParams.delete("cursor");
        url.searchParams.delete("alert_id");
        window.history.replaceState({ sentinelAlertsOwner: owner }, "", url);
      }
    };
  }, [owner]);

  useEffect(() => {
    if (draft.search.trim() === filters.search) return;
    const timer = window.setTimeout(() => {
      const next = newFilterRange({ ...filters, search: draft.search.trim() });
      if (!validFilters(next)) {
        setValidation(
          "Enter a search of at most 128 UTF-8 bytes and no control characters.",
        );
        return;
      }
      filtersRef.current = next;
      setFilters(next);
      setPrevious([]);
      setValidation("");
      writeLocation(next, owner);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [draft.search, filters, owner]);

  const alerts = useQuery({
    queryKey: [
      "session",
      session?.subject,
      session?.tenant_id,
      session?.role,
      session?.generation,
      "alerts",
      query,
    ],
    queryFn: ({ signal }) => {
      if (!client) throw new Error("Sign in is required.");
      return client.alerts(query, signal);
    },
    enabled: session !== null && validFilters(filters),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const page = alerts.data;
  return (
    <section aria-labelledby="alerts-title">
      <h2 id="alerts-title" tabIndex={-1}>
        Alerts
      </h2>
      <p className="page-description">Investigate your tenant’s detections.</p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const next = newFilterRange({
            ...draft,
            rule_id: draft.rule_id.trim(),
            search: draft.search.trim(),
          });
          if (!validFilters(next)) {
            setValidation("Choose a valid UTC time range of at most 7 days.");
            return;
          }
          navigate(next, []);
        }}
      >
        <label htmlFor="alert-status">Status</label>
        <input
          id="alert-status"
          list="alert-status-options"
          value={draft.status}
          onChange={(event) =>
            setDraft({
              ...draft,
              status: event.target.value as Filters["status"],
            })
          }
        />
        <datalist id="alert-status-options">
          <option value="open" />
          <option value="acknowledged" />
          <option value="resolved" />
        </datalist>
        <label htmlFor="alert-severity">Severity</label>
        <input
          id="alert-severity"
          list="alert-severity-options"
          value={draft.severity}
          onChange={(event) =>
            setDraft({
              ...draft,
              severity: event.target.value as Filters["severity"],
            })
          }
        />
        <datalist id="alert-severity-options">
          <option value="low" />
          <option value="medium" />
          <option value="high" />
          <option value="critical" />
        </datalist>
        <label htmlFor="alert-rule">Rule ID</label>
        <input
          id="alert-rule"
          maxLength={64}
          value={draft.rule_id}
          onChange={(event) =>
            setDraft({ ...draft, rule_id: event.target.value })
          }
        />
        <label htmlFor="alert-from">From (UTC)</label>
        <input
          id="alert-from"
          type="datetime-local"
          step="0.001"
          value={timeInput(draft.from)}
          onChange={(event) =>
            setDraft({
              ...draft,
              from: utcInput(event.target.value),
            })
          }
        />
        <label htmlFor="alert-to">To (UTC)</label>
        <input
          id="alert-to"
          type="datetime-local"
          step="0.001"
          value={timeInput(draft.to)}
          onChange={(event) =>
            setDraft({
              ...draft,
              to: utcInput(event.target.value),
            })
          }
        />
        <label htmlFor="alert-search">Search alerts</label>
        <input
          id="alert-search"
          type="search"
          maxLength={128}
          value={draft.search}
          onChange={(event) =>
            setDraft({ ...draft, search: event.target.value })
          }
          aria-describedby="alert-search-help"
        />
        <p id="alert-search-help">
          Literal summary or entity search. Updates after 300ms.
        </p>
        <div className="sign-in-actions">
          <button type="submit">Apply filters</button>
          <button
            className="secondary"
            type="button"
            onClick={() => {
              const to = new Date().toISOString();
              navigate(
                {
                  from: new Date(Date.parse(to) - 86400000).toISOString(),
                  to,
                  limit: 50,
                  status: "",
                  severity: "",
                  rule_id: "",
                  search: "",
                  cursor: "",
                },
                [],
              );
            }}
          >
            Clear filters
          </button>
        </div>
      </form>
      {(validation || !validFilters(filters)) && (
        <p role="alert" className="error-state">
          {validation || "Choose a valid UTC time range of at most 7 days."}
        </p>
      )}
      {session?.role === "viewer" && (
        <p className="access-note">
          Read-only access. An analyst or admin can change alert status.
        </p>
      )}
      {alerts.isPending && validFilters(filters) && (
        <p role="status">Loading alerts…</p>
      )}
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
                <button
                  className="secondary"
                  onClick={() => selectAlert(alert.alert_id)}
                >
                  Open alert details
                </button>
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
                  <th scope="col">Details</th>
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
                    <td>
                      <button
                        className="secondary"
                        onClick={() => selectAlert(alert.alert_id)}
                      >
                        Open alert details
                      </button>
                    </td>
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
      <div className="sign-in-actions" role="group" aria-label="Alert pages">
        <button
          className="secondary"
          disabled={previous.length === 0 || alerts.isFetching}
          onClick={() =>
            navigate(previous[previous.length - 1], previous.slice(0, -1))
          }
        >
          Previous page
        </button>
        <button
          disabled={!page?.has_more || !page.next_cursor || alerts.isFetching}
          onClick={() => {
            if (page?.next_cursor)
              navigate(
                { ...filters, cursor: page.next_cursor },
                [...previous, filters].slice(-20),
              );
          }}
        >
          Next page
        </button>
      </div>
      {page && <p role="status">{page.items.length} alerts on this page</p>}
      {selected && (
        <AlertDetails
          key={selected}
          id={selected}
          onClose={() => selectAlert("")}
        />
      )}
    </section>
  );
}
