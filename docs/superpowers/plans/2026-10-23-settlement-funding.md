# Settlement Funding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking. User explicitly authorizes inline continuous implementation; no delegation or execution-choice pause is needed.

**Goal:** Make settlement applications own their gross reservation and immutable encrypted recipient snapshot while preserving local-only legacy calls.

**Architecture:** Add request-scoped ledger accounts and immutable funding evidence, using shared journal posting. Append recipient configuration versions, then snapshot the current version inside the existing merchant/request transaction. Expose redacted local workflows; no provider send exists in this increment.

**Tech Stack:** Go, pgx/v5, PostgreSQL, existing Store AEAD/HMAC, PowerShell.

**Spec:** docs/design/settlement-funding.md

## Global Constraints

- Only migration000040_settlement_funding.up.sql; never change1..39.
- All tests use postgres://goepay:goepay_local_test@127.0.0.1:55432/goepay?sslmode=disable and random schema.
- GOTMPDIR/GOCACHE are E:\GOEPAY\.tools\go-tmp / go-cache. No production, real funds, secrets or provider I/O.
- Coordinate frontend date fix before modifying ledger_requests.go. Root owns server.go balance hook and shared auditFields filter.

## Task1: Scoped funding and journals

Files: db/migrations/000040_settlement_funding.up.sql, payment/settlement_funding.go, scoped_ledger.go, store.go, ledger_check.go, settlement_funding_test.go.

- [x] Add PG test: RequestFunds(settlements,681,0,"scope",100,..) must create one scope=requestID account with100, ordinary reserved remains0, Balance.Reserved=100. A second request must have a different account. CheckLedger must report0 invalid funding.
- [x] Run `go test ./internal/payment -run TestSettlementFunding -count=1` and observe missing-table/behavior red before implementation.
- [x] Add scope uniqueness/domain, immutable binding plus deferred funding proof guard. Extract `postByAccountIDs(ctx,tx,key,description,fromID,toID,amount)` from existing post with same account lock/negative balance rule; post must resolve onlyscope0. Reserve gross and reject using exact account, while old unbound requests retain old path.
- [x] Verify concurrent same-number and reject(12), foreign scope/refund cannot debit request funds, failure rollback, approved still funded, legacy staysunbound.

## Task2: Encrypted recipient versions and snapshots

Files: payment/settlement_recipient.go, settlement_recipient_test.go, ledger_requests.go, settlement_terms.go.

Interfaces: `SetSettlementRecipient(ctx,mid,in,expectedID,actor)`, `SettlementRecipient(ctx,mid)`, `RequestFundsWithSettlementRecipient(ctx,kind,mid,rid,no,amount,actor,expected,configID)`; old RequestFundsExpected delegates withnil configID.

- [x] Test configuration current pointer CAS, samecontent replay, per-merchant ownership, invalidtype/app/bankcode, AEAD wrongrecord context and changed config after request.
- [x] Observe red, implement domain-separated AEAD/HMAC and record validation, append-only configs plus independent request cipher. No raw PII in summaries or audit.
- [x] Test no-config compatible request hasmissing summary, optional configID prevents stale/new requests, old externalNo replay returns old recipient. Failure injecting snapshot/audit trigger must roll back money/configpointer/all metadata.

## Task3: Backoffice and public read models

Files: backoffice/settlement_recipient.go, workflows.go, settlement_funding_test.go; tests/contract/settlement_funding_test.go. Root hooks server admin summary.

- [x] Write HTTP red: merchant setsrecipient, GETredacts, POSTsettlements withconfigID returns funding/snapshot, merchant2 cannotread/write owner1; CSRFblocked. No provider calls/jobs/transfer rows are created.
- [x] Add workflow handler before genericworkflows, strict field parsing, admin/merchant identity, operation audit onlytype/IDs. Existing list calls funding/snapshot summary. Root adjusts globalreserved aggregate.
- [x] Run payment/backoffice/contract settlement/refund/ledger/funds tests and `go vet` affectedpackages. Record migrationSHA and fresh test output in testingdoc; request independent review before declaring freeze.

No Git metadata exists in this workspace, so checkpoints are tests plus immutable migration SHA rather than commits.
