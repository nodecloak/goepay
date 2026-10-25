# Administrator Funds Authorization Implementation Plan

**Goal:** Add per-administrator payment-password settings and a reusable action-bound authorization dialog, preserving explicit final execution and query-only handling of uncertain results.

**Architecture:** Existing scoped ApiClient carries authorization only in one request header. Dedicated settings and modal components own short-lived passwords/challenge/grant state. Existing business pages integrate only after server builders and transactional guards freeze; this increment does not imply every money action is protected.

**Tech Stack:** Vue3, TypeScript, Vitest, Playwright; existing CSS and CSRF/session transport.

**Spec:** `E:/GOEPAY/docs/design/admin-funds-authorization.md` and protocol agent's HTTP contract.

## Constraints

- Administrator's own original session only; never use merchant login password as administrator funds password.
- Settings GET/PUT `/security/payment-password`; PUT current_password/current_payment_password/new_payment_password/code.
- Preview `POST /security/funds-authorizations/challenges` with complete admin target path and unchanged body; confirm challenge with payment_password. Grant travels only via `X-Payment-Authorization` on final original POST.
- Challenge/grant deadlines must be respected. Do not log, persist, put in URL, or copy grant. Clear credentials on validation failure, request failure, cancellation and unmount.
- Explicit summary confirmation precedes password confirmation; a separate explicit final execution follows successful authorization. HTTP202/network-unknown never automatically resubmits.
- No Docker or live tests until parent coordinates final deployment. Use E drive temporary directories; retain failure artifacts.

## Tasks

- [x] Write red unit/component tests for strict status/deadline/target handling, single-request header scope, password clearing and body binding.
- [x] Extend `src/api.ts` PUT support and transient authorization option; implement `src/funds-authorization.ts` validation helpers.
- [x] Build `PaymentPasswordSettings.vue`, mount only administrator security page. Test initial setup/change/TOTP/failed-request clearing.
- [x] Build `FundsAuthorizationModal.vue` with preview→summary confirmation→funds password→grant→explicit original POST; invalidate on prop edits, no retries after send boundary.
- [x] Run targeted and all unit/mock tests, typecheck/build and dependency audits. Update truthful implementation boundaries in webREADME. Latest run: 52 unit/component, 63 mocked browser passed; 7 explicit live tests skipped; type/build and full/production audits passed.
- [ ] After root transaction hook contract freezes, coordinate integration per business page without replacing existing idempotency keys or unknown-state recovery.

The frontend now intercepts the parent's exact administrator financial POST allowlist centrally. The original request caller receives the final status/data; HTTP202 and errors never trigger automatic reauthorization or resubmission. Backend transactional hooks and unified Docker/live verification remain separately pending.
