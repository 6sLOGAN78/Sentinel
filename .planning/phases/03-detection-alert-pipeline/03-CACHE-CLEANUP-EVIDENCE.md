# Approved build-cache cleanup

User explicitly approved removal of unused Docker build cache after the disk-space
hold. Approval does not include volumes, containers, images, project files or
the shared Go cache. The Docker destructive guardrails and per-command reference
were read before cleanup; default context and default docker-driver builder were
resolved and local CLI flags checked.

Executed `docker builder prune --builder default --force` without `--all`.
Actual exit0,2026-10-21T11:41:11Z–11:41:15Z; Docker reported9.189GB reclaimed.
Filesystem availability rose from2.3GB to11GB (94%used). Only regenerable unused
build cache was removed; future builds may take longer.

Root saved sorted complete image-ID, container-ID and volume-name inventories
before and after. All three byte-for-byte comparisons returned0: these resources
were unchanged. Private persistent evidence is in ignored
`.local/phase3-acceptance/approved-build-cache-cleanup.log` and
`cache-cleanup-{images,containers,volumes}-{before,after}.txt`.
No broad system/image/container/volume prune, reset or data deletion was used.

Kafka's normal preserved-volume `compose up -d --wait kafka` recovery exited0,
11:42:00Z–11:42:18Z. Other infrastructure was observed stopped at that point;
Normal startup of all nine services using existing images/volumes then exited0,
11:42:59Z–11:43:26Z; all nine were healthy. No rebuild or reset was used.
The source/runtime is unchanged since the reviewed repairs. New full native
acceptance and hosted CI remain pending; cleanup itself is not a test pass.

The next exact `make test-integ` is isolated in transient user unit
`sentinel-phase3-post-cache-20261021.service`, with private ignored harness and
mode600 `post-cache-full-integ.log`. The Go32m timeout is unchanged; the outer
unit ceiling45m matches the existing hosted job cap. This execution arrangement
retains an independently queryable producer state if the terminal detaches.
Running-state ExecMainStatus0 is not a completed test result. Final actual
RETURNED_EXIT/END and terminal unit status must be checked before claiming PASS.
