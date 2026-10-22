# 04-17 ownership amendment A1 — proposed historical scanner fingerprints

Status: pending independent review; no scanner exception applied yet.

The pinned full-history scan reports three generic-api-key matches in earlier
planning prose, not application credentials. Root inspected the exact historical
lines: 04-07 describes API-key test cases; the duplicated 04-17 lines describe
session outage/retry behavior. None contains a usable key, token or secret value.
The duplicated lines arose in the previously repaired plan serialization; history
must remain intact. The original scan failure and redacted private report remain
evidence, not a passing result.

Proposed additional ownership: `.gitleaksignore` only, beyond the original twelve
implementation files. Add only these commit/path/rule/line fingerprints, following
the existing precisely scoped historical-prose exception policy:

- `778d3dbb5b6a373604e26b0f9193c6d01008142f:.planning/phases/04-query-api-analyst-dashboard/04-07-PLAN.md:generic-api-key:58`
- `5d11ca2d72cdc303a27e75900eb4d4afd4434108:.planning/phases/04-query-api-analyst-dashboard/04-17-PLAN.md:generic-api-key:70`
- `5d11ca2d72cdc303a27e75900eb4d4afd4434108:.planning/phases/04-query-api-analyst-dashboard/04-17-PLAN.md:generic-api-key:101`

No path/rule/content-wide allowlist, scanner configuration change, history rewrite,
credential rotation or exception for implementation files is proposed. Every new
finding at another fingerprint must still fail. Independent review must classify
these exact historical lines and approve the limited amendment before application.
After approval, rerun pinned gitleaks 8.30.1 against full history, preserve actual
SHA/start/end/exit in private evidence, and require exit zero before acceptance.
