# Group settlement recipient types

**Goal:** Apply supported group settlement rail restrictions to new recipient versions and settlement intents. Keep frozen recipient snapshots, history and recovery stable. Group 1 never overrides a platform 0 or provider capability.

**Scope:** Add typed parsing and group schema, a shared payment policy reader, Store save/initial setup/quote/new-request checks, and the existing allowed-types endpoint's bounded administrator group preview. Parent owns the service validator hook and frontend changes.

**Compatibility:** Blank/`_` inherits, 0 closes, 1 permits only within platform availability. Only alipay/wxpay/bank are executable. Preserve legacy missing-recipient requests if at least one supported rail remains; such requests still cannot prepare payout. Exact existing recipient/request replay precedes new gates. New group saves never create payout authority.

**Source mapping:** `internal/backoffice/settlement_recipient.go` serves the existing chooser/save and checks only global switches; `merchant_create_setup.go` prechecks the same globals and calls Store initial setup inside merchant creation. `internal/payment/settlement_recipient.go` normalizes/encrypts versions and freezes the original request recipient. `settlement_quote.go` and `ledger_requests.go` are the read/new-intent consumers. `settlement_payout.go` deliberately retains `originalSettlementRecipient` plus actual provider transfer types, AppID, bank product and runtime checks; new group restrictions do not reinterpret frozen intent.

- [x] Add behavior/schema/HTTP tests, observe expected failures using one coordinated isolated PostgreSQL runner.
- [x] Implement typed restrictions, locked current-policy reads, deadline and missing-setting rechecks for new durable writes.
- [x] Use the same restriction in recipient choice, quote and new requests; preserve frozen payout capability checks.
- [x] Run focused regressions, check diff, report source mapping and evidence, then freeze Go.

## Evidence

- `.tools/production-parity-20261101/group-rails-red.log`: missing group schema, new recipient/initial setup, quote/new-request and platform-ceiling failures. Existing frozen payout/recovery and inactive initial setup were positive controls. Its server leg encountered a separate agent's temporary missing helper and is not digest evidence.
- `group-rails-preview-fee-red.log`: typed parser passed; all six payment behavior tests passed in 37.970s. Server separately reproduced all six recipient-switch digest gaps, both redpack digest gaps and the incorrect single/batch effective-fee summaries. Its initial BO failure was a test actor fixture error, corrected to the existing super administrator fixture.
- `group-rails-admin-merchant-red.log`: corrected BO setup reached and proved missing administrator merchant preview.
- `group-rails-fee-final-green.log`: two BO tests passed (12.426s), two original Store recipient compatibility tests passed (12.115s), and three server digest/summary tests passed (30.877s). The latter also verifies no-override action bytes remain stable across missing/null/empty/_ and preserves distinct 0 versus 0.00 source binding with per-item batch rounding.
- Final diff check passed; only own isolated PostgreSQL databases were cleaned, after verifying zero connections. No frontend, production or preview resource was changed in this task.
