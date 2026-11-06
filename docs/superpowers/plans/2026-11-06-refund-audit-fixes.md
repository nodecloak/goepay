# Refund Audit Fixes Implementation Plan

> **For agentic workers:** Execute the following tasks in order, using the existing independent code reviewer before release. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the four findings from the review of commit `5933611`, add regression coverage, and deploy the verified commit.

**Architecture:** Allocate automatic API refund identities inside the same order-locked transaction that reserves the refund. Reuse transaction-aware provider readers for attestation, and keep its UI read-only until an unknown result has been successfully read back. Separate portable key-generation behavior from POSIX permission assertions.

**Tech Stack:** Go 1.27.3, PostgreSQL, Vue 3, Vitest, Playwright.

**Spec:** `docs/design/unknown-refund-resolution.md` and the four review findings accepted by the user on 2026-11-06.

## Global Constraints

- No real transactions or provider calls during acceptance; use synthetic local providers.
- Reuse `goepay-full195-windows-b378c4a3`; do not create containers, networks or volumes.
- Preserve the current preview and all failed or interrupted test receipts.
- Keep explicit refund numbers, original orders/channels, authorization and ledger invariants intact.
- Give new Go files descriptive names; rename numbered files when changing them.

### Task 1: Atomic automatic refunds

**Files:** `internal/payment/automatic_refund.go`, `internal/payment/automatic_refund_test.go`, `internal/payment/reservations.go`, `internal/payment/provider_flow_operations.go`, `internal/server/provider_http.go`, `tests/contract/refund_idempotency_test.go`.

- [ ] Add failing tests using `ReserveAPIRefund(ctx, merchantID, outTradeNo, "", amount)` for simultaneous retries; assert one refund ID, one reservation and unchanged API origin.
- [ ] Move the existing current/previous ten-minute identity selection into a transaction helper. `reserveAutomaticAPIRefund(..., now func() time.Time)` acquires merchant admission and the order lock, chooses the number, reserves through `reserveRefundTx`, then commits. Inject the clock only through this private helper for a deterministic boundary regression.
- [ ] Route only unnamed API refunds through that helper; assign `refundNo = r.RefundNo` before send/result lookup. Keep explicit-number semantics unchanged.
- [ ] Add boundary, rollback, final-failure retry, amount isolation and concurrent HTTP regressions; assert local provider invocation count and ledger reconciliation.

### Task 2: Attestation without connection reentry

**Files:** `internal/payment/refund_attestation.go`, `internal/payment/refund_attestation_test.go`, `tests/contract/refund_attestation_test.go`.

- [ ] Add a failing single-connection test while the current transaction owns the only connection. Assert no canceled acquire and prompt completion.
- [ ] Replace `g.owned` with `g.ownedTx(ctx, tx, ...)`; propagate lookup errors instead of interpreting them as permission to attest.
- [ ] Exercise the real GET/POST/funds-authorization chain with a single-connection pool, queryable and unqueryable providers, revoked sessions and immutable evidence.

### Task 3: Unknown-result recovery

**Files:** `web/src/components/RefundAttestationDialog.vue`, `web/tests/refund-attestation.test.ts`, `web/e2e/refund-attestation.spec.ts`.

- [ ] Add failing tests for a pending readback and a rejected readback after a lost POST response; POST count must remain one.
- [ ] Include loading/unknown state in submission guards. Keep unknown locked until a successful matching readback; clear the previous verification checkbox and require a new confirmation.
- [ ] Test mismatched IDs, final readbacks, an eligible readback and a second explicit operator confirmation, cancellation, and session changes.
- [ ] Run real-browser versions at desktop/mobile widths with fully intercepted synthetic APIs.

### Task 4: Portable key tests and release

**Files:** `cmd/cli/production_setup_test.go`, documentation source manifest, release evidence under `.tools/refund-audit-fixes`.

- [ ] Preserve the reproduced Windows failure. Check file existence/regularity and never-overwrite behavior on every platform; verify `0600` on POSIX rather than interpreting Windows mode bits as ACLs.
- [ ] Run the same POSIX assertion using a cross-compiled Linux test binary in the existing local test container, with no database or provider operations.
- [ ] Run focused Go regressions, all frontend unit tests, production build and affected browser flows; run the full Go suite against a fresh owned database in the existing pool. Treat optional skips and failures explicitly.
- [ ] Obtain independent review, regenerate/check generated artifacts, freeze source hashes, and build Linux API/CLI/worker binaries with commit metadata.
- [ ] Commit and push the tested source. Back up production, quiesce existing workers/API, apply additive pending migrations, verify ledger/configuration, atomically promote backend/static releases, and retain a rollback path.
- [ ] Verify health, public assets and read-only online UI. Confirm credentials/configuration and original orders/channels were preserved; save the deployment receipt.
