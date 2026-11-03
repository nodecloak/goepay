# Alipay Browser Checkout Implementation Plan

> **For agentic workers:** Implement the adapter and release preparation independently, then request an independent review before publication. Preserve actual failing tests, final-source tests and deployment receipts.

**Goal:** Show Alipay's official embedded QR cashier on desktop and automatically continue to Alipay on mobile, preserving each original order, account, channel and payment method.

**Architecture:** Verify the sealed original page/wap form using its original restored adapter. Produce a transient re-signed presentation copy: desktop page adds only `qr_pay_mode=4` and `qrcode_width=230`; mobile retains its original method and business fields. Render the desktop official cashier in a constrained iframe and auto-submit the verified form once; keep a manual continuation. Desktop wap uses a QR link to the same authenticated order's browser continuation. Poll only local paid status for automatic return; never convert page/wap into precreate/native or create another attempt.

**Tech Stack:** Existing Go HTTP server, Alipay RSA2 adapter, PostgreSQL, Go QR encoder and local Playwright. No new dependency or container.

**Spec:** User instruction2026-11-03: reference local epay_pro semantics; desktop QR, mobile continuation; preserve order/channel; local simulation tests before deployment; no real funds or new containers. Local available unencrypted reference `epay_release/plugins/alipay/alipay_plugin.php` uses embedded page QR mode4 in qrcodepc/submitpc. Production diagnostic found the screenshot order is pending, method page, material html; do not click or send this order.

## Global Constraints

- Work in E:/GOEPAY; keep the current preview and reference PHP local only.
- Reuse PostgreSQL container `goepay-full195-windows-b378c4a3`; use owned `checkout239_<GUID>` databases and drop only after zero connections. No FORCE or session termination.
- All payment/gateway/browser interactions in tests use generated keys and local peers. Production acceptance uses only read-only state/health/asset checks, never opens or submits a live checkout.
- No database migration or production configuration change: schema158 and all migration hashes remain identical. Preserve collection defaults and explicit admissions from236.
- Keep original form bytes, attempts, request hash, money, merchant, seller, expiry, notify URL and payment method unchanged in storage. Desktop QR fields affect only a verified in-memory display copy.
- Only explicitly supported original Alipay adapters opt into automatic browser presentation. Unsupported/ported HTML retains its existing path.

## Task1: Verified browser form capability

**Files:** `internal/adapters/payment/alipay_browser_checkout239.go`, its tests, `internal/channelconfig/factory.go`, forwarding tests.

**Interface:** `BrowserCheckoutPresentation{HTML,Method,GatewayOrigin string}` and `PrepareBrowserCheckout(material,tradeNo,returnURL string,desktop bool) (BrowserCheckoutPresentation,error)`.

- [x] Independently verify real RED, then implement strict signature/app/gateway/order verification and presentation-only RSA2 re-signing.
- [x] Test old UTF8 forms, all original business fields, desktop page mode4, mobile page/wap preservation, unsupported forwarding and immutable input.

## Task2: Cashier HTTP and browser behavior

**Files:** `internal/server/alipay_browser_checkout239.go`, `provider_http.go`, focused HTTP/browser tests and local browser fixture.

- [x] Verify an actual failing HTTP test for the missing desktop iframe/mobile submission.
- [x] Restore the original account and bind the returned method to the original attempt; enforce pending/paid/token checks before emitting any form or QR.
- [x] Add desktop iframe, mobile automatic submission, manual fallback, authenticated same-order wap QR and local-only status polling; nonce scripts and constrained frame/form origins.
- [x] Test desktop/mobile Chromium against a local synthetic Alipay cashier, RSA2 request verification, local completion and no duplicate attempts. HTTP tests cover tablet user agents, expired/tampered tokens, paid orders and preserved history/account after policy changes. This is not real device/App acceptance.

## Task3: Review and publication

**Files:** verification documentation, private `.tools/alipay-checkout239` and `.tools/deploy239`.

- [x] Freeze current Go inputs, reuse unchanged compiled frontend only after hash verification, and run affected checkout/core-risk/routing/adapter regressions.
- [x] Independent review verifies original-operation restoration, signed-field preservation, HTML/CSP/iframe boundaries and actual test evidence; resolve material findings.
- [ ] Build all production packages from the committed, pushed source.
- [ ] Commit/push, build from the clean commit, back up and deploy239 over236 with158 unchanged migrations and every configuration hash unchanged.
- [ ] Verify services, binaries,113 public assets,33 reconciliation counters and unchanged original payment records through read-only checks. Keep failed/skipped evidence separate and state exact test scope.

Local acceptance: 227 passing test roots, two explicit PHP interop opt-in skips; four synthetic Chromium scenarios at1280/375px. Full QR bounds and all four final screenshots independently reviewed. Existing1010 frontend inputs and113 compiled assets are unchanged; prior full SPA suite evidence is reused explicitly, not reported as a239 rerun. See `docs/verification/alipay-browser-checkout239-2026-11-03.md`. Publication checkboxes remain pending in this pre-deployment commit; actual release receipts are recorded outside the repository.
