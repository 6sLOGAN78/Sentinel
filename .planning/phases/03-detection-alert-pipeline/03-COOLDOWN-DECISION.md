# Phase 3 cooldown decision

**Recorded:** 2026-10-17. [VERIFIED: session date]

The user selected **option 1: actual alert-level cooldown suppressing repeat alerts for the same rule, tenant and entity across nearby windows**. The parent orchestrator relayed this explicit choice to the research specialist. [VERIFIED: parent task and follow-up messages recording user choice]

This supersedes the original research/planning interpretation of cooldown as a window-scoped candidate-only throttle. That interpretation did not reduce unique visible alerts beyond one-per-window deduplication and was rejected by the user. Separate eligible windows must retain distinct existing alert identities and remain investigable; a previously resolved workflow must not be reopened. [VERIFIED: selected user semantics via parent; DET-08; ALRT-02; backend/internal/contracts/dedupe.go]

The choice does not lock new alert-ID encoding, severity/cooldown numeric defaults, a package, or a PostgreSQL schema. The durable operational decision ledger/cursor, event-time clock, ownership/config fences, deployment wiring and recovery tests in `03-RESEARCH.md` are implementation recommendations to satisfy this choice. They are not claims that those mechanisms have already been implemented or proven. [VERIFIED: scope of user choice relayed by parent; ASSUMED: engineering design in amended research]

No application, plan, state, roadmap or configuration changes are made by this record. Research revision and plan regeneration are separate orchestrated steps. [VERIFIED: specialist ownership instruction]
