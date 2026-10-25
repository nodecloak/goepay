# 原订单操作UX与业务入口实施计划

> **For agentic workers:** 按下述测试→实现→验证步骤连续执行。沿用现有工程与本地模拟环境，不访问生产。当前会话执行，保留失败证据。

**Goal:** 对齐用户截图和本地原页面的订单行操作、状态菜单、确认流程，恢复已有能力的直接入口，并实现审计状态标记、逻辑删除和独立手动退款。

**Architecture:** 行菜单与动作弹窗独立组件；API退款、冻结/解冻、重发通知复用现有资金与通知能力。后台标记/逻辑删除作为独立操作审计投影，真实支付与账本保持原事实；手动补单核验原通道并补齐已有入账链。手动退款在同一事务内使用原预留/退款资金/奖励回收和内部结果证明，并记录管理员线下确认，不调用通道。不改已部署迁移。

**Tech Stack:** Vue3/TypeScript/Vitest/Playwright、Go/pgx/PostgreSQL。

**Spec:** 用户两张截图；`epay_release/admin/order.php:247`、`admin/ajax_order.php:266`、`includes/lib/Order.php:68`；Pro编译前端订单动作核对。原后台控制器加密，差异明确显示在操作确认中。

## Global Constraints

- 不读取真实凭据，不访问生产或支付机构；所有部署验证仅sandbox/simulation。
- 已支付：改未完成、API退款、手动退款、冻结订单、分隔线、重新通知、删除订单；冻结时改为解冻，未完成：改已完成、分隔线、手动补单、删除订单。
- 菜单键盘可操作，不被表格滚动容器裁切；关闭、切换订单/会话清除旧详情，异步响应不能串单。
- 所有写入需角色/本人权限、CSRF、服务端当前状态校验。手动退款使用现有资金授权，保留稳定幂等号和原金额；失败不把未知结果显示为成功。
- 状态标记只改变后台操作标记，不伪造通道支付/入账；逻辑删除保留付款查询、退款及审计依据，管理员可查看恢复。界面明确这两个边界。

## Task 1: 状态菜单与直接动作对话框

**Files:** 新建`web/src/order-actions.ts`、`components/OrderActionMenu.vue`、`components/AdminOrderActionDialog.vue`；修改`DataTable.vue`、`App.vue`、`OrderIncomePanel.vue`、`NotificationDeliveryPanel.vue`；测试`web/tests/order-actions.test.ts`及`web/e2e/order-actions.spec.ts`。

接口：`adminOrderActions(row)`返回按原顺序的动作；菜单发出`action`，弹窗只读取新订单详情，用户确认后才写入。API退款沿用原表单；其余现有能力直接在动作弹窗复用组件，不要求先打开完整详情。

- [ ] 先增加菜单状态与顺序断言，执行`npm test -- tests/order-actions.test.ts`，确认缺少模块/菜单行为失败；实现并重跑。

```ts
expect(adminOrderActions({status:'paid'}).map(a=>a.label)).toEqual(['改未完成','API退款','手动退款','冻结订单','重新通知','删除订单']);
expect(adminOrderActions({status:'pending'}).map(a=>a.label)).toEqual(['改已完成','手动补单','删除订单']);
```

- [ ] 增加浏览器断言：未确认不POST、Escape/外部点击关闭、375/1280不溢出、菜单项直接弹出对应操作；实现最小变更并执行。

## Task 2: 后台状态标记、删除与补单

**Files:** 新建`internal/server/order_admin_actions.go`、对应测试；修改`server.go`、`order_operations.go`、`list.go`、`list_filters.go`。

接口：订单详情返回`admin_control`与动作能力；POST`/orders/{trade}/admin-status`、`archive`、`restore`要求规范external_no、reason、expected_revision。使用订单行锁及`backoffice_audit`追加记录计算修订，重复同号同体幂等，重复异体409。列表默认隐藏已删除，管理员显式`include_archived=true`可查看恢复。POST`manual-complete`只查询原通道，可信paid后恢复自动标记，未知不改完成。

- [ ] 先写PG/HTTP测试：状态标记不改变orders.status/余额/支付API事实；归档保留原订单；商户/观察员/缺CSRF拒绝；旧修订与同号异体冲突，观察RED后实现。
- [ ] 加入有可信回执才补单、未知不重发/不造收入测试；执行相关server/payment/contract测试并保留原失败。

## Task 3: 独立线下手动退款

**Files:** `internal/payment/reservations.go`提取事务内原预留函数；新建`manual_refund.go`、`manual_refund_test.go`；修改`internal/server/funds_action_resolver.go`与新动作HTTP；原退款资金/结果证明不改语义。

接口：`Store.ManualRefund`只允许带管理员actor、reason、线下已处理确认，事务内原预留→原退款结转→内部HMAC结果证明→审计；绝不创建provider_operations。相同原请求重复返回原结果，旧未知API退款仍占原额度。HTTP POST`manual-refund`使用现有refund资金授权目标。

- [ ] 写实际PG断言：部分/全额/并发/同号异体/余额不足/审计失败回滚/已有unknown保留/商户拒绝/资金授权必需，执行RED再实现。

```go
// 同一手动退款重放只存在一份退款和一次退款结转；provider_operations始终0。
// 事务失败时退款、余额、资金授权消费和审计全部不留下部分提交。
```

## Task 4: 验证与本地交付

2026-10-25增量：原行菜单/控制已进入109独立预览。继续补原批量与自动刷新；新`order-batches`审计命名空间不增加财务表或改支付事实。`/orders/batch-control`只支持标记完成/未完成、归档、恢复，最多100原版本绑定订单、稳定请求号，整批事务与最终审计同成败；已实测501→GREEN、并发8重试只一次批审计、第二单旧版本和最终审计失败时整批回滚。新多选/确认/可执行数量及跨SPA未知结果保留有浏览器RED/GREEN；自动刷新明确开始、最少3秒、无重叠、隐藏页/操作弹窗暂停、离页停止。新增源码尚未固定全量回归或部署，不能把其它批量资金操作视为已经完成。

固定110的368单元/200浏览器及Linux服务+订单合同race、全库vet/build现已全部通过，仍未部署。后续工作树继续恢复原列表支付IP/账号旁拉黑按钮与详情入口：必须原订单来源精确比对，0天永久、最多36500天，选填备注，稳定请求号；期限/状态不因重放延长或启用。到期后实际支付政策不命中，旧无期限永久，损坏期限保持拦截；来源变化/同号异体409，最终审计失败无部分黑名单。HTTP501/浏览器缺按钮RED已观察，针对性数据库及浏览器6项通过；111固定回归待执行。

- [ ] 前端完整unit/typecheck/build及mock浏览器；实际PG相关退款/冻结/通知/账本与server/contract回归，Linux相关race；只改UI的既有测试保持原断言含义。
- [ ] 审阅diff中的状态、权限、授权、并发及网络未知路径，修复重要问题；不将原菜单可见数量当完成功能比例。
- [ ] 固定明确新源码构建本地API/web，同一项目保留原数据库；本地实际菜单/确认/模拟退款及归档恢复验收，验证账务和默认外部能力关闭。
- [ ] 更新独立本轮报告，说明原行为与保留账务/可信补单差异，记录实际执行范围与证据，再向用户汇总。
