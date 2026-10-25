# Preauthorization next stage — design only

This is an offline source assessment and proposed implementation boundary, not implemented functionality or a compatibility/completion claim. No PostgreSQL tests, external calls, encrypted-controller decryption, production access, real credentials, or real transactions were used. Only this document is added; current source and frozen candidates are unchanged.

## Direct local evidence

| Evidence | What it establishes |
|---|---|
| `reference/server-pro/plugins/payment/alipay/AlipayPlugin.php:353–371,432–458,554–571,622–637` | Products 5/10 create APP/mini freeze material; product 9 creates a voucher. Original freeze `out_order_no` and `out_request_no` both equal platform `trade_no`, amount is original `realmoney`, optional payee is configured `appmchid`. |
| Same plugin `:803–840` | A verified freeze notification supplies `auth_no`/payer. The plugin captures original order money through `scanPay`, `PREAUTH_PAY`, `auth_confirm_mode=COMPLETE`, then processes the returned payment `trade_no`. This is one final capture, not proof of multiple captures. |
| `epay_pro/app/lib/Payment.php:166–211` | Manual helper queries the original freeze operation. `FINISH` leads to ordinary payment query. `AUTHORIZED` with positive `rest_amount` leads to capture of that remaining amount, using the same platform `out_trade_no` and `COMPLETE`. It does **not** expose an arbitrary caller-selected capture amount or a loop of child captures. |
| Same file `:218–241` | Manual unfreeze queries that freeze, requires `AUTHORIZED` and positive remainder, then unfreezes the remainder using a newly generated timestamp/random `out_request_no`. This is evidence for residual unfreeze protocol, not its hidden controller permissions or a safe retry policy. |
| `epay_pro/vendor/cccyun/alipay-sdk/src/AlipayTradeService.php:343–382` | Exact wrappers: APP freeze → `alipay.fund.auth.order.app.freeze`; unfreeze → `alipay.fund.auth.order.unfreeze`; authorization-operation cancel → `alipay.fund.auth.operation.cancel`; operation query → `alipay.fund.auth.operation.detail.query`. |
| Same SDK `:39,178–181`; plugin `:725` | `scanPay` is ordinary `alipay.trade.pay`; ordinary `cancel` is `alipay.trade.cancel`. The plugin's scan-payment cancellation must not be relabelled authorization revocation. |

The Release SDK copy has the same named preauthorization wrappers; its `includes/lib/Payment.php:571–622` also uses queried remainder with `COMPLETE`/unfreeze. Neither inspected plaintext business path proves non-final `NOT_COMPLETE` capture, multiple capture identifiers, or permission to invoke authorization cancel.

**Unresolved identifiers:** no occurrence of `authfunds_amount`/`authfund_amount`/`auth_funds` or a preauthorization residual-release-policy field was found in the searched plaintext PHP, Go, API and design sources. `requestNo`/`orderNo` are not demonstrated original preauthorization business parameter names here; the evidenced wire names are `out_request_no`/`out_order_no`. Any claimed hidden field, unused policy, revoke alias or enum-to-controller permission mapping remains unverified. Obtain its exact plaintext declaration/call site before wiring it. A generic SDK method, enum, label or unrelated sharing policy grants no business privilege.

## Current Go boundary and call graph

`internal/channelconfig/preauth_products.go` selects SDK material when 5 or 10 is present, otherwise voucher for 9. Voucher product-profile differences are already documented in `preauth-voucher.md`; product numbers do not enable partial capture.

```text
SubmitPreauthorization
  → immutable provider attempt/account binding → Freeze/material
verified freeze callback → RecordPreauthorization
  → BeginPreauthorizationCapture [commit one processing claim + query job]
  → captureClaimedPreauthorization → Alipay.CaptureAuthorization
  → authenticated payment event → markPaid + PaidReceipt + ledger transaction

explicit capture console → actor intent audit + ownership/policy checks
  → exact authenticated trade-not-found + fresh full FREEZE/remainder evidence
  → same claim/send/receipt path

explicit release → durable release intent (blocks capture)
  → payment/freeze preflight → committed sent=true barrier → Unfreeze
  → query original UA operation + explicit zero remainder → close unpaid order
```

The limiting files are `payment/preauth.go`, `preauth_flow.go`, `preauth_manual_capture.go`, `preauth_capture_settlement.go`, `preauth_release*.go`, and `adapters/payment/alipay_preauth.go` (all under `internal`). Migrations 23/25 enforce one authorization and one release per order, immutable identity, no return to capture `none`, no unknown/failed capture reacquisition, and capture/release exclusion. They are deployed/frozen and must not be rewritten.

The current capture request always uses original `TradeNo`, `o.Amount` and `COMPLETE`; manual preflight requires known `rest_amount == authorization.amount == order.amount`. Partial remaining balances are deliberately rejected. `verified_payment_receipts` has primary key `order_id` and unique `(channel_id,provider_trade_no)` (migration21). `paid_receipt.go` rejects a second different payment receipt for that order. `store.go:248–318` marks the parent captured and credits its full snapshotted net/fee once. Consequently, accepting a smaller response or repeatedly calling `markPaid` cannot safely implement partial/multiple capture.

Identity mapping must stay explicit:

- Freeze order/request numbers currently both equal original platform trade number; provider `auth_no` identifies authorization, never payment.
- Capture `out_trade_no` currently also equals that number; the returned provider payment `trade_no`, exact amount, CNY and original buyer are independently authenticated before receipt/credit.
- Release uses immutable `UA{order_id}` as a distinct operation request number. Recovery queries that exact UA, not a replacement number.
- Preserve original channel/AppID/seller-account scope and buyer. A runtime credential/account change cannot select a new identity for an old authorization.

There is no currently demonstrated residual-release policy hook to activate. The release flow is hard-coded to **full unused** authorization: partial remainder fails preflight; processing/unknown capture additionally needs authenticated closed-trade evidence before its first release send. Successful unfreeze itself is insufficient; a matching `UNFREEZE` query and known zero remainder are required. `CancelAuthorization` is adapter-only, with no durable business entry point. Its synthesized success event/expected amount is not independently verified zero-remainder evidence.

## Minimal safe next increment

Start with **durable read-only authorization observations and a partial-balance diagnostic**, keeping all existing capture/release sends unchanged. Add a new migration after the allocated current head, plus proposed `internal/payment/preauth_observation.go`/tests and an explicit owner-scoped console query. Persist authenticated freeze identity, queried operation number/type, original amount, known remainder, provider status, account binding and observation time with domain-separated HMAC. Missing identity/remainder or unrecognized consumption is unresolved, never an executable balance. Historical rows without new proof remain read-only; do not reconstruct proof from mutable status caches. No automatic release policy, new capture, cancel or revoke alias is enabled by this increment.

This gives a concrete safe intermediate deliverable while the following contract decisions are resolved. It is insufficient by itself to authorize a new financial write.

For the subsequent opt-in capture increment, use a new authorization-family model rather than loosening migrations23/25 or the existing receipt uniqueness:

1. Immutable per-capture intent: parent authorization, merchant/account/buyer, client idempotency key, stable child payment `out_trade_no`, exact amount, final/non-final mode, source observation and policy revision. One in-flight/unknown operation per authorization initially; no number rotation or automatic retry after a committed claim.
2. Under the parent lock, bound committed captures + releases + unresolved reservations by the original authorized amount. Fresh signed remainder is a required cross-check, not proof of what unrecorded payments earned merchant credit. Unknown consumption blocks new writes.
3. Separate immutable capture receipts and exact per-capture ledger events, or explicitly linked child payment orders using the normal payment pipeline. Never feed a smaller payment into the original parent's full-amount `markPaid`. Define cumulative fee rounding, refunds, parent status, settlement eligibility, referrals, printing and merchant callbacks before exposing partial payments. Do not invent those semantics from the PHP helper.
4. Snapshot a residual policy only after its exact approved contract exists: retain, final-completion behavior, or explicit residual unfreeze. Reserve release/capture mutually; bind amount and stable request number; require all prior unknowns resolved. Buyer authorization/release never credits merchant available/reserved accounts.
5. Revoke/cancel needs its own durable intent/claim, authenticated confirmation profile and explicit funds-action target. Establish whether the targeted request number denotes the original freeze or another operation and what signed terminal/remaining fields confirm it. Ordinary trade cancel, auth operation cancel and unfreeze remain distinct. An evidenced legacy alias may canonicalize to the **same** protected operation before CSRF/funds-target hashing; no GET side effects, new permission or alias-based resend.

## Required evidence and tests before financial expansion

Blockers are precise non-final capture request/response/query examples; stable multiple-capture number semantics; the claimed amount/policy/alias declarations and controller authorization; cancel terminal/remainder evidence; and partial-payment accounting/callback policy. No production sample or real money is needed for the next local design/test stage: approved plaintext contracts and independently signed synthetic fixtures suffice. Until then, label broader compatibility unknown and keep writes disabled.

Keep the existing `preauth*_test.go`, adapter preauth/voucher tests, reconciliation authorization-release tests and `tests/contract/preauth_{capture,release}_console_test.go` as regression baselines. New tests must cover:

- Signed observation identity/account/amount/remainder validation, unknown fields/missing remainder, HMAC corruption and missing evidence rows; owner/delegated/CSRF boundaries; query performs zero writes.
- Capture sum/overflow and one-in-flight races; capture versus release/cancel; immutable source/number/amount; merchant or account revocation; audit/authorization/claim commit before I/O.
- Each original child number sent at most once after crash/timeout/failed persistence; only its signed query resolves it; unknown blocks further capacity. Duplicate/out-of-order callbacks credit exactly once.
- Multiple distinct provider payments cannot occupy the original single receipt; exact partial credit/fee/refund allocation, no credit on freeze/release/cancel, parent/callback semantics, late real payment after release.
- Every alias has identical ownership, method, CSRF, funds-action digest and send barrier to its canonical operation; unsupported or merely enumerated aliases remain rejected.

**Next execution step:** add failing local adapter/store/HTTP tests for the read-only observation boundary on an isolated candidate, then implement that bounded increment and review it independently. Allocate an additive migration only after checking the current head. Do not change frozen885, deployed migrations, or the current payment implementation as part of this design task.
