# Default Merchant Payment Channels Implementation Plan

> **For agentic workers:** Independently implement and freeze the narrow startup parser; independently review the collection-runtime integration and release repair. Reuse the existing synthetic PostgreSQL pool and retain actual red/green evidence.

**Goal:** New and existing eligible merchants without an explicit operator admission entry can receive through deliberately configured default payment sources, with effective-group restrictions and unchanged outgoing-funds authority.

**Architecture:** Add a strictly validated `GOEPAY_DEFAULT_PAYMENT_CHANNELS` source list distinct from `GOEPAY_MERCHANT_CHANNELS`. Construct a request-local collection view only; never mutate the root registry or the API/worker's generic merchant allowlist. Explicit entries, including empty lists, remain authoritative. Validate the current merchant/group before selecting defaults; private aliases retain their existing authenticated grant requirements. Original-order operations continue restoring their original sealed payment source.

**Tech Stack:** Existing Go/PostgreSQL provider runtime, merchantpolicy/grouppolicy and HTTP/browser fixtures; no new services or dependencies.

**Spec:** User authorization 2026-11-03 to fix the code and repair live data after the read-only diagnosis of merchants10000/10003, default group0 and the running API's explicit admissions10001/10002.

## Global Constraints

- Work in E:/GOEPAY. Preserve the preview and all unrelated local work. Reuse the existing test PostgreSQL container, isolate with owned GUID databases, and drop only after zero connections; no new containers/networks/volumes.
- No real payment/refund/transfer/settlement/provider transaction. Test all money paths with local simulation or test providers.
- Explicit `GOEPAY_MERCHANT_CHANNELS` entries, including `[]`, do not receive fallback. Unknown/private/unregistered sources never become defaults by catalog enumeration or group editing.
- Default admission is collection-only. Keep root ProviderFlow.MerchantChannels, outgoing transfer/automatic-settlement executors and generic source allowlists unchanged. Existing refund/notification/account identity checks remain in force.
- Read the current effective group, membership expiry and enabled/payment status; reject closed groups/payment types and nonmatching dedicated routes. Keep historical receipts/accounts unchanged.
- Live repair changes only the newly supported default-collection configuration to `[1,2]`, after backing up the previous environment and comparing every existing key. Merchants10000/10003 then inherit dynamically; do not rewrite balances, orders, existing explicit grants or group fees.

## Task 1: Explicit startup configuration

**Files:** `internal/channelconfig/default_payment_channels236.go`, its tests, `cmd/api/configured.go`, `internal/server/server.go`, `.env.example` and payment-configuration documentation.

**Interface:** `func (p ParentStartupConfiguration) DefaultPaymentChannels(raw string) ([]int64,error)`. Empty configuration disables fallback. A configured value is a bounded JSON list of at most64 unique positive integer IDs registered in `p.origins`; reject unknown IDs, null, strings, floats, duplicate keys/values, trailing input and excessive input. Return sorted IDs.

- [x] Write and run failing parser tests, then implement and rerun with no PG or network.
- [x] Decode after startup origins are validated, assign `Server.DefaultPaymentChannels`, and leave `MerchantChannels` unchanged. Invalid configuration stops startup.
- [x] Document that the public default pool authorizes new collection only, effective groups still select/disable its channels, and explicit empty entries disable inheritance.

## Task 2: Collection-runtime integration

**Files:** `internal/server/default_payment_channels236.go`, its integration tests, `parent_channel_runtime.go`, `dashboard.go`, `cashier.go` and any confirmed direct collection-only projection that bypasses this view.

**Interface:** A private Server helper returns a request-local merchant admission map and matching current operator predicate. For an explicit merchant entry, preserve the original map. For a missing entry, intersect configured default sources with current merchant/group eligibility and loaded source identities; preserve original aliases' sealed-grant checks.

```go
// Root data is immutable; all fallback changes belong to this request only.
allowed := append([]int64(nil), root.DefaultPaymentChannels...)
view := root.executionView()
view.MerchantChannels = cloneOneMerchantAdmission(root.MerchantChannels, mid, allowed)
```

The implementation must apply the current group validation before constructing that view; the helper above describes ownership, not permission validation by itself.

- [x] Add real HTTP/PG regressions for a merchant absent from the static map: dashboard channels, payment routing and creation using a local provider, with zero upstream financial calls.
- [x] Add regressions for explicit empty admission, disabled/pending merchant, disabled group/type, mismatched dedicated channel, missing defaults, unknown/private source, membership expiry and private alias grants.
- [x] Verify root maps and original operation snapshots remain unchanged; default payment admission does not expand transfer or automatic-settlement admission.
- [x] Run the affected parent/channel-routing, group-policy, cashier, merchant-creation/dashboard and funds-boundary suites; compile all production packages.

## Task 3: Review, publication and live repair

**Files:** scoped verification documentation and private release236 artifacts.

- [x] Independently review collection-only propagation, original-source restoration, explicit-empty semantics, configuration parsing and live change bounds.
- [x] Validate an actual locally created merchant in the browser against the final API and production frontend, with simulation only, using existing infrastructure.
- [ ] Commit/push the verified source and build the release from the clean published commit. Preserve existing schema and production configuration while switching binaries.
- [ ] Back up the live environment; compare current version/source/configuration; set only `GOEPAY_DEFAULT_PAYMENT_CHANNELS=[1,2]`; restart and verify both previously unlisted merchants inherit while10001/10002 keep their explicit admissions. Preserve all other flags/credentials; no production transaction or balance/order write.
- [ ] Record final service/build identities, unchanged schema, read-only channel resolution, 33 reconciliation counters, public asset integrity and browser proof. Report actual test scope and any remaining limits.
