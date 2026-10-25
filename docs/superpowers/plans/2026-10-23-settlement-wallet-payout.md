# Settlement Wallet Payout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Root explicitly authorizes inline continuous implementation; no approval pause or delegation.

**Goal:** Pay exactly the immutable net amount from an approved settlement's dedicated gross reservation once.

**Architecture:** Add immutable request→transfer binding and a durable unsent/claimed/cancelled barrier. Reuse authenticated provider sending/query, but dispatch result journals inside the original transaction to scoped funding; migration43 upgrades evidence invariants without changing40.

**Tech Stack:** Go/pgx/PostgreSQL; existing adapters, AEAD and funds action guard.

**Spec:** docs/design/settlement-wallet-payout.md

## Global Constraints

- Migration000043 only; no1..42 SQL edits. Bank unsupported. No provider I/O inPrepare.
- LocalPG55432 with randomschema; mockTLS and temporarykeys; E:.tools caches; no secrets/production/real funds.
- Root owns shared server/list/frontend hooks. Payment provider_transfers.go/reservations.go small fundinghooks are necessary shared modifications; coordinate before changes.

## Task1: Binding and read state

Files: db/migrations/000043_settlement_wallet_payout.up.sql, payment/settlement_payout.go, settlement_payout_test.go.

- [ ] PG red: approvedgross100 withfee3+net97 andavailable0 createsuniqueTF net97/cost97/fee0, scope still100, ordinaryreserved0; pending/missingrecipient/bank denied. OrdinarysingleQuery unsent returnslocal0I/O.
- [ ] Add immutablebinding andsendstate checks, ciphertext snapshottedfromoriginalrequest; Prepare verifies merchant/platform/provider/AppID/check but doesnotreserve again.
- [ ] Replace40invariantview in43 to verify all originalfunding plus boundTF and each permittedterminaljournal. Test duplicateprepare concurrency, channel/appmutation conflict andnoattemptjobs beforeclaim.

## Task2: Single claim and common result source

Files: payment/settlement_payout_flow.go, settlement_payout_resolution.go; hooks provider_transfers.go/reservations.go/settlement_funding.go.

- [ ] PG red12concurrentExecute; unknown onlyoneupstreamsend, originalTF readquery resolves net+fee once, availableunchanged. Cancel/Execute race permits onlyone, grant/auditfailure zeroI/O.
- [ ] Claim merchant→request→transfer, requireFundsAction exactkey, audit/queryjob commit before sharedsend. Add readonly/query/cancelmethods and singleQuery localhook.
- [ ] Add commonresolver lock/fundingdispatch inside originaltx, reject unsent resolution without cancellationproof. Verify successsource/net/fee andoriginalgrossrelease, same resultidempotence, latefailednotrefund.

## Task3: VerifiedTLS and scope delivery

Files: payment/settlement_payout_tls_test.go; design/testingdocs.

- [ ] RealTLS fakeAli validates signednet/recipient/TF anddurableclaim before503unknown; later signedquery sameTF→success; incorrectamount/signature nevercredits. AtleastwalletWx fake evidence exercisesAppIDtype binding andoriginalrecipient.
- [ ] Test currentmerchant/provider/whitelist changes, interruptionafterclaim, independentreadonlyrecoverystopwrite, noordinaryTransfer/batch resend, scopejournalguard corruption andcheckledger.
- [ ] Run targetedpayment+adapter+reconciliation tests/vet, freeze43hash/docs and report namedAPI/types to root. Root runs full/race/unifieddeployment and independentreview.
