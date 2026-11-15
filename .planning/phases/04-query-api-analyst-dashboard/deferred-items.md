# Phase 4 deferred items

## From 04-18 (G1), 2026-11-15

### D-04-18-01: e2e test 13 is stale after 04-20 (G4); phase4-browser stays red at HEAD

- **Symptom:** a scrubbed-env `scripts/check-phase4-browser.sh` at HEAD eb01c1d (sentinel-g1check) gives 12/13. Test 13 `live reconnect recovers exact missed commits through each query replica` times out at `web/e2e/phase4.spec.ts:730` waiting for `getByRole('button', { name: 'Refresh alerts' })`.
- **Cause:** 04-20 (a59f365) suppresses the banner for the initial catch-up, so the first-load "New alerts available" notice and its "Refresh alerts" button no longer appear. The spec still clicks that button to "acknowledge the initial catch-up". 04-20 was web-only and did not run Playwright (04-20-SUMMARY).
- **Evidence it is not a launcher/G1 issue:** the same scrubbed run, made from a temporary worktree at HEAD with only the four 04-20 `web/src` files restored to bbd8d35^, gives 13/13 (`.local/phase4-acceptance/04-18/9-browser-isolation-cwdfix.log`).
- **Suggested fix (owner: G4 follow-up, not 04-18):** replace the click at spec:730 with an assertion that, after the initial catch-up, the banner and button are absent. G4 requires exactly that, so the check becomes stronger, not weaker. Keep the rest of the reconnect rounds unchanged.
