# Notification completion 231

Goal: Complete the original notification settings, including business SMS,
administrator bot credentials and deliberate QQ binding, with real consumers.

Architecture: Reuse existing protected notice events and permanent delivery
claims. Resolve configuration in the source/claim transaction. Store bot secrets
in an encrypted, revisioned vault, never generic settings. Binding requires a
short-lived challenge, a current original administrator session and a signed QQ
callback. Rotation revokes binding. Existing external penalty plugins stay
deferred; do not fabricate their events.

Tech stack: Go, PostgreSQL, Vue 3, Playwright, existing Docker test pool.
Spec: Original local Pro notice page and MsgNotice/Qqbot source; authenticated
reference UI at p.ma3fu.com. Production inspection is read-only.

Constraints: No new containers, no real funds, no production test notification
or binding. Preserve preview. Isolate test databases and retain receipts.

1. Add regression tests for independent SMS templates, safe verified recipients,
   missing/rotated credentials, callback signature/replay/session expiry and CAS.
2. Add internal/businesssms231, internal/noticeconfig231 and migrations 188–191;
   strict actor/CSRF checks, encryption, audit proofs and transaction resolution.
3. Connect merchant/admin workers, trusted first complaint/limit/confirmation
   sources, and freeze effective authorization at event birth. No retroactive
   send or fallback after explicit disable/clear.
4. Complete SettingsPage/SmsSettings and bot editor interactions: explicit
   keep/replace/clear, masked state, binding window/status/unbind, draft guards,
   unknown-result readback and no automatic mutation retries.
5. Run unit/integration/browser tests against local mocks and existing pool;
   obtain independent review and fix findings. Record evidence with actual
   results, preserving failed/interrupted runs.
6. Commit/push clean source, build exact tested artifacts, deploy with backup,
   migration and rollback guards. Verify production assets/read-only state;
   summarize only after all checks finish.
