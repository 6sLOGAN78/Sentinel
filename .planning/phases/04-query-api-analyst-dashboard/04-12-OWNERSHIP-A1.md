# Plan12 delivery client ownership — proposed for independent check

The approved PG-only backfill and Alerts catch-up requires an additional source
path `web/src/api/client.ts`. Existing generated transport is private; add one
guarded typed GET method for the new delivery envelope there. Preserve session
generation/lifetime/caller cancellation/post-body checks/current401 disposal.
Original nine plus this one yield ten paths, nine tracked and ignored schema.
No direct UI fetch, token path, numeric cursor conversion or hidden ownership.

The checker must inspect whether existing App and Alerts component fixtures
remain sufficient once initial catch-up runs, and identify any causally required
extra test fixture before source editing. Preserve previous list/deep-link/
cache/privacy/count assertions; do not weaken them to accommodate new calls.
Add only demonstrated paths through a separate amendment if required.

All original contract/ADR/008/projector/reversecommit/nativeproof and budgets
remain. No workflowseq-as-delivery, identitysequence/cross-networkI/Ounderlock,
CHdependencyforbackfill, arbitrarydiskGCclaim, unboundedclientappend or decimal
cursor coercion. Actual008 migration via current host tern on NEWPG25432 is
required; apply additive trigger+preexistingseed with realconcurrentcutover.
Shared006migrationimage is stale and EXIT1againstschema7; do not implicitrerun.
Use privateNEWcontext/currentephemeralAPI/browser, preserveoldheldstack/data/
userconfig/.serena. Rootowns tracking/reviews; executor onlydeclaredsources/
private12evidence/12SUMMARY. No newfeature/dependency/remote/Phase5authority.
