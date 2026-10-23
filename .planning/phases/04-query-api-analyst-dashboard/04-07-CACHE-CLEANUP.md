# Approved cache-only disk recovery

User explicitly replied "okk" to removing only unused default-builder build
cache, preserving code/images/containers/volumes, after the separate demo was
blocked below its4GiBadmission floor. This is distinct from the earlier choice1
approval to create a separate demo. No wider cleanup was inferred.

Read-only checks confirmed Docker contextdefault, builderdefault using Docker
driver/BuildKitv0.34.0, and installed prune flags. Root executed exactly:

`docker builder prune --builder default --force`

No --all, system/image/container/network/volume prune, source or Go-cache
deletion. Actual prune EXIT0/3.173s, END2026-10-23T23:12:58.608388Z; tool reports
3.717GB reclaimed. Free bytes increased4,019,933,184 to7,720,230,912.
Outer private wrapper EXIT0/3.927s, END23:12:59.019351Z.

Sorted complete image identity/tag/digest records, container identity/image/name
records, volume names, network identity/name records, Docker context and Git
status compare exactly equal before/after. All userconfig/.serena dirt remains
unstaged. No existing stack resource was created/recreated/stopped/removed.
Cache is regenerable by later builds; original cached layers are not restored
automatically and later builds may be slower.

Private evidence under04-07(0700/0600): approved-cache-before.json,
approved-cache-after.json, approved-cache-prune.log and
root-approved-cache-cleanup.log. The private script refuses nondefault context
and uses exclusive evidence creation to prevent accidental duplicate cleanup.

This one approved cache action is finished. It does NOT authorize further
pruning or old runtime/data mutation. Fresh separate demo still must repeat
disk/port/project/isolation admission, preserve>=2GiBverification margin and
prove all9newservices/1000exactevents/controlleddetection/oldpreservation under
the independently approved STACK-RECOVERY-PLAN. No07acceptance/Phase5/remote
publication follows from disk recovery alone.
