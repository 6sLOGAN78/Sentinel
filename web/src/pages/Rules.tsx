import { useQuery } from "@tanstack/react-query";
import { useSession } from "../auth/session";

export default function Rules() {
  const { session, client } = useSession();
  const rules = useQuery({
    queryKey: ["rules", session?.generation],
    queryFn: ({ signal }) => client!.rules(signal),
    enabled: !!session && !!client,
    retry: false,
  });
  return (
    <section aria-labelledby="rules-title">
      <h2 id="rules-title">Rules</h2>
      <p>
        Read-only startup definitions. Changes require a service restart.
        Historical alerts retain their recorded rule version.
      </p>
      {rules.isPending && <p role="status">Loading rules…</p>}
      {rules.isError && (
        <div role="alert">
          <p>Could not load rules. Try again.</p>
          <button onClick={() => void rules.refetch()}>
            Retry loading rules
          </button>
        </div>
      )}
      {rules.data &&
        (rules.data.rules.length === 0 ? (
          <div>
            <h3>No active rules are available</h3>
            <p>Check the configured rule file and restart the service.</p>
          </div>
        ) : (
          <ul className="mobile-alerts" aria-label="Startup rule definitions">
            {rules.data.rules.map((rule) => (
              <li key={rule.rule_id}>
                <article aria-labelledby={`rule-${rule.rule_id}`}>
                  <h3 id={`rule-${rule.rule_id}`}>{rule.name}</h3>
                  <p>{rule.description}</p>
                  <p>
                    <code>{rule.rule_id}</code> · Version: {rule.version} ·{" "}
                    <span>{rule.enabled ? "Enabled" : "Disabled"}</span>
                  </p>
                  <p>Severity: {rule.severity}</p>
                  <p>
                    Threshold: {rule.parameters.threshold} · Window:{" "}
                    {rule.parameters.window_ms} ms · Cooldown:{" "}
                    {rule.parameters.cooldown_ms} ms
                  </p>
                </article>
              </li>
            ))}
          </ul>
        ))}
    </section>
  );
}
