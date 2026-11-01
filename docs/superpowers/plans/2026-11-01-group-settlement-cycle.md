# Group settlement cycle implementation plan

> **For agentic workers:** Execute this focused task in the existing parent task; no further delegation is required.

**Goal:** Apply group settlement-cycle overrides to available funds, every outgoing reservation, quotes and settlement plan scopes, preserving original transaction replay.

**Architecture:** Extend the shared typed group settlement parser and add an effective-cycle reader based on the existing membership-aware group resolver. Blank/`_` inherits, `0` is D+0 and `1` is D+1. Lock the cycle source for new outgoing reservations and recheck membership expiry after funds confirmation. Per-merchant plan cycle fields are omitted for inherited policies to preserve historical signed JSON.

**Tech Stack:** Go, PostgreSQL, Vue, Vitest.

**Spec:** Parent assignment; local reference `epay_release/admin/gedit.php:115` and `epay_pro/app/common.php:748` confirm original group 1/2 values are normalized to global 0/1. No reference source is added to version control.

## Constraints

- Reuse existing synthetic PostgreSQL on loopback 55101 with isolated databases and zero-connection cleanup; coordinate a single backend runner.
- Preserve frozen preview and all unrelated resources. No production write, commit, push or deployment.
- Do not change ResourcePage, merchant creation or account files; group schema/validation, payment availability/reservation/plan code and focused tests only.
- Group saves never create automatic payout authorities.

## Steps

- [x] Add failing schema/CRUD, group-cycle availability/outgoing/replay, expiry-after-confirmation, and plan-scope tests; save red logs.
- [x] Add typed cycle parsing and the group field, then resolve it through the existing effective group.
- [x] Apply the cycle in coherent funds views and lock/revalidate it in new outgoing reservations; add optional cycle facts to plan scopes and compare them before new generation.
- [x] Run targeted Go/PostgreSQL and frontend regressions, typecheck and diff checks; save evidence.

## Verification evidence

- `.tools/production-parity-20261101/group-cycle-red.log`: missing schema, quote/reservation, membership-expiry and scope behavior failed before implementation.
- `group-cycle-green.log`: two backoffice and eighteen payment main tests passed; original154 signed scope bytes are unchanged.
- `group-cycle-final-payment-green.log`: missing merchant error compatibility and threshold plan maturity/completed replay passed after the final source changes.
- `group-cycle-auth-red.log` and `group-cycle-auth-green.log`: cycle/minimum digest and per-merchant summary failed then passed. The latter log records a transient payment vet failure while another agent was writing the batch helper; the independent payment rerun above passed.
- `group-batch-digest-red.log` then the server portion of `group-cycle-auth-green.log`: group/global batch switch changes now alter the confirmation digest. `group-deposit-digest-red.log` and `group-deposit-digest-green.log`: all three confirmed deposit keys alter it independently at group and global scope (six cases).
- `group-cycle-ui-green.log`: six files, twenty-two frontend tests passed; `group-cycle-typecheck.log` passed. `group-auth-ui-green.log`: three files, nine authorization/group tests passed after the summary labels changed.
- Every isolated PostgreSQL database was removed only after a zero-connection check. No group save or summary read creates automatic payout authority.
