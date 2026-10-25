# Standalone OAuth identity lookup

## Scope and trust boundary

Obtain an application-scoped WeChat OpenID or Alipay user identifier through existing WebOAuthProvider adapters. This workflow never creates an order, submits a payment, credits a balance, or restores a console session from callback parameters. Alipay identifiers are labelled “支付宝用户标识”; no account type is inferred from prefixes. AppAuthToken, service-provider/submerchant, ecommerce, mini-program and unrelated plugins are unsupported here.

An original, durable console session starts the request. Merchant sessions select only themselves; original operator/super administrators may select an enabled merchant. Delegated sessions are rejected. Every store operation verifies the real console_sessions row, current console_users identity/auth_version/enabled/admin class, expiry, nondelegation, and target merchant enabled. The request stores only the initiating token SHA-256 digest, never its plaintext. Account/session revocation or version change prevents callback exchange, result storage, status reading and reveal.

## HTTP contract

All responses use Cache-Control: no-store and Referrer-Policy: no-referrer. Console JSON uses the existing {code,message,data} envelope and strict JSON decoding. State-changing console operations require X-CSRF-Token and the original console cookie. No arbitrary redirect URI or subject ID is accepted.

| Endpoint | Request | Success data |
|---|---|---|
| POST /console/v1/{admin,merchant}/identity-lookups | channel_id string, merchant_id string (merchant may omit) | request_id, merchant_id, channel_id, provider, app_id, scope, status=pending, expires_at, authorization_url |
| GET /console/v1/{role}/identity-lookups/capabilities | only merchant_id query (required for admin, optional/self for merchant) | merchant_id, available, reason, channels [{channel_id,provider,app_id}]; no external calls |
| GET /console/v1/{role}/identity-lookups/{request_id} | no query | same metadata/status, no subject or authorization token |
| POST /console/v1/{role}/identity-lookups/{request_id}/reveal | empty JSON object | subject_id, label, provider, app_id; succeeds once |
| GET /identity/authorize/{random_token} | no query | bind a secure HttpOnly SameSite=Lax __Host-goepay_identity cookie and redirect to official provider authorization URL |
| GET /identity/callback/{provider}/{channel_id}/{request_id}/ | state plus provider-specific code; bound app/scope where supplied | static completion text; no identity in URL/body |

The first authorize GET binds exactly one browser and one independent purpose=identity-lookup state. Reopening the token cannot bind another browser or mint another state. Callback query parsing rejects malformed encoding, duplicate keys, unknown fields, mismatched provider/app/scope and invalid codes. Alipay requires app_id and auth_code, WeChat requires code and permits matching app_id/scope. Callback requires exactly one well-formed identity browser cookie. Console GET does not call a provider. Public token is sufficient only to start browser authorization, never to poll/reveal.

Ready checks require production-mode HTTPS callback origin, AllowLive, no Sandbox/LocalSimulation, configured WebOAuthProvider, the selected merchant's channel whitelist, matching provider/ApplicationID with its loaded payment adapter, and an enabled local channel record when one exists. OAuth scope (`snsapi_base`/`auth_base`) is distinct from provider account scope. The trusted provider.TransferAccountScope value is stored only as an identity-account-domain HMAC, never in URLs/JSON; readiness rechecks it, so changing a WeChat MerchantID under the same AppID rejects the old request. Checks run before authorization, before exchange, after exchange and before status/reveal. Only existing standard web-OAuth configurations are supported; no input-controlled account scope or redirect.

## State and storage

Migration 47 introduces independent identity_lookup_requests and identity_lookup_states without order foreign keys. Request lifetime is at most five minutes and never exceeds initiating session lifetime. Public token, state and browser cookie persist only as purpose-separated HMAC hashes. SubjectID persists only as AES-GCM ciphertext, bound to request/provider/app/merchant as associated data. Status pending -> processing -> verified/failed, verified -> revealed, is terminal except one-way reveal. Immutable binding guards prevent rebinding sessions, merchants, providers, apps, purpose, state or expiry. Limit each session to five live requests.

Before any external exchange, one transaction consumes the independent state, changes pending to processing and claims the provider/app/code HMAC in oauth_authorization_code_claims domain=web. A valid but already claimed code burns the state and leaves failed; no exchange occurs. Provider errors or mismatched identities leave failed with a generic error and cannot retry state/code. The existing payment web consumer also inserts this global claim. Migration 47 backfills old oauth_code_claims hashes using domain web or mini and retains their existing HMAC algorithm; historical consumed codes remain protected without plaintext. Mini stays in a separate domain and keeps the older stricter legacy constraint.

StoreIdentityLookupResult rechecks the real session and deadline after exchange. It never accepts results for a no-longer-processing request. Reveal locks the request and session, verifies ownership/role/version/current policy, decrypts once and commits revealed before returning plaintext. Concurrent reveal has one winner. DB audit includes only actor/merchant/channel/request metadata and transitions, never code, browser token, subject, OAuth access token or ciphertext plaintext. Transport/application logs must not log callback query strings or reveal bodies.

## Integration ownership

This work adds internal/payment/identity_lookup*.go, internal/server/identity_lookup*.go, migration 000047_identity_lookup.up.sql, focused tests, and a small web OAuth code claim hook. Migrations 1..45 and bank/transfer/ledger/provider-flow files are untouched.

Root wiring: call s.IdentityLookupPublic(w,r) early in Server.Handler and return when true. After existing console session, role/admin class, CSRF and delegated-audit middleware, call s.IdentityLookupConsole(w,r,role,session,parts,c.Value) and return when true. No extra Server fields are needed; existing Store, Pool, Sessions, ProviderFlow, OAuthProviders and MerchantChannels supply dependencies. Root owns shared routing and web UI. UI must keep results in volatile component memory only, require explicit reveal, and not save identities in storage or URLs.

## Implementation plan (TDD, execute in this task)

Goal: complete both real web adapters through local synthetic HTTPS upstreams and random PostgreSQL schemas.
Architecture: persistence enforces identity/session/state invariants; HTTP layer enforces fixed routes/provider configuration and never invokes payment submit.
Tech stack: existing Go/pgx/PostgreSQL/AES-GCM/WebOAuthProvider.

- [ ] Write failing store tests for session-bound creation, first-browser state, one-time result, revocation, cross-purpose code replay and atomic failure. Run only IdentityLookup test names to observe missing implementation.
- [ ] Add migration 47 plus focused store APIs and web global-code-claim hook; rerun store tests and existing web/mini state tests.
- [ ] Write HTTP contract tests that exercise original persistent console sessions, strict callbacks and real WeChat/Alipay adapters over local TLS fake upstreams; observe failing missing handlers.
- [ ] Implement exported HTTP hooks and fixed readiness/authorization checks. No shared route mutation. Test HTTP hooks directly until root integrates routing.
- [ ] Exercise malformed/duplicate query, cross-browser/merchant/session/app, replay, disabled configuration, exchange failure and slow-exchange revocation. Assert no orders/provider_attempts/journals and no plaintext persisted.
- [ ] Format changed Go files, run required scoped tests using local DSN and task-local Go cache/temp; inspect the final file scope and report exact root wiring, limitations and verification.

Test command environment: GOEPAY_TEST_DATABASE_URL=postgres://goepay:goepay_local_test@127.0.0.1:55432/goepay?sslmode=disable, GOTMPDIR=E:/GOEPAY/.tools/identity-lookup-tmp, GOCACHE=E:/GOEPAY/.tools/identity-lookup-cache, wrapper D:/GoLang/sdk/go1.21.3/bin/go.exe. No production hosts or secrets are loaded; upstream transport only permits official OAuth hosts and rewrites them to httptest TLS servers.
