import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { APIError } from "../api/client";
import type { EventFilters, EventPage } from "../api/client";
import { useSession } from "../auth/session";

type Filters = {
  from: string;
  to: string;
  source_ip: string;
  event_type: NonNullable<EventFilters["event_type"]> | "";
  outcome: NonNullable<EventFilters["outcome"]> | "";
  limit: number;
  cursor: string;
};
const eventTypes = [
  "auth.success",
  "auth.failure",
  "net.connection",
  "net.request",
  "policy.decision",
] as const;
function readFilters(allowCursor = true): Filters {
  const q = new URLSearchParams(window.location.search);
  const cursor = allowCursor ? (q.get("cursor") ?? "") : "";
  const to = q.get("to") ?? (cursor ? "" : new Date().toISOString());
  const from =
    q.get("from") ??
    (cursor || !Number.isFinite(Date.parse(to))
      ? ""
      : new Date(Date.parse(to) - 86400000).toISOString());
  const type = q.get("event_type"),
    outcome = q.get("outcome");
  return {
    from,
    to,
    source_ip: q.get("source_ip") ?? "",
    cursor,
    limit: Number(q.get("limit") ?? 50),
    event_type: eventTypes.find((value) => value === type) ?? "",
    outcome: outcome === "success" || outcome === "failure" ? outcome : "",
  };
}
function queryFilters(f: Filters): EventFilters {
  return {
    ...(f.from && { from: f.from }),
    ...(f.to && { to: f.to }),
    limit: f.limit,
    ...(f.source_ip && { source_ip: f.source_ip }),
    ...(f.event_type && { event_type: f.event_type }),
    ...(f.outcome && { outcome: f.outcome }),
    ...(f.cursor && { cursor: f.cursor }),
  };
}
function writeLocation(f: Filters, owner: string, replace = false) {
  const url = new URL(window.location.href);
  url.search = new URLSearchParams(
    Object.entries(queryFilters(f)).map(([k, v]) => [k, String(v)]),
  ).toString();
  window.history[replace ? "replaceState" : "pushState"](
    { sentinelEventsOwner: owner },
    "",
    url,
  );
}
function isValidIP(value: string) {
  if (!value) return true;
  if (value.length > 45 || value.includes("%") || /\s/.test(value))
    return false;
  if (!value.includes(":"))
    return (
      value.split(".").length === 4 &&
      value
        .split(".")
        .every((part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255)
    );
  try {
    return !new URL(`http://[${value}]/`).hostname.startsWith("[::ffff:");
  } catch {
    return false;
  }
}
function validRange(f: Filters) {
  const from = Date.parse(f.from),
    to = Date.parse(f.to);
  return (
    ((!!f.cursor && (!f.from || !f.to)) ||
      (Number.isFinite(from) &&
        Number.isFinite(to) &&
        to > from &&
        to - from <= 7 * 86400000)) &&
    Number.isInteger(f.limit) &&
    f.limit >= 1 &&
    f.limit <= 200
  );
}
function newRange(f: Filters): Filters {
  if (f.cursor && (!f.from || !f.to)) {
    const to = new Date().toISOString();
    return {
      ...f,
      cursor: "",
      to,
      from: new Date(Date.parse(to) - 86400000).toISOString(),
    };
  }
  return { ...f, cursor: "" };
}
function timeInput(value: string) {
  return Number.isFinite(Date.parse(value))
    ? new Date(value).toISOString().slice(0, 23)
    : "";
}
function utcInput(value: string) {
  const d = new Date(value + "Z");
  return value && Number.isFinite(d.getTime()) ? d.toISOString() : "";
}
function isNarrow() {
  return window.matchMedia?.("(max-width: 767px)").matches ?? false;
}
function subscribeWidth(notify: () => void) {
  const media = window.matchMedia?.("(max-width: 767px)");
  media?.addEventListener("change", notify);
  return () => media?.removeEventListener("change", notify);
}
function rowSummary(event: EventPage["items"][number]) {
  return event.principal ?? event.source;
}

export default function Events() {
  const { session, client } = useSession();
  const owner = `${session?.subject}:${session?.tenant_id}:${session?.role}:${session?.generation}`;
  const [filters, setFilters] = useState(() =>
    readFilters(
      !window.history.state?.sentinelEventsOwner ||
        window.history.state.sentinelEventsOwner === owner,
    ),
  );
  const current = useRef(filters);
  const [draft, setDraft] = useState(filters);
  const [previous, setPrevious] = useState<Filters[]>([]);
  const [validation, setValidation] = useState("");
  const narrow = useSyncExternalStore(subscribeWidth, isNarrow);
  function navigate(next: Filters, trail: Filters[]) {
    current.current = next;
    setFilters(next);
    setDraft(next);
    setPrevious(trail);
    setValidation("");
    writeLocation(next, owner);
  }
  useEffect(() => {
    writeLocation(current.current, owner, true);
    const pop = () => {
      const own =
        !window.history.state?.sentinelEventsOwner ||
        window.history.state.sentinelEventsOwner === owner;
      const next = readFilters(own);
      current.current = next;
      setFilters(next);
      setDraft(next);
      setPrevious([]);
      setValidation("");
      if (!own) writeLocation(next, owner, true);
    };
    window.addEventListener("popstate", pop);
    return () => {
      window.removeEventListener("popstate", pop);
      if (window.history.state?.sentinelEventsOwner === owner) {
        const url = new URL(window.location.href);
        url.searchParams.delete("cursor");
        window.history.replaceState({ sentinelEventsOwner: owner }, "", url);
      }
    };
  }, [owner]);
  useEffect(() => {
    if (draft.source_ip.trim() === filters.source_ip) return;
    const timer = window.setTimeout(() => {
      const next = newRange({ ...filters, source_ip: draft.source_ip.trim() });
      if (!isValidIP(next.source_ip)) {
        setValidation("Enter a valid IP address.");
        return;
      }
      if (!validRange(next)) {
        setValidation("Choose a valid UTC time range of at most 7 days.");
        return;
      }
      current.current = next;
      setFilters(next);
      setPrevious([]);
      setValidation("");
      writeLocation(next, owner);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [draft.source_ip, filters, owner]);
  const query = queryFilters(filters);
  const events = useQuery({
    queryKey: [
      "session",
      session?.subject,
      session?.tenant_id,
      session?.role,
      session?.generation,
      "events",
      query,
    ],
    queryFn: ({ signal }) => {
      if (!client) throw new Error("Sign in is required.");
      return client.events(query, signal);
    },
    enabled: !!session && validRange(filters) && isValidIP(filters.source_ip),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const page = events.data;
  return (
    <section aria-labelledby="events-title">
      <h2 id="events-title" tabIndex={-1}>
        Events
      </h2>
      <p className="page-description">
        Search your tenant’s retained event history.
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const next = newRange({
            ...draft,
            source_ip: draft.source_ip.trim(),
          });
          if (!isValidIP(next.source_ip)) {
            setValidation("Enter a valid IP address.");
            return;
          }
          if (!validRange(next)) {
            setValidation("Choose a valid UTC time range of at most 7 days.");
            return;
          }
          navigate(next, []);
        }}
      >
        <label htmlFor="event-from">From (UTC)</label>
        <input
          id="event-from"
          type="datetime-local"
          step="0.001"
          value={timeInput(draft.from)}
          onChange={(event) =>
            setDraft({ ...draft, from: utcInput(event.target.value) })
          }
        />
        <label htmlFor="event-to">To (UTC)</label>
        <input
          id="event-to"
          type="datetime-local"
          step="0.001"
          value={timeInput(draft.to)}
          onChange={(event) =>
            setDraft({ ...draft, to: utcInput(event.target.value) })
          }
        />
        <label htmlFor="event-ip">Source IP</label>
        <input
          id="event-ip"
          type="search"
          maxLength={45}
          value={draft.source_ip}
          onChange={(event) =>
            setDraft({ ...draft, source_ip: event.target.value })
          }
          aria-describedby="event-ip-help"
        />
        <p id="event-ip-help">
          IPv4 or IPv6. Search updates after you stop typing.
        </p>
        <label htmlFor="event-type">Event type</label>
        <input
          id="event-type"
          list="event-types"
          value={draft.event_type}
          onChange={(event) =>
            setDraft({
              ...draft,
              event_type: event.target.value as Filters["event_type"],
            })
          }
        />
        <datalist id="event-types">
          {eventTypes.map((type) => (
            <option key={type} value={type} />
          ))}
        </datalist>
        <label htmlFor="event-outcome">Outcome</label>
        <input
          id="event-outcome"
          list="event-outcomes"
          value={draft.outcome}
          onChange={(event) =>
            setDraft({
              ...draft,
              outcome: event.target.value as Filters["outcome"],
            })
          }
        />
        <datalist id="event-outcomes">
          <option value="success" />
          <option value="failure" />
        </datalist>
        <label htmlFor="event-limit">Events per page</label>
        <input
          id="event-limit"
          type="number"
          min={1}
          max={200}
          value={draft.limit}
          onChange={(event) =>
            setDraft({ ...draft, limit: Number(event.target.value) })
          }
        />
        <div className="sign-in-actions">
          <button type="submit">Apply filters</button>
          <button
            type="button"
            className="secondary"
            onClick={() => {
              const to = new Date().toISOString();
              navigate(
                {
                  from: new Date(Date.parse(to) - 86400000).toISOString(),
                  to,
                  source_ip: "",
                  event_type: "",
                  outcome: "",
                  limit: 50,
                  cursor: "",
                },
                [],
              );
            }}
          >
            Clear filters
          </button>
        </div>
        {validation && (
          <p role="alert" className="error-state">
            {validation}
          </p>
        )}
      </form>
      {events.isPending && <p role="status">Loading events…</p>}
      {events.isError && (
        <div className="error-state" role="alert">
          <p>
            {events.error instanceof APIError && events.error.status === 403
              ? "You do not have permission to read events."
              : "Could not load events. Try again."}
          </p>
          <button onClick={() => void events.refetch()}>
            Retry loading events
          </button>
        </div>
      )}
      {page && page.items.length === 0 && (
        <div className="empty-state">
          <h3>No events match these filters</h3>
          <p>Broaden the time range or clear filters to search again.</p>
        </div>
      )}
      {page &&
        page.items.length > 0 &&
        (narrow ? (
          <ul className="mobile-alerts" aria-label="Event results">
            {page.items.map((event) => (
              <li key={event.event_id}>
                <h3>{rowSummary(event)}</h3>
                <dl>
                  <dt>Occurred (UTC)</dt>
                  <dd>
                    <time dateTime={event.occurred_at}>
                      {event.occurred_at}
                    </time>
                  </dd>
                  <dt>Event ID</dt>
                  <dd className="identifier">{event.event_id}</dd>
                  <dt>Type</dt>
                  <dd>{event.event_type}</dd>
                  <dt>Source IP</dt>
                  <dd>{event.source_ip ?? "Absent"}</dd>
                  <dt>Outcome</dt>
                  <dd>{event.outcome ?? "Absent"}</dd>
                </dl>
              </li>
            ))}
          </ul>
        ) : (
          <div
            className="table-region"
            role="region"
            aria-label="Event results"
            tabIndex={0}
          >
            <table>
              <caption>Retained events</caption>
              <thead>
                <tr>
                  <th scope="col">Occurred (UTC)</th>
                  <th scope="col">Event ID</th>
                  <th scope="col">Type</th>
                  <th scope="col">Source IP</th>
                  <th scope="col">Outcome</th>
                  <th scope="col">Summary</th>
                </tr>
              </thead>
              <tbody>
                {page.items.map((event) => (
                  <tr key={event.event_id}>
                    <td>
                      <time dateTime={event.occurred_at}>
                        {event.occurred_at}
                      </time>
                    </td>
                    <th scope="row" className="identifier">
                      {event.event_id}
                    </th>
                    <td>{event.event_type}</td>
                    <td>{event.source_ip ?? "Absent"}</td>
                    <td>{event.outcome ?? "Absent"}</td>
                    <td>{rowSummary(event)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ))}
      {page && (
        <div className="sign-in-actions">
          <p role="status">{page.items.length} events on this page</p>
          <button
            className="secondary"
            disabled={previous.length === 0 || events.isFetching}
            onClick={() =>
              navigate(previous[previous.length - 1], previous.slice(0, -1))
            }
          >
            Previous page
          </button>
          <button
            disabled={!page.has_more || !page.next_cursor || events.isFetching}
            onClick={() => {
              if (page.next_cursor)
                navigate(
                  { ...filters, cursor: page.next_cursor },
                  [...previous, filters].slice(-20),
                );
            }}
          >
            Next page
          </button>
        </div>
      )}
    </section>
  );
}
