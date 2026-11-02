# System settings parity Implementation Plan

复选项记录实际完成状态；测试和部署结果以独立回执为准。

**Goal:** 对照参考站系统设置17个分类，将已实现功能的字段、分组、单位、默认反馈、条件显示和保存交互对齐，修复后测试并部署。

**Architecture:** 参考站和Go线上只读，不保存生产配置。优先复用真实版本化配置接口与已有组件，修正设置目录与分组；任何新增配置必须有真实读取/执行路径。安全敏感资料继续经专用加密接口管理，不退回通用明文设置。外部插件不扩展。

**Tech Stack:** Vue 3、TypeScript、Go、PostgreSQL、Playwright、Vitest。

**Spec:** 本轮用户指示：继续对照 p.ma3fu.com 系统设置下所有节点，发现问题修复，测试后部署；资金仅本地模拟，暂缓外部插件不做。

## Global Constraints

- 项目 E:/GOEPAY；基线 main 2629704，线上前端227，后端225，151迁移。
- 不在两站生产配置点击保存、发送、启停、删除或运行任务；只读与未提交草稿允许。
- 复用既有 goepay-full195-windows-b378c4a3 PostgreSQL池55101；本轮自有GUID数据库，零连接后清理，不创建容器、网络或卷。
- 保留8187预览与CURRENT.json，保留失败回执，不改写227证据。本轮 .tools/settings228-20261102，发布 .tools/deploy228。
- 参考表单只记录字段标签、类型、选项标签、分组和按钮，不记录字段值、密钥或收款资料。
- 实名认证、OCR、进件/投诉外部插件保持暂缓；仅展示明确状态。Go运行环境选项不得伪装成可在线生效的PHP选项。

## Task 1: Field and behavior matrix

**Files:** Create docs/verification/system-settings-parity-2026-11-02.md; read web/src/settings.ts, web/src/components/SettingsPage.vue, existing embedded configuration components and matching Go readers.

**Interfaces:** reference browser metadata -> per-category original label / canonical Go field or endpoint / persisted consumer / writable or deferred boundary.

- [x] Capture all17 reference and Go categories, including safe dependent form branches and secondary account/template dialogs without production submission.
- [x] Trace every missing/misplaced field to an existing implementation, a Go deployment-only control, or the explicitly deferred plugin scope; distinguish configured current values from shipped defaults.
- [x] Identify concrete payload/unit/default/cancel/refresh issues; write a regression that fails before each product fix.

## Task 2: Implement coherent settings forms

**Files:** Modify web/src/settings.ts, SettingsPage.vue and focused existing embedded components as required; create focused integration components only where an existing real API needs presentation. Backend changes, if necessary, stay within existing configuration/service modules and tests.

**Interfaces:** preserve setting keys and versioned fingerprints, secret keep/replace/clear operations, global dirty-draft registry and own-session/administrator permissions.

- [x] Align labels, headings, grouping, dependent options and units; put existing merchant-related capabilities into merchant settings and corresponding payment capabilities into payment settings.
- [x] Expose existing dedicated policy/notification/printing/mail/SMS controls in their reference categories without duplicating conflicting generic writers.
- [x] Keep drafts across tabs, cancel/discard and navigation guard; prevent unknown-result automatic replay; validate empty/default and unsupported values explicitly.
- [x] Verify every editable control has a persisted effect and every deployment-only/deferred control has an honest explanation.

## Task 3: Real local workflows and regression

**Files:** New focused web/tests and web/e2e specs; reuse guarded local fixtures and existing PostgreSQL pool. Evidence/scripts under this round only.

**Interfaces:** browser form -> actual Go HTTP -> unique synthetic PostgreSQL; external send/payment adapters disabled or loopback-only fakes.

- [x] Run representative real save/reload/cancel for writable categories and existing embedded policies; assert canonical stored units, unchanged secrets and independent saves.
- [x] Test missing settings/defaults, forbidden actor, stale version, partial/unknown save responses and invalid inputs; no external mail/SMS/printing or funds.
- [x] Obtain independent read-only review, resolve findings, freeze source and asset lists.
- [x] Run frontend full unit/typecheck/build/mocked suite and all actual local suites; if Go inputs change rerun required backend full coverage/build/vet instead of reusing old evidence.
- [x] Verify all33 reconciliation metrics, owned database cleanup and preview identity; separate any expected deferred behavior from success.

## Task 4: Publish and verify

**Files:** Versioned verification document, guarded deployment scripts/evidence in .tools/deploy228.

**Interfaces:** tested source/assets -> clean committed/pushed main -> pinned package -> guarded release -> live read-only settings checks.

- [x] Complete field/operation matrix and accurately record tests and intentional runtime/plugin boundaries.
- [ ] Scan staged paths/credentials, commit/push tested source; run isolated release safeguards before deployment.
- [ ] Deploy pinned artifacts, verify configuration/service/database invariants and public hashes; recheck17 live setting categories without saving, close owned browser tabs, summarize after verification.

此计划随受测源码在发布前冻结；最后两项的实际执行结果以`.tools/deploy228/FINAL.json`为准，不在部署前预先标记成功。
