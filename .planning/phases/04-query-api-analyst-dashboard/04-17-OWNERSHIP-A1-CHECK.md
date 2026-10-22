# 04-17 ownership amendment A1 — independent bounded check

**Decision: APPROVE.** Reviewed at c3b060bbcfdcd364f353e2d95f10208e318c925e, source candidate a1fbd4f7b8393616bb199853734156f2b9dd91cb, on 2026-10-22.

Approval adds .gitleaksignore ownership only for these three exact historical fingerprints:

- `778d3dbb5b6a373604e26b0f9193c6d01008142f:.planning/phases/04-query-api-analyst-dashboard/04-07-PLAN.md:generic-api-key:58`
- `5d11ca2d72cdc303a27e75900eb4d4afd4434108:.planning/phases/04-query-api-analyst-dashboard/04-17-PLAN.md:generic-api-key:70`
- `5d11ca2d72cdc303a27e75900eb4d4afd4434108:.planning/phases/04-query-api-analyst-dashboard/04-17-PLAN.md:generic-api-key:101`

The reviewer read the proposal, existing .gitleaksignore, all three exact historical lines using git show, and the redacted final report's rule/path/line/commit/fingerprint fields. The sets match exactly. The first line describes future event/API-key negative test cases; the other two duplicate session availability/retry policy. None contains a usable secret, JWT or API-key value. These are policy/test prose false positives, consistent with the existing precisely scoped historical-prose exception.

No path-wide, rule-wide or content-wide allowlist, scanner configuration change, implementation exception, history rewrite or credential rotation is approved. Every additional fingerprint must still fail. Existing scanner failure evidence and immutable original review must remain retained.

This artifact approves the concrete amendment; the reviewer did not apply it. The reviewed .gitleaksignore has no new entries, and the pinned full-history scan remains exit 1. After application, root must rerun gitleaks 8.30.1 over full history and preserve actual SHA/start/end/exit privately; require zero findings and exit 0 before closing the scanner gate.

The separate source review is **issues_found**, with new WARNING WR-04 concerning the expired-token fixture. This bounded scanner approval does not waive that finding, accept 04-17, or authorize starting 04-02 or publishing remotely.
