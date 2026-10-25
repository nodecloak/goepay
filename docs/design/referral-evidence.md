# Invitation reward evidence and implementation boundaries

Read-only source research, 2026-10-23. No real payment, secret-file access, or test execution occurred during research. Paths below are repository-relative source citations, not a claim that Release and Pro are equivalent.

## Proven Release behavior

- `epay_release/includes/functions.php:604-614`: fee is `max(round(realmoney-getmoney,2),0)`. Profit starts with that fee and subtracts `realmoney*channel.costrate/100` when positive costrate exists; profit may be negative.
- `functions.php:707-734`: immediate ordinary invitation reward applies only to tid 0/3 and false invite_mode. Merchant upid selects promoter; promoter group config merges over the current config. Type 0/default uses order.money; type 1 uses fee; type 2 uses profit. Each calculation is `round(base*invite_rate/100,2)`.
- `functions.php:725-727`: only type 0 and false invite_order_fee cap the reward at fee. Type 1/2 have no equivalent explicit cap. Positive final reward is credited; rate uses `!empty`, not a positive/range check.
- `epay_release/admin/set.php:563-584`: UI names types 0=order amount, 1=fee, 2=fee profit; mode 0=immediate, 1=daily; invite_order_fee 0=enforce cap, 1=disable. Rates are text inputs. `admin/ajax.php:113-128` saves posted setting strings without invitation precision/range validation. `admin/ajax_user.php:359-388` similarly stores group config JSON. Do not claim a legacy 0..100% or two-decimal percentage restriction.
- `functions.php:1211-1228`, `functions.php:475-476`: group falls back to gid 0 when group missing; only null/empty string are ignored, so string zero is an explicit override. `admin/gedit.php:242-253` exposes group invite_open and invite_rate.
- `includes/lib/Plugin.php:45-48` first merges the paying merchant group into global config; invitation handling then merges promoter group. Thus missing promoter values may inherit payer group values. No dedicated merchant-level invitation-policy override was established. The immediate mode check happens before promoter config merge.
- `user/invite.php:8-14,33-34`: page uses its effective user-group config, generates invite URL, and shows a 100-unit illustration. Its unconditional cap text and its lack of a type-2-specific explanation are not authoritative core behavior.

## Binding, membership, daily task, reversals

- `functions.php:1083-1098`: promotion code XORs UID bytes with SYS_KEY, then base64 without padding; inverse decoding has no independent signature. `index.php:19-24`, `user/index.php:5-10`, `user/reg.php:13-18` save a numeric decoded UID in session. `user/ajax.php:313-337` binds session invite_uid as upid for free registration or caches it for paid registration; `functions.php:618-633` binds cached upid. These snippets do not prove existence/enabled/self/cycle checks.
- `functions.php:659-674` and `user/ajax2.php:850-875`: membership reward is independent, based on purchase amount and invite_groupbuy_rate, requiring effective invite_open and positive rate. It has no ordinary type/mode/fee cap, and credit calls do not pass a membership purchase event ID.
- `cron.php:163-179`: daily mode sums yesterday's realmoney by merchant for status 1/3, with no tid/type/cap filter; it uses promoter group and records invited merchant UID in trade_no. It is not batching the immediate formula. `cron.php:68-70,159` uses a day marker saved before reward loop: no per-award exactly-once proof, and interruption can omit rewards.
- `functions.php:789-807`: only the unrelated transfer-return record has explicit deduplication. Credit/debit locks the user balance, then adds/subtracts and inserts a record; no nonnegative debit constraint or platform reward funding account appears here.
- `includes/lib/Payment.php:348-371`: callback checks caller-supplied order status then updates status without a conditional old-status predicate or order lock in this snippet. Ordinary repeated callbacks after reread are suppressed; concurrent exactly-once behavior is not proven.
- `includes/lib/Order.php:6-37`: freeze/unfreeze only removes/restores merchant getmoney and changes status. It does not reclaim promoter reward; daily reward includes frozen status 3.
- `includes/lib/Order.php:65-134` and `admin/ajax_order.php:322-350`: read refund chain handles merchant debit, direct-clearing fee return, order/refund records; it contains no invitation reward lookup or reversal. This is limited evidence about the read Release core, not a claim about all Pro code.

## Proven Pro and unavailable branches

- `epay_pro/route/user.php:85-96`: GET `/user/invite/inviteInfo`, POST `/user/invite/inviteList`, and membership routes have session, referer, and authenticated-user middleware. `docs/baseline/routes.csv:400-401`: USER-R055/R056.
- `epay_pro/app/cron/Order.php:49-75`: readable daily branch uses status 1/3 and SUM(realmoney), promoter group open/rate, no type/cap. Day marker is written at line 94 after completion, still without a per-award key.
- `epay_pro/app/async/OrderAfter.php:32-38`: merges paying merchant group, calls OrderProcessService.executeOrderAfter.
- `epay_pro/app/common.php:425-453,667-684,746-769`: readable balance, promotion-code and group functions establish only their own behavior.
- `app/service/PaymentService.php`, `OrderProcessService.php`, `controller/user/Invite.php`, `Group.php` begin with Swoole Loader encoded payloads. Pro immediate type semantics, result JSON fields, final registration validation, membership rewards and refund clawback are unproven. Do not infer full equivalence from Release.

## Current Go integration points

- `internal/payment/fees.go:13-19,92-144`: integer-cent/BPS retained-amount rounding and immutable new-order fee policy. Purchased active group overrides administrative baseline; replay returns old order. `db/migrations/000015_payment_fees.up.sql:14-20` prevents fee snapshot mutation.
- `internal/payment/store.go:249-303`: order lock, inbox dedupe, merchant net and platform fees journals, paid state/outbox share one transaction. Reward must consume real original fee budget, not unfunded clearing credit.
- `internal/payment/scoped_ledger.go:15-49`: canonical account locks, unique event key, balanced immutable ledger and insufficient-funds rejection. Migration 15 makes fees/fee_reserved nonnegative.
- `internal/payment/reservations.go:20-92,108-152`: refund locks order, fixes allocations, reserves funds before upstream call; unknown keeps reserve, failed releases, succeeded settles. `provider_flow_operations.go:183-225` calls provider only after reservation.
- `internal/payment/order_income_hold.go:52-60`: active income hold rejects new refund; holds do not turn paid orders into unpaid orders.
- `internal/enrollment/service.go:399-421`: InviteCode is registration admission code consumption, not promoter binding. `db/migrations/000036_public_enrollment.up.sql:36-42` records immutable uses. No promoter relation was found.
- `internal/payment/membership_deposit.go:46-94`: unique membership purchase and atomic balance payment; price enters clearing. No membership revenue/reward budget exists; do not use ordinary payment fees to subsidize it.

## Safe next-stage proposal, deliberately different where needed

1. Immutable referral relation: unique invited merchant, promoter FK, source token/evidence. Bind in verified registration transaction; separate promotion token from admission invitation. Validate existence, enabled, self/cycle; unknown legacy evidence stays unresolved.
2. Start with explicit immediate ordinary-payment fee-basis policy and rate 0..10000 BPS. Fix promoter, effective policy/revision, basis, rate and original platform_fee at order creation. Changes affect new orders only. Any other supported basis must also cap reward at original fee. Profit basis requires audited upstream cost snapshot, which was not found on Go payment orders.
3. Unique immutable reward per order/event; debit fees and credit promoter available using deterministic journal key. Validate event and snapshot before relying on journal dedupe. No reward on unconfirmed/unknown payment or zero fee.
4. Reserve/reverse the reward before an upstream refund call. Insufficient promoter funds must reject before calling upstream, never create negative available or silently fund from clearing. Unknown keeps the provisional reversal; failed restores it; success finalizes it. Trusted upstream success with local persistence trouble remains reconciliation/unknown, never failed or resent.
5. A minimal explicit `any_successful_refund_voids_reward` policy can reverse the entire remaining reward on any successful partial/full refund, serializing refunds touching one provisional reward. This is a new conservative policy, not legacy parity. Proportional clawback needs cumulative rounding plus failed-reservation-hole design.
6. Membership rewards remain disabled until membership revenue budget, purchase-bound unique reward and membership refund policy are implemented separately.

## Minimum test matrix

| Area | Required evidence |
|---|---|
| Precision | 1/3 cents at 50%, zero/10000 BPS, maximum amount no overflow, invalid negative/excess rate rejected |
| Binding | Promotion vs admission independent, invalid/self/cycle rejected, registration rollback restores OTP/code use/relation |
| Snapshot | Changed group/rate/promoter and expired membership do not alter old-order replay or late payment |
| Exactly once | Duplicate/concurrent callbacks, distinct event IDs and outbox retry produce one award and balanced fee debit |
| Budget | Zero fee gives zero reward, every basis bounded by original fee, unrelated pooled fees cannot enlarge award |
| States | No award for created/pending/unknown/failed; trusted late success awards once |
| Freeze | Paid stays paid, freeze/unfreeze cannot award twice, existing refund hold gate retained |
| Refund | Partial/full, unknown to terminal, failed/retry, duplicate/refund concurrency, total reversal <= original award |
| Insufficient | Spent reward blocks upstream request without negative accounts or altered history |
| Atomicity | Reward/refund journal or audit failure rolls back the complete transaction |
| Membership | External number replay, free purchase, separate revenue budget |

Existing useful tests: fees_test.go:42,81,161; order_income_hold_test.go:282; enrollment/service_test.go:260,423. These citations do not assert they were executed during research.
