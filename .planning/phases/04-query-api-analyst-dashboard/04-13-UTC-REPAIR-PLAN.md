# Bounded prerequisite — preserve UTC delivery wire contract

Status: proposed, independent check required before source edits.

Actual committed13source2c88d18 passes nativeStream6 but browser loops on200
backfill before SSE attach. Private13/browser-1792968078977373286 evidence
(latest delivery-shape.json records exact data) shows all four WorkflowAlert
timestamps serialized with +05:30, while OpenAPI WorkflowAlert references
UTCTimestamp requiring Z. Strict new live validator rejects the invalid wire.
Retain all failed browser attempts, do not weaken validator/schema or alterTZ.

Root independently traced accepted12 Store.Read: it scans pgx timestamptz into
WorkflowAlert then appends without UTC normalization. Existing list/triage
adapters already normalize UTC. This is a prerequisite contract defect; the
accepted delivery ordering algorithm remains unchanged. No retention/reset or
counter repair. Phase13 fourteen-file feature scope stays unchanged.

Execute this separate two-path repair sequentially:
1. `backend/testphase4/delivery_test.go`: actual current query process with
   explicit non-UTC child timezone (e.g.Asia/Kolkata), real owned migrated PG,
   retrieve valid workflow through backfill and assert all four times endZ
   while representing exactly the inserted instants. Retain tenant/cursor/
   status/version/audit assertions. Causal tests-only RED must fail actualold
   wire, not compilation/setup. Do not mutate global Go time.Local or shared
   counter/history; use fixture child environment.
2. `backend/internal/alertstream/delivery.go`: normalize CreatedAt,UpdatedAt,
   WindowStart,WindowEnd to UTC after Scan and before appending delivery.
   Same typed WorkflowAlert, no extra fields; do not change SQL, tx/counter/
   cursor/tenant/pagination/projection logic or relax timestamp validation.

Scope exactly two source paths plus private13 evidence and this repair report
metadata. No fifteenth source file hidden in original13 task ownership; this
is a recorded separate prerequisite, with independent two-path review before
resuming browser13. Root owns tracking/reviews. All user dirt preserved.

Gates: causalREDcommit/minimalGREENcommit, exactnativeDeliveryphase4/race10m
andStream10m, unchangedschema/OpenAPI11/three-modequality asaffected, browser13
actualcurrentAPI at1440/375 with original strict timestamp checks. Normalmake
test sequential withwebgates so npmci cannotrace. ActualcommittedSHA/proof/
scans unchangedpolicy. NEWPG25432/schema8only/isolatedDB;originalold+NEWdetector
historyholds untouched. No timezoneworkaround/serverconfigreset/newlibrary/
remote/prune/newpipelineclaim. Independent repair source review required.
