# Plan14 projection notification ownership — proposed

Add `backend/internal/alertstream/delivery.go` to the original eight paths,
yielding nine declared source paths. Current projector commits durable delivery
rows but emits no NOTIFY; a dedicated LISTEN connection alone cannot wake clients
from actual projection. Emit one fixed channel/constant-payload pg_notify inside
the existing PG projection transaction after nonempty delivery insertion and
before COMMIT. Notification is only a hint; never change/advance any cursor.

Keep counter-row lock/atomic pending-counter-delivery operations/rollback/ACKloss
retry/tenant/UTC serialization unchanged. No HTTP/CH/non-PG network call under
the lock. Transactional pg_notify is PG-native and visible after commit; no
unregisterednotificationproducer/migration rewrite/globalDBtrigger needed.
Own isolatednativefixture may suppress publication/read a controlled backend
to prove unconditionalpoll surviveslosthints. Kill only owned LISTEN backendPID
and ensure reLISTEN/reconnect; no sharedservice stop/reset/group/offsetchange.

Independent checker must verify nine paths sufficient for config caps/actual
mainreadiness/lifecycle/perstreamqueue32/client100/5swrite/expiry/model10min/
ordinaryHTTPtimeouts and nativekill/suppression/slowreader/resourceproof. No
extra config fixture/source dependency hidden. Any causal additional path needs
checkedamendment beforeedit. Existingconfig/httpx ownedpaths are optional if
nochange needed; preserveordinarytimeouts, no globaldisable.

ActualSSE13 browserdropprivateproxy neededgracefulEOF throughVite; original
res.destroyfailurepreserved; reuse13-review proxy/passpatterns forbrowserproof.
NEWPG25432/schema8/CH28123+29000/Redis26379/Kafka39092 only;detectorhistorylost
8healthyseparatehold,no fullpipeline/reset/recoveryclaim. No migration14 or
stale006migrator/defaultoldruntime/seed/prune/remote. Roottracking/reviews,
executor declaredsources/private14evidence/14SUMMARY, userdirtypreserved.
