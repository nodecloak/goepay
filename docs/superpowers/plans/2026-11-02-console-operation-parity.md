# Console operation parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 对照参考站，把两后台的新增、编辑、删除、状态切换和关联业务按完整操作验证，修复实际缺陷后部署。

**Architecture:** 参考站只读取菜单、表单、确认和错误反馈，不提交生产数据。Go操作测试使用真实Go服务、现有PostgreSQL池的逐文件独立数据库和本地模拟适配器；浏览器填表提交、刷新再读，并核对实际API持久状态。只读节点验证查询/导出/详情，受保护和外部未接入动作验证明确拒绝。

**Tech Stack:** Go、PostgreSQL、Vue 3、Playwright、Vitest、现有Linux静态发布工具。

**Spec:** 本轮用户要求“再对照 p.ma3fu.com，把管理后台和商户后台的新增、编辑、删除等完整操作流程逐项测一遍……资金操作仍只在本地模拟环境测试……全部通过后部署”。既有范围见 docs/verification/console-node-audit-2026-11-02.md。

## Global Constraints

- 不发起真实资金交易；不在两个生产站点保存、删除、发送消息、重置密钥或改变权限。
- 不创建容器、网络或卷，复用 goepay-full195-windows-b378c4a3 的55101端口。
- 每文件独立GUID数据库；只在零连接后移除本轮自有数据库。保留8187预览及CURRENT.json。
- 不修改已冻结证据目录；本轮证据放在 .tools/ux227-20261102，新发布工具放 .tools/deploy227。
- 已暂缓的外部插件明确记录，不把缺少上游能力的拒绝记作业务成功。账本、原订单与安全保护的删除限制单列。

## Task 1: Reference action matrix

**Files:** Create docs/verification/console-operation-parity-2026-11-02.md; read web/src/menu.ts, web/src/resources.ts and reference browser DOM.

**Interfaces:** Consumes actual sidebar URLs and visible buttons; produces per-node actions, form labels, confirmation steps, persistence checks, result and evidence mapping.

- [x] Read both reference sidebars and the available writable dialogs without submitting; capture labels and action names, never field values or credentials. Record current merchant-role and deferred-plugin boundaries explicitly.
- [x] Map each of the 60 admin and29 merchant Go menu nodes to CRUD, settings/action flow, readonly, protected or external-deferred behavior.
- [x] Check actual write coverage in existing live-node-workflows, live-business-lifecycle and remaining live suites; separate page-open evidence from action-completion evidence.

## Task 2: Real backend operation suites

**Files:** Create web/e2e/local-crud-fixture.ts, web/e2e/live-admin-config-crud.spec.ts, web/e2e/live-merchant-config-crud.spec.ts and web/e2e/live-operations-crud.spec.ts. Existing infrastructure copied to a new .tools/ux227-20261102 directory, never edited in place.

**Interfaces:** Shared helper returns local API context, CSRF, browser cookies and synthetic environment. API and browser share only loopback origin. Writes use real endpoints; request interception may only block off-origin network, not fulfill console responses.

- [x] Implement local guard using readLocalSandboxEnv/localSandboxBase, assert GOEPAY_SANDBOX=true, GOEPAY_PROVIDER_MODE=simulation and dashboard.sandbox=true.
- [x] For admin configurations, exercise payment-method create/edit/toggle, routing draft and channel selection/edit/disable, archive only where supported, invitation generation/delete/clear, managed channel/application drafts, admin-user create/edit/disable/delete, collection styles and local settings save/reload. Check invalid input and cancel produce no writes.
- [x] For merchant configurations, exercise own domain create/edit/pending-review/delete, profile and notification preferences save/reload, settlement recipient version and reserve save/reload, printing preferences, aggregate code create/update/disable. Verify another merchant cannot see or mutate their records.
- [x] For operational nodes, exercise local announcement/template settings, group/routing/merchant flags, manual complaint/onboarding drafts where available, and readonly searches/exports. Reuse complete local payment/callback/refund/settlement/transfer workflows and inspect their outcomes.
- [x] Assert persistence, not just HTTP status. Representative assertions:

```ts
await dialog.getByRole('button', {name:'保存', exact:true}).click();
await expect(dialog).toHaveCount(0);
await page.reload();
await expect(row).toBeVisible();
await row.getByRole('button', {name:'编辑', exact:true}).click();
await expect(dialog.getByLabel('显示名称', {exact:true})).toHaveValue(updatedName);
```

- [x] Every actual failure is retained; distinguish a product defect from a wrong test assumption using visible DOM, response code/body and persisted API readback.

## Task 3: Reproduce and fix confirmed defects

**Files:** Modify only the failing UI/backend components and their focused tests, identified from Task2 evidence. Existing module boundaries are preserved.

**Interfaces:** Browser form payload must match existing versioned API contracts. Financial intent, signature, authentication, grants and unknown-result recovery must retain original semantics.

- [x] Add a focused failing test for each confirmed defect before product changes.
- [x] Fix its source and run the failing test to green; retain error feedback, no automatic replay of unknown writes, draft cancellation and focus behavior.
- [x] Request independent readonly code review before the final full regression; resolve material findings first.

## Task 4: Final regression and deployment

**Files:** Final manifests, receipts and logs under .tools/ux227-20261102; guarded deployment under .tools/deploy227; update the verification document with actual counts and scope.

**Interfaces:** Final source and static asset digests bind test results, Git commit and uploaded archive. Go evidence may be reused only if every Go/embedded/migration input matches; otherwise rerun affected and full required backend checks.

- [x] Run final unit tests, typecheck, build and complete mocked browser suite once source has passed review.
- [x] Run every actual local browser suite with separate owned databases; match every conditional skip to an actual local pass and verify all33 reconciliation checks.
- [x] Verify default preview unchanged, no new containers and all owned databases cleaned after zero connections.
- [ ] Commit/push tested source after credential/path checks; run isolated release guard tests, deploy pinned artifacts, verify health/configuration and public resource digests.
- [ ] Recheck changed live interactions without saving and record accurate completed/protected/deferred action counts. Close owned browser tabs and summarize only after final deployment proof.
