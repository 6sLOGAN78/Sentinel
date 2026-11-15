# Phase 4 deferred items

## From 04-18 (G1), 2026-11-15

### D-04-18-01: e2e test 13 is stale after 04-20 (G4); phase4-browser stays red at HEAD

- **Symptom:** a scrubbed-env `scripts/check-phase4-browser.sh` at HEAD eb01c1d (sentinel-g1check) gives 12/13. Test 13 `live reconnect recovers exact missed commits through each query replica` times out at `web/e2e/phase4.spec.ts:730` waiting for `getByRole('button', { name: 'Refresh alerts' })`.
- **Cause:** 04-20 (a59f365) suppresses the banner for the initial catch-up, so the first-load "New alerts available" notice and its "Refresh alerts" button no longer appear. The spec still clicks that button to "acknowledge the initial catch-up". 04-20 was web-only and did not run Playwright (04-20-SUMMARY).
- **Evidence it is not a launcher/G1 issue:** the same scrubbed run, made from a temporary worktree at HEAD with only the four 04-20 `web/src` files restored to bbd8d35^, gives 13/13 (`.local/phase4-acceptance/04-18/9-browser-isolation-cwdfix.log`).
- **Suggested fix (owner: G4 follow-up, not 04-18):** replace the click at spec:730 with an assertion that, after the initial catch-up, the banner and button are absent. G4 requires exactly that, so the check becomes stronger, not weaker. Keep the rest of the reconnect rounds unchanged.

**D-04-18-01 resolved 2026-11-15:** `web/e2e/phase4.spec.ts` now asserts that neither the "New alerts available" notice nor the "Refresh alerts" button is present after the initial catch-up (stronger than clicking it away). Prettier, tsc and eslint pass; the real browser proof is the hosted phase4-browser job in 04-25.

## From 04-24 (G7), 2026-11-15

### D-04-24-01: ADR index and ADR gate cover only 0001-0006

- **Symptom:** `docs/decisions/README.md` lists 0001-0006 and now 0012, but not 0007-0009. `scripts/check-adrs.sh` globs `000[1-6]-*.md` and requires exactly six files, so 0007-0009 and 0012 are not heading-checked by the gate.
- **Scope:** pre-existing, not caused by 04-24. 04-24 added only the 0012 row. 0012 has all four required headings (checked by grep).
- **Suggested fix:** list 0007-0009 in the index and widen check-adrs to every `[0-9][0-9][0-9][0-9]-*.md`, with the count assertion replaced by a nonempty check.

**D-04-24-01 resolved 2026-11-15:** ADRs 0007-0009 now carry the Status/Context/Decision/Consequences headings (content kept; short Context sections added for 0007/0008, 0009 paragraphs grouped under headings), all are listed in `docs/decisions/README.md`, and `scripts/check-adrs.sh` now checks every ADR's headings and index entry while still requiring the six foundation ADRs. A heading mutation makes it exit 1.
