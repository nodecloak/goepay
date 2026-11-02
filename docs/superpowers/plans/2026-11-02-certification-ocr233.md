# Real-name certification and OCR implementation plan

> **For agentic workers:** Execute the tasks below with independent provider, domain/API and UI ownership. Read this plan and the local parity references before changing code. Check off steps only after retaining their actual results.

**Goal:** Implement the built-in real-name certification and OCR flows shown by the original console, including protected credentials, merchant certification and success-only fees.

**Architecture:** A dedicated `internal/kyc233` domain owns encrypted configuration, merchant-bound durable attempts and certification records. Provider adapters implement a small typed interface without database access; HTTP handlers enforce own-session authorization, explicit requests and response redaction. OCR consumes bounded uploaded image bytes, returns normalized fields for review and never certifies a merchant by itself.

**Tech Stack:** Go 1.27, PostgreSQL/pgx transactions, AES-GCM, existing payment Alipay signing/client code, Vue 3/TypeScript and Playwright.

**Spec:** User request dated 2026-11-02; local `epay_pro/public/static/assets/index-D_Mjs5yy.js`, `epay_pro/app/lib/certify`, `epay_pro/app/lib/ocr`; read-only original console settings. Store the resulting parity inventory and provider references with verification evidence.

## Global Constraints

- Project directory is `E:/GOEPAY`. Keep `epay_pro` and `epay_release` local and ignored.
- Reuse existing synthetic PostgreSQL container `goepay-full195-windows-b378c4a3` on loopback port 55101. Do not create containers, networks or volumes.
- Preserve the current preview and user browser drafts. Test only GUID-owned synthetic databases and clean them after checking zero active connections.
- No production financial transactions, identity submissions, real document uploads or billable provider tests. Use deterministic local provider mocks.
- Keep strict TLS verification, fixed provider endpoints, bounded responses and no cross-host redirects. Do not copy the original HTTP/TLS bypasses.
- Never return plaintext credentials or stored identity documents in configuration, audit, errors or logs. No browser persistent storage for identity data or credentials.
- Certification callbacks are navigation signals only: success requires a verified server-to-provider response. Failed, unknown, cancelled and duplicate attempts must not charge fees.
- The missing mode-6 extension remains unavailable; do not substitute the older release's unrelated mode-6 implementation.

## Task 1: Provider contract and cloud adapters

**Files:** Create `internal/kyc233/types.go` (domain owner publishes contract first), `internal/adapters/kyc233/*_test.go`, `internal/adapters/kyc233/*.go`; extend focused Alipay adapter methods when needed.

**Interfaces:** `kyc233.Provider` exposes initialization, query and OCR recognition using an immutable configuration snapshot, explicit identity request and opaque provider token. Its constructor performs no network I/O. Publish the exact Go structs to the domain and UI owners before implementing consumers.

- [x] Define the shared typed contract with explicit pending/passed/rejected outcomes, normalized OCR field maps and sanitized errors; initialize only from server-owned configuration.
- [x] Write failing local HTTP fixtures for Alipay signed initialization/query, Tencent TC3, Aliyun RPC, phone/company checks and both OCR providers; include malformed/oversized responses, TLS/redirect refusal and credential/PII error redaction.
- [x] Implement modes 1–5 and corporate pre-check only where documented HTTPS protocols can be verified. Preserve all six OCR document enums and original normalized output names.
- [x] Run adapter tests with `go test ./internal/adapters/kyc233 ./internal/adapters/payment -run 'KYC233|OCR233'`; record actual results and limitations.

## Task 2: Encrypted configuration, certification lifecycle and HTTP

**Files:** Create `internal/kyc233/config.go`, `service.go`, focused tests, migration(s) starting at 192 under the existing migration directory; create `internal/server/kyc233.go` and tests, update the console dispatch and admin permission routing.

**Interfaces:** Read/update settings at `/console/v1/admin/settings/certification` and `/console/v1/admin/settings/ocr`; merchant endpoints `/console/v1/merchant/account/certificate/{info,submit,geturl,query,reset}`. OCR is an explicit multipart POST scoped to an authenticated original session, with `document_type` and `image`. Publish exact JSON schemas before UI implementation.

- [x] Add failing database/HTTP tests proving disabled defaults, complete explicit schemas, own-super-admin credential editing, CSRF, revocation and delegated-session denial.
- [x] Implement AES-GCM credential and identity storage with revision CAS, immutable attempt snapshots, expiry, one active attempt per merchant and one-use initialization claims. Unknown initialization outcomes must not be automatically resubmitted.
- [x] Implement personal and corporate submissions, supported document types, bound-phone three-factor checks, resumable status, reset and authenticated result queries. Never trust browser result fields or accept arbitrary redirect destinations.
- [x] Commit certification and success-only ledger fee together, deduplicated by attempt ID. Test duplicate/concurrent queries, insufficient balance, cancelled/expired attempts, configuration changes and cross-merchant access.
- [x] Enforce the configured requirement for certification at payment acceptance without changing default payment behavior; prove disabled settings preserve existing flows.
- [x] Validate upload size, decoded dimensions and image type before claiming a provider operation; do not store source images or permit remote URL/path inputs. Add local rate/durable one-use controls to prevent automatic repeat billable calls.
- [x] Run focused owned-database tests and preserve raw logs, reconcile output and cleanup receipts.

## Task 3: Original-console fields and complete merchant interaction

**Files:** Create `web/src/kyc233.ts`, `web/src/components/CertificationSettings233.vue`, `OCRSettings233.vue`, `MerchantCertification233.vue` and relevant tests. Update `SettingsPage.vue`, `App.vue`, `deferred.ts` and parity docs.

**Interfaces:** Consume Task 2's published JSON schema through the existing authenticated API helpers. Certification preserves `certtype`, `certcardtype`, `certname`, `certno`, `certcorpname`, `certcorpno`, `certmethod_text`, `certtime` and masked information.

- [x] Add browser tests for each conditional provider field, credential keep/replace/clear, fee/force/company options, disabled/unconfigured errors and draft/unknown-result handling.
- [x] Add the merchant fill → scan/open → result steps, explicit consent and fee display, bound-phone display, six supported Alipay document types, company upgrade and reset confirmation. Do not persist identity data in browser storage or auto-submit mutations.
- [x] Add six-document OCR upload and result review with explicit submission. OCR results prefill only after review and never mark certification complete.
- [x] Test failed input, failed provider response, cancellation, timeout, duplicate clicks, stale responses, delegated sessions and narrow screens. Use local synthetic images only.
- [x] Run typecheck, unit tests, build and focused browser tests; retain evidence.

## Task 4: Review, regression and deployment

**Files:** Add `docs/verification/certification-ocr233-2026-11-02.md`; create new ignored `.tools/kyc233-*` and `.tools/deploy233` evidence without altering previous frozen receipts.

- [x] Request independent code review of authorization, PII handling, provider proof, replay/fee behavior and UI/API schema consistency; fix material findings and rerun affected checks.
- [x] Run all Go tests, frontend units/build/browser tests and local backend business lifecycle tests against reused infrastructure. Count opt-in cases only when they actually execute. Preserve the legacy scheduler's initial clock-boundary failure and its one-shot same-period retest separately, as recorded in the verification report.
- [ ] Freeze tested source and assets, verify a clean Git archive, commit/push and prepare a migration-aware deployment from production version 232/schema-count 156 with rollback checks for new identity/configuration data.
- [ ] Deploy only after verification succeeds; preserve all existing production credentials and sending flags. Confirm public asset hashes, health, schema, read-only reconciliation and authenticated settings rendering without submitting production configuration or identity requests.
- [ ] Report tested scope, deployment version and the explicit absence of live provider identity acceptance; real operation requires provider onboarding and credentials configured by the owner.
