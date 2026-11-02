# System management parity Implementation Plan

**Goal:** 对照参考站计划任务、公告配置、管理员、登录日志、运行日志、数据清理及系统更新，修复字段和操作交互，使用本地隔离环境验证后部署。

**Architecture:** 保留现有 Go 接口、权限和不可变财务记录规则。参考站和 Go 线上仅查看页面及未提交表单；新增、修改、删除、任务运行、日志清理及更新准备均在本地合成数据环境执行。优先修复现有 Vue 组件；清理流程独立成组件，复用已经实现的运行日志清理弹窗和公共对话框。

**Tech Stack:** Vue 3、TypeScript、Vitest、Playwright、现有 Go HTTP 与 PostgreSQL。

**Spec:** 本轮用户要求：系统管理下剩余七个页面逐项检查字段、操作及交互，发现问题修复，测试后部署；资金操作仅本地模拟，暂缓外部插件不做。

## Global Constraints

- 项目 E:/GOEPAY，基线 main bcf88369f60b838d377a7388f8b80e65fb590401，线上版本228，151项迁移。
- 沿用既有 PostgreSQL 容器 goepay-full195-windows-b378c4a3 的55101端口。每个实际浏览器测试文件使用自有GUID数据库，零连接后清理，不新建容器、网络或卷。
- 保留当前预览指针、CURRENT.json及所有历史回执。本轮证据使用 .tools/system-management229-20261102，发布证据使用 .tools/deploy229。
- 生产页面不保存、不运行任务、不清理数据、不准备更新、不发送通知。管理员表单不填写凭据，不变更权限。
- Go源码如保持不变，校验其与228全量3054项覆盖的冻结输入一致，并实际执行本轮HTTP浏览器及相关回归；不把历史证据描述为新的全量运行。
- 登录清理接口实际包含四类：过期会话、内存尝试计数、持久限流记录、登录审计。不得再次按早期前端文字猜测执行范围。

## Task 1: Seven-page comparison matrix

**Files:** docs/verification/system-management-parity-2026-11-02.md；读取 TaskSchedulerPage.vue、AnnouncementsPage.vue、ResourcePage.vue、ResourceFields.vue、SystemLogPage.vue、RuntimeLogPage.vue、OperationsPage.vue、SystemUpdatePage.vue 及对应Go处理器。

**Interfaces:** 两站浏览器可见标签、选项、列名、按钮、未提交表单 -> 对应页面/接口/权限 -> 本地操作与持久化断言。

- [x] 记录七页字段、按钮和条件显示，打开非破坏性表单后取消；不记录密钥、用户资料或日志正文。
- [x] 将Swoole调度入口、PHP原地升级和不可变交易数据清理等运行机制差异单独记录，外部插件不扩展。

## Task 2: Accurate cleanup scope and confirmation

**Files:** 新增 web/src/components/DataCleanupPage229.vue、web/src/data-cleanup229.ts、web/tests/data-cleanup229.test.ts；修改 OperationsPage.vue 的cleanup入口和 clean-kit-ux2.test.ts。

**Interfaces:** POST /operations/cleanup，preview=true/false，cutoff为UTC+8截止日期对应的RFC3339时间；读取四项计数和实际返回截止时间。复用 RuntimeLogCleanup.vue 独立处理运行日志。

- [x] 首先运行失败回归：模拟返回四类非零计数，断言四项均显示；取消确认不得产生preview=false请求。
- [x] 日期限制与后端1年至前一天的窗口一致；输入、预览和确认使用同一截止条件，忙碌时禁止编辑，旧预览不能覆盖新输入。
- [x] 仅本人超级管理员展示可写操作；会话/权限变化中止在途响应，清空待确认状态。
- [x] 损坏成功响应或网络结果未知时不显示成功、不重复删除；仅允许重新预览核对。
- [x] 显示真实删除数量；运行日志入口复用原有确认流程。财务记录保持禁用并说明实际边界。

回归形状：

```ts
expect(screen.text()).toContain("登录日志");
expect(screen.text()).toContain("持久限流记录");
expect(writes.filter(x => x.preview === false)).toHaveLength(0);
```

## Task 3: Task and administrator conditional forms

**Files:** 修改 TaskSchedulerPage.vue、ResourceFields.vue；新增或扩展对应Vitest及Playwright文件。

**Interfaces:** 原scheduled-tasks版本指纹、控制模式与运行记录保持不变；管理员菜单权限仍只对operator有效，不扩展服务端授权。

- [x] 验证一次性任务不要求无意义的间隔，并使用“执行时间”标签；周期任务保留间隔和首次时间，取消不写入。
- [x] 新增管理员未选角色或选择super时隐藏不适用的菜单权限树，operator时才显示；切换不隐式修改已选择的权限。
- [x] 本地实际新增管理员、编辑、权限切换、启停及删除，检查持久化和本人会话权限；不调整生产账号。

回归形状：

```ts
expect(wrapper.find(".admin-permission-tree").exists()).toBe(false);
await wrapper.setProps({ values: { admin_class: "operator", permissions: [] } });
expect(wrapper.find(".admin-permission-tree").exists()).toBe(true);
```

## Task 4: Announcements, logs and update lifecycle

**Files:** 对照及测试 AnnouncementsPage.vue、SystemLogPage.vue、RuntimeLogPage.vue、SystemUpdatePage.vue；新增 web/e2e/live-system-management229.spec.ts，复用现有actual HTTP fixture。

**Interfaces:** 公告资源接口；两类日志筛选与分页；现有签名离线更新包、准备/撤销/重试接口。更新包准备与实际部署状态必须明确区分。

- [x] 公告新增、排序/颜色编辑、显示/隐藏、取消删除、确认删除、刷新及末页回退；检查实际持久化。
- [x] 日志用户ID/IP筛选、日期筛选、查询、重置和分页；运行日志仅在本地执行清理并核对范围。
- [x] 系统更新明确“准备更新包”的行为，取消不提交，未知结果只核对原请求；使用已有合成签名更新包验证prepared/applied区别，不请求真实更新服务。
- [x] 只对可复现的问题修改实现；已对齐的节点补充操作验收记录，不为了产生diff改动。

## Task 5: Regression, publication and read-only recheck

**Files:** 本轮验证文档和独立证据；复用228部署保护，使用229独立目录与包，不覆盖历史回执。

- [x] 先完成定向失败/通过回归，再冻结输入，运行全量前端单元、类型检查、生产构建、模拟浏览器及全部实际本地浏览器文件。
- [x] 验证33项对账、所有自有数据库清理及预览身份；若Go变化，重新安排相应后端完整覆盖。
- [ ] 独立复核、暂存路径及敏感信息检查，提交推送并部署固定产物。
- [ ] 线上只读复查七页、公开静态文件哈希、服务版本、配置与迁移；关闭本轮临时页，最后总结。

本轮按用户已授权的修复、测试和部署连续执行，不在各子步骤结束时等待再次确认。计划随源码在发布前冻结，最终上线结果由独立部署回执记录。
