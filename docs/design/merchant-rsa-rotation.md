# Merchant RSA keypair generation and replacement

Implementation plan, 2026-10-24. This increment adds merchant self-service only; it does not change the frozen83 candidate or require a migration.

## Plaintext Pro evidence

`epay_pro/public/static/assets/account-DkHvjhqn.js` separates `account/createRsaPair`, `account/resetKey` (MD5), and `account/editKeyType`. The API-info page in `index-CgFnG600.js` confirms generate/reset, receives `public_key` and `private_key`, and warns that closing prevents retrieving the private key again. Only these static plaintext sources were inspected.

## HTTP contract and algorithm policy

GET `/console/v1/merchant/account/api-info` adds `rsa_fingerprint` (lowercase SHA-256 of domain-separated algorithm plus exact stored public-key text) and `can_rotate_rsa` (strict boolean). No API key is included in the fingerprint. POST `/console/v1/merchant/account/rsa-key/rotate` accepts only `current_password` and `expected_fingerprint`; neither target merchant nor RSA bit count is accepted. Its sole successful response contains `algorithm`, `public_key`, `private_key`, and `fingerprint`.

Generation always uses 2048-bit RSA, PUBLIC KEY / PKIX public PEM and PRIVATE KEY / PKCS8 private PEM, compatible with the existing Go and PHP/OpenSSL signature parsers. For an MD5 merchant the public key is a standby key and the algorithm remains MD5. For an RSA merchant replacement invalidates signatures from its previous RSA key. This endpoint never silently selects an algorithm and is separate from API-key/MD5 rotation.

## Transaction and concurrency design

The durable merchant actor includes the original token, username, merchant ID, auth_version and CSRF value. Every operation locks merchant first, console_user second, original console_session third; rotation takes the merchant UPDATE lock, reads use SHARE. This is consistent with the existing API-key rotation's merchant-before-user order. Password changes only lock users and commit before deleting sessions, so they do not hold a user lock while waiting on a merchant lock. Durable session checks reject missing/revoked/expired sessions, changed credentials/version, disabled merchants/users and any console_session_issuers row, irrespective of caller-supplied identity metadata. CSRF is checked both by the console middleware and against the original durable session.

Preflight validates actor, old fingerprint and current password before generating a key. RSA/Argon work shares the bounded verification semaphore. Generation occurs outside the transaction, then commit rechecks the same actor, password and fingerprint under locks. Exactly one concurrent replacement from the same fingerprint may commit. The update changes only public key and config_revision, preserving API key, algorithm and unrelated merchant fields. A nonsecret account audit is inserted in the same transaction; audit failure rolls the entire operation back. No automatic retry occurs after a failed/uncertain response.

The private key is generated in request memory and returned only after commit. It is never provided to the store, SQL, audit, log, URL or later GET. Responses use no-store/no-referrer. A lost successful response requires an explicit new rotation after refreshing current state; there is no replay or private-key recovery API.

## UI and validation sequence

Keep existing account layout and MD5 action. A separate RSA component has explicit password confirmation, algorithm-specific warning, one-time private textarea and explicit hide/close. Context changes (client, CSRF, capability, fingerprint, route/unmount) clear secrets and abort requests; epochs discard late responses. There is no local/session storage, URL encoding, automatic retry or clipboard write. A failed/unknown POST requires a fresh account-info read before a new explicit attempt.

Actual RED/GREEN tests cover fingerprint domain separation, generated key parsing/signing, strict request shape, wrong password/CSRF/actor, concurrent CAS, disabled/revoked/delegated/version-changed actors, transaction audit rollback, old/new real RSA signatures, private absence from persisted state and later reads, and UI cancellation/context/unmount/late-result behavior. Run narrow PG tests on a new owned database only after a separate server/DDL slot is confirmed; never use deployment PG48. Run component units, type/build and mock browser tests without real upstream requests.

## Implemented verification

Evidence pointer: `.tools/rsa-rotation-evidence-path.txt`. Actual missing-capability HTTP RED was followed by successful concurrent rotation, old/new real API signatures, audit-fault rollback and original account/password regression contracts (13.372s). The store actor matrix covers disabled user/merchant, revoked/expired session, issuer delegation, changed auth_version/password and MD5 standby generation (5.979s). Tests use only the assigned GUID database on dedicated PG55450 through the two-slot wrapper.

The generator's actual local PHP/OpenSSL verification signs in both directions using PKIX/PKCS8 PEM (2.399s, network-none temporary container, input over stdin only). UI context tests additionally reproduce and fix a transient same-client merchant→admin→merchant race; both AccountPage and the RSA component synchronously burn state on area/client/CSRF changes. Final frontend: 267 units, type/build, and six immutable-dist browser mock tests including original MD5/password behavior and 375px layout passed. No real upstream, deployment database or frozen source was modified. Parallel profit test compilation interruptions remain recorded separately; final focused RSA/account runs passed after that working-tree type completed.
