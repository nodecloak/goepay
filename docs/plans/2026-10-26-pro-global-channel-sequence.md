# Pro Global Channel Sequence Implementation Plan

> **For agentic workers:** Execute this authorized patch inline, task by task with the existing TDD and verification skills. No commit, deployment or additional agents are authorized by this plan.

**Goal:** Make new group `channel=-4` assignments select the globally least recently used eligible parent channel, ordered by exact ID for equal times, without changing old assignments or financial snapshots.

**Architecture:** A new version-97 table stores each parent channel's last committed selection in integer Unix milliseconds and references its immutable route assignment. Existing sorted parent advisory locks serialize overlapping candidate sets across groups. A small server helper sorts eligible channels using the locked global facts and advances the chosen head in the same transaction as the route assignment. Old group counters remain untouched and readable, but new -4 selections no longer depend on them.

**Tech Stack:** Existing Go/pgx/PostgreSQL only; offline Windows toolchain; no new dependency or UI.

**Spec:** Local `epay_pro/app/lib/Channel.php:193-215` (SHA256 `65c6d57bcd4678fe805834e6927a34576be31f382a821d717743e06cf8803de8`) and `E:/GOEPAY/.tools/channel168-contract-review.md` route evidence. Pro uses global parent usetime ascending, exact numeric ID ascending, updates only after selection. The previous Release-derived shared-group-index contract differs when groups share parents or candidate sets change.

## Global constraints

- Source is frozen164, all1825 files checked before copying. Isolated development only; main, frozen snapshots and168A are untouched.
- Migration97 reserved by parent. This branch temporarily lacks96 and is **not deployable alone**; cumulative integration must verify95→96→97.
- No backfill of historical routes/usetime, no rewrite of orders/fees/attempts/old counters, no payment or upstream call.
- Only synthetic local PG in an owned labelled container and free loopback port. No unrelated container mutation.
- Existing allowlist, product/method, stop, group expiry, quote/routing, explicit-channel conflict and replay checks remain authoritative.

## Task1: Actual PG RED

Files: new `internal/server/sequence169_test.go`; existing groupRoutingFixture152 is reused without modification.

- [x] Add helpers that create distinct -4 groups/merchants with the same parent provider registry and build original compat.Params.
- [x] Test two groups: groupA first7, groupB first8 (old per-group cursor returns7 and must fail).
- [x] Test7/8/9 after7,8 then remove7: choose9 (old cursor wraps to8 and must fail).
- [x] Test equal-millisecond ties including9007199254740993/9007199254740995 retain numeric ID order; seeds are explicit new-table facts attached to genuine synthetic assignments.
- [x] Test replay including legacy assignment/unknown attempt keeps the old channel without advancing heads or old counters; failed explicit request/no eligible leaves no route/head changes.
- [x] Test two concurrent groups sharing7/8 choose distinct never-used parents under the original sorted advisory locks, no external I/O.
- [x] Run `go test -json ./internal/server -run '^TestSequence169' -count=1` against the ownedPG and preserve actual failing JSONL/exit result.

## Task2: Minimal implementation and migration

Files: new `db/migrations/000097_global_channel_last_used.up.sql`, new `internal/server/provider_global_sequence.go`; minimal changes `internal/server/provider_policy_routing.go`.

Interfaces: `orderGlobalSequence(ctx context.Context, tx pgx.Tx, channels []backoffice.PayRollChannel) error` sorts eligible slice in place by stored milliseconds default0 then int64ID. `recordGlobalSequence(ctx context.Context, tx pgx.Tx, mid int64, outTradeNo string, channel int64) error` upserts only this chosen head from the newly inserted immutable route; it is called before tx.Commit. Helpers do not select providers or write money.

- [x] Create positive-parentID PK table with nonnegative millisecond head and immutable routeFK, no INSERT SELECT historical backfill. Protect FK source identity and monotonic relationship against arbitrary reassignment using explicit SQL checks / tests.
- [x] Keep existing route order/group/parent locks; remove new -4 dependency on group counter acquisition/increment, leaving table and rows unchanged. Sort after eligibility filtering; choosefirst. After insertProviderRoute, advance onlychosen head inside same tx.
- [x] New head clock is DBclock_timestamp truncated to milliseconds (not Go wallclock); equal times use precise numeric ID. Unseen channels begin0, documented new-version warm start rather than fabricated historical usetime.
- [x] Run RED tests GREEN and migration shape/history unchanged tests. Failures roll back both assignment and lastused.

## Task3: Regression and delivery

- [x] Run affected original group route semantics / random roll / pay-roll quota / expiry / existing provider routing PG tests. Update only obsolete -4 test expectations with explicit new Pro evidence, preserving previous receipts.
- [x] Verify new migrator no historical UPDATE and no financial diffs. No source modifications outside allowlist. No browser tests needed for backend-only selection correction.
- [x] Generate delivery JSON/MD with parent manifest digest, every old/newSHA, PG RED/GREEN and affected receipts, migration96 gap boundary and all remaining limitations. Independent review requested from parent before integration.
- [ ] Preserve local ownedPG until independent review, then only label-verified owned cleanup. No commit/push/freeze/merge.

## Documentation errata

`2026-10-26-original-group-route-semantics.md:3,7` and historical `original-group-routing.md:3` accurately cite Release group.index but overextend it to current Pro. Pro uses global parent.usetime. `original-random-roll-groups.md:9` overstates encrypted boundaries: Pro Channel.php:293-309 is readable and proves all same-type enabled rolls are drawn before member eligibility, failure does not redraw;161runtimealreadymatches that order. These are new evidence corrections, not alteration of old frozen checkpoints or old verification receipts.

## Local verification completed (2026-10-26)

Core actualPG RED: cross-group7/7 vs7/8, removed-candidate8 vs9, concurrent7/7; missing-new-table scaffold failure also retained. Five new server roots PASS; schema/history migration root PASS and preserves all old columns/rows; affected49roots+8subtests PASS with2 existing PHP-only skips; actual signed HTTP15roots+4subtests PASS0skip. Three prior shared-group-index expectations were actually observed failing and preserved in a separate obsolete-counter receipt, then updated to the directly executed Pro contract. New Pro PHP interop defaults to compile/skip and separately ran1positive0skip with originalSHA check, pinnedPHP and networknone. Proprietary reference is present only in a nonpublishable temporary interop workspace.

No UI, fee, credential or financial workflow was modified. Default epoch has no historical usetime backfill; same-millisecond ties choose exact numeric parentID. This97-only development branch lacks concurrent96 and must not deploy alone; root integration must verify95→96→97 and cumulative affected platforms before publication.

## Independent-review P1 correction

The reviewer actually reproduced expiry during a new head write: a synthetic BEFORE head trigger waited0.8s while the membership had0.5s left; the previous pre-write clock check incorrectly committed one route and head after expiry. The independent RED log is preserved. Permanent `internal/server/sequence169_expiry_test.go` contains that exact regression. The final clock check now follows all assignment/head/rollcounter writes and their lock waits, immediately beforeCommit; an expired decision rolls back every new fact. Final core6roots PASS, affected50roots+8subtests PASS (2old PHP-only default skips), signed HTTP15roots+4subtests PASS. Migration1 and actual new Pro PHP1positive receipts remain valid because neitherSQL nor interoperability source changed. The initial5/49/15 receipts and pre-correction delivery manifest are retained under explicitpre-expiry names, not overwritten as historical green proof for the fixed source.
