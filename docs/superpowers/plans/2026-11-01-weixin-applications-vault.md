# Weixin Application Vault Implementation Plan

> **For agentic workers:** Execute this plan inline using test-driven development and the existing independent reviewer. Parent-owned router/UI hooks remain coordinated; no new agents are needed.

**Goal:** Make the official-account/mini-program editor store APPSECRET safely and supply the actual payment/identity application catalog.

**Architecture:** Keep legacy metadata untrusted and imported profiles read-only. Store new managed applications as authenticated, versioned encrypted payloads; public views never contain credentials. Resolve managed profiles into immutable request-owned factories and identity views, intersecting explicit merchant scope with the original source allowlist and preserving historical sealed payment sources.

**Tech Stack:** Go, PostgreSQL, existing AEAD/HMAC primitives and original-super-session checks, Vue/TypeScript and existing ApiClient.

**Spec:** Root's 2026-11-01 instructions in the production console parity task; existing source contracts in `internal/channelconfig/identity_profiles.go`, `internal/channelconfig/parent_channel_bindings.go`, `internal/payment/parent_channel_actor.go`, and `internal/server/parent_channel_runtime.go`.

## Global Constraints

- Reuse `goepay-full195-windows-b378c4a3` at `127.0.0.1:55101`; one Go/PG test runner, isolated own databases and zero-connection cleanup.
- Preserve the frozen preview. No production access, real transactions, new containers, commit or push.
- Reserve only migration `000186_weixin_applications_vault`; maximum existing migration is 000185.
- Never put APPSECRET in `backoffice_records`, audit payloads, public JSON, logs or browser persistence.
- Preserve imported profile IDs, including disabled IDs. Never shadow an imported entry with a managed application.
- Preserve current source capabilities, explicit merchant allowlists, global execution/OAuth/identity gates, mini-program paths and historical sealed sources.

### Task 1: Establish failing HTTP behavior and public API

**Files:** New `internal/server/weixin_applications_http_test.go`; root hook in console dispatch; new `internal/server/weixin_applications_http.go`.

**Interfaces:**

```go
func (s *Server) weixinApplicationConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool
```

Routes under `/console/v1/admin/weixin-applications`: GET list; GET capabilities; POST create; PATCH `/{id}`; POST `/{id}/archive`; GET `/requests/{request_id}`. IDs, revisions and merchant IDs are canonical decimal strings. Request IDs are fresh 32-byte lowercase hexadecimal values. Mutations accept exact JSON objects and require the original durable super administrator session and CSRF. Errors never echo submitted bodies.

- [ ] Write an HTTP test that creates an application through the real handler, expects 201, confirms the secret is absent from response, records and audit, and reads the result back by request ID.
- [ ] Run the test against the current handler and preserve its expected 404 failure.
- [ ] Add cases for stale revision, unknown extra fields, duplicate keys, non-super/delegated/expired sessions, wrong CSRF, and secret redaction.

```go
response := appRequest("POST", "/weixin-applications", `{"request_id":"...","kind":"official_account","name":"Synthetic","app_id":"wxsynthetic","appsecret":"synthetic-only","enabled":false,"source_channel_id":"0","merchant_ids":[],"path":"","expected_source_fingerprint":""}`)
if response.Code != http.StatusCreated { t.Fatal(response.Code) }
if strings.Contains(response.Body.String(), "synthetic-only") { t.Fatal("secret disclosure") }
```

### Task 2: Versioned encrypted storage and confirmed mutation receipts

**Files:** New forward migration `db/migrations/000186_weixin_applications_vault.up.sql` (the repository executes every embedded SQL file and does not support down files); `internal/payment/weixin_application_types.go`, `weixin_application_vault.go`, `weixin_application_write.go`, tests.

**Interfaces:** Public `WeixinApplicationView` contains only identity, name, business kind, AppID, revision/fingerprint, configured-secret flag, explicit source, merchants, path, enabled/draft/archive state. Private `WeixinApplicationSpec` carries APPSECRET only inside trusted Go calls. `WeixinApplicationScope` supplies actual source fingerprints, imported reserved IDs and existing merchant grants. Store create/update/archive/readback methods take `ParentChannelActor` and recheck it after final waits.

- [ ] Create separate heads, immutable encrypted versions and immutable mutation-receipt tables. Key absence fails closed.
- [ ] Bind AEAD AAD to purpose, ID, revision, source and kind; authenticate head/version/receipt facts with keyed proofs.
- [ ] New APPSECRET is required. Blank/omitted edit secret retains the current credential; public GET exposes only `secret_configured`.
- [ ] CAS checks current revision and fingerprint; each accepted mutation gets exactly one new revision/audit/receipt. The receipt binds actor, request ID and keyed complete-input hash. Reusing a request ID for different content rejects.
- [ ] Allocate numeric IDs while excluding every reserved import ID; reject future managed/import collisions explicitly. Never infer execution from legacy metadata/AppID equality.
- [ ] Drafts may retain encrypted credentials without source/merchants/path, but no draft enters executable resolution. Configured entries lock current enabled merchants and validate all explicit grants.
- [ ] Preserve tests showing rollback on final session/CSRF expiry, stale CAS, absent key, ciphertext/proof tampering and no plaintext persistence.

```go
// Durable view intentionally excludes the write-only credential.
type WeixinApplicationView struct {
    ID, Revision int64
    Name, Kind, AppID, Fingerprint string
    SecretConfigured, Enabled, Draft, Archived bool
    SourceChannelID int64
    MerchantIDs []int64
    Path string
}
```

### Task 3: Actual request-owned payment and identity consumption

**Files:** New `internal/channelconfig/managed_weixin_profiles.go`; `internal/server/weixin_applications_runtime.go`; narrow integrations in parent-channel catalog/write/runtime and independent identity readers; new synthetic runtime tests.

- [ ] Expose reserved profile IDs and clone factories with an additional validated managed-profile catalog. Clone maps/slices deeply; retain all original ParentBuildOptions closures and execution gates.
- [ ] Resolve authenticated current managed specs into `IdentityProfileSpec` values with exact source, explicit merchant IDs and mini path; call existing `BuildIdentityProfiles` without network I/O.
- [ ] Apply a request-owned factory map to parent options/catalog/new writes/current runtime. Keep original historical `ParentPaymentIntent`/sealed profile restoration authoritative for old orders.
- [ ] Resolve independent identity capability and binding lookups through the same current app source, preserving original identity opt-in and merchant-channel checks.
- [ ] Rotation/disable changes current catalog fingerprint and makes old current bindings stale for new sends; existing unknown orders remain readable/reconcilable through their sealed source.
- [ ] Prove a managed app appears in actual ParentWeixinBindings, its secret reaches a local synthetic OAuth/mini adapter, grants cannot expand, imported IDs cannot be shadowed, and rotation does not mutate shared factories or old sealed sources.

### Task 4: Dedicated application editor and unknown-result recovery

**Files:** New `web/src/weixin-applications.ts`, `web/src/components/WeixinAppsPage.vue`, tests. Root owns `ResourcePage.vue` and `App.vue` integration.

```ts
defineProps<{ client: ApiClient }>();
```

- [ ] Show Pro's kind/name/APPID/APPSECRET first; keep source/allowed merchants/mini path in an association configuration section.
- [ ] Use a write-only password input. Creating requires a secret; editing says blank retains it and never receives the old value.
- [ ] Display imported applications read-only and old metadata as unconfigured drafts; configuration creates a separate authenticated managed entry.
- [ ] On save, use a fresh mutation ID and CAS facts. Clear secret in `finally`, cancel stale loads when identity/session changes, and never store credentials in URL/localStorage/sessionStorage.
- [ ] If the response is unknown, disable resubmission and offer only GET receipt readback. Do not replay the body automatically.
- [ ] Red/green tests cover fields, draft status, permissions, invalid source/path, stale selection, failure clearing and receipt recovery.

### Task 5: Independent review and targeted verification

- [ ] Coordinate each Go run with the existing runner holder. Use `.tools/production-parity-20261101/test-merchant-create-http.ps1` and save red/green receipts.
- [ ] Have account_settings_parity independently review encryption boundaries, scope intersection, runtime deep copies, history semantics, ID collision and unknown-result recovery.
- [ ] Run affected package tests, existing parent binding/history tests and frontend component tests. Root owns final broad checks and deployment decisions.
- [ ] Verify no own test databases remain, frozen preview pointer is unchanged and scoped `git diff --check` is clean. Report concrete evidence and remaining limitations; do not commit.

No commits are planned because the parent explicitly prohibited commits for this delegated work.
