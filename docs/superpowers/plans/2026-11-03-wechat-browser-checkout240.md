# WeChat Browser Checkout and QR Centering Implementation Plan

> **For agentic workers:** Execute the test-first tasks below, retain actual failures, and dispatch an independent reviewer before publication.

**Goal:** Complete desktop QR and mobile continuation for existing WeChat orders, and center all checkout QR displays without changing original payment operations.

**Architecture:** Present saved native/H5/JSAPI/APP material according to the current browser. Desktop non-native methods QR-link the signed original checkout. H5 mobile navigates the risk-filtered original URL with its required origin referrer; JSAPI waits for WeixinJSBridgeReady and never treats a bridge callback as paid. Native remains scan-only rather than creating a different product. OAuth desktop shows the original-checkout QR before issuing browser-bound authorization state. Alipay mode4 iframe follows the local reference's230-square viewport.

**Tech Stack:** Existing Go HTTP server, PostgreSQL, QR encoder and local Chromium/Playwright; generated credentials and local TLS peers only.

**Spec:** User2026-11-03: desktop QR, mobile WeChat payment, preserve original order/channel, center QR, local simulation then deployment, no real money or new containers. Local reference: `epay_release/includes/pages/alipay_qrcodepc.php`, `epay_release/plugins/wxpay/wxpay_plugin.php`, `epay_release/includes/pages/wxpay_jspay.php`. Official [Native flow](https://pay.wechatpay.cn/doc/v3/merchant/4012791878) requires scanning; [H5 guidance](https://typ.wecard.tencent.com/developers/product/c2b/h5/faq) requires a referrer and external browser.

## Global Constraints

- E:/GOEPAY; retain preview and ignored local PHP references.
- Existing synthetic PostgreSQL only, owned checkout240_GUID databases; zero active connections before drop, no FORCE or session termination.
- Preserve original account/channel/method/order/amount/material and attempt count. Never Create/Query from presentation or bypass frozen scan-region rules.
- No automatic native→H5 conversion, invented APP launching, unsafe plugin auto-execution, premature paid result or callback retry.
- Production schema158 and all configuration hashes unchanged; back up, quiesce both writers, reconcile33 counters, switch binaries, verify read-only. No production checkout submission or new containers.

## Task1: HTTP contract, RED then implementation

Files: create `internal/server/wechat_browser_checkout240.go`, `internal/server/wechat_browser_checkout240_test.go`; modify `provider_http.go`, `oauth_web.go`, `alipay_browser_checkout239.go`.

- [x] Write table tests exercising actual signed merchant create→desktop/mobile checkout for native/H5/JSAPI/APP; detect missing desktop continuation QR, missing mobile launch, referrer and bridge readiness handling.
- [x] Run owned local `run-tests.ps1 -Run http-red-v1 -Pattern '^TestWechatBrowser240'` and preserve genuine missing-behavior failures.
- [x] Add method-bound render/QR helpers, centered responsive QR image, same authenticated URL and local-status poll. H5 in WeChat shows external-browser guidance. Native never auto-navigates its scan payload.
- [x] Add guarded pre-attempt OAuth desktop QR; mobile retains original state/cookie/identity flow. Test no desktop OAuth-state issuance.
- [x] Test closed/unknown/paid/token tamper states and immutable saved operation; run HTTP/risk/history/OAuth regressions.

## Task2: Real browser and centering

Files: create `internal/server/wechat_browser240_actual_test.go`, `web/e2e/wechat-browser240-local.mjs`; update existing Alipay239 browser fixtures.

- [x] Use real SDK/local signed gateway and callbacks; browser network allowlist permits local synthetic peers only. Cover desktop/mobile native, H5, JSAPI readiness/cancel/manual retry/confirmed return.
- [x] Replace synthetic Alipay320-centered layout with reference230-square QR-only layout; preserve signature/303/callback checks. Verify RED against old viewport, then center without cropping.
- [x] Assert QR midpoint equals card midpoint, full quiet zones, no overflow at1280/375px. Inspect actual final screenshots.

## Task3: Frozen regression, review and release

- [x] Freeze current Go/web inputs; run targeted HTTP/history/OAuth/WeChat/Alipay/template/cmd tests and both browser suites. Dist113 assets unchanged; explicitly reuse old SPA suite evidence only for unchanged compiled application.
- [x] Independently review product, fixture evidence and release tool hashes; resolve actionable findings.
- [ ] Commit/push main, build API/CLI/worker from clean source, run guarded240 backup/switch/read-only verification, preserve original material/configuration.
- [ ] Record actual final deployment receipt and report exact tested scope, skips and real-app limitations.
