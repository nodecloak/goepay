# Pay-roll editor implementation plan

> Execute inline using test-driven-development; no commit, deployment, or production writes.

**Goal:** Create a named inert polling group with three fields, then configure real channels independently without overwriting routing policy.

**Architecture:** An admin-only draft endpoint stores a disabled, non-default global group. A separate channel endpoint uses a full-record revision under a row lock, preserving merchant scope, default use, group limits, and surviving channel rules. Vue components own these dedicated requests, including unknown-outcome recovery; ResourcePage integration remains with the root agent.

**Tech stack:** Go/pgx/PostgreSQL, Vue 3/TypeScript/Vitest.

**Constraints:** No production use; reuse the existing serial PostgreSQL runner; no browser runner until root releases it. Do not edit service.go or ResourcePage.vue. Configuring channels never enables routing or changes default use. Existing enabled groups require explicit confirmation before changing channels.

1. Add behavior tests for the dedicated draft endpoint and channel CAS: safe stored constants/audit; hidden activation rejected; empty draft accepted only while inert; stale full-body snapshot rejected; preserved limits/rules/merchant/default; enabled-change confirmation; cross-type/unknown channels refused. Run red before implementation.
2. Implement pay_roll_draft.go, pay_roll_channels.go and the smallest ParsePayRoll empty-draft exception. Expose ServePayRollEditor for root's existing authenticated/CSRF dispatch hook. Verify targeted backend tests and old pay-roll parsing tests.
3. Add component tests before implementation: three-field create payload and unknown-create lock; real channel table/filter/order/weights; CAS payload and preserved advanced fields; enabled confirmation; unknown-save blocks further writes until explicit readback; context epoch guards.
4. Implement PayRollCreateForm.vue, PayRollChannelEditor.vue and typed pay-roll parsing helpers. Run targeted unit tests/typecheck. Root integrates components; browser verification follows root's exclusive static-server regression.

**Validation evidence:** Record red/green logs under .tools/production-parity-20261101; do not claim browser verification before it runs.
