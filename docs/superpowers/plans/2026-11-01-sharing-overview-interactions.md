# Sharing Overview and Remaining Interactions Implementation Plan

**Goal:** 补齐管理员跨商户分账规则/记录总览、查询上下文与安全反馈，并消除公告新增布局等可实现的剩余交互差异。

**Architecture:** 新增仅管理员 GET 的总览端点，沿用受控分页、统计及 CSV；现有单商户读写端点保留。规则总览读取已持久化当前版本，不依赖实时上游或启用通道；配置复用单商户编辑组件。记录行操作绑定服务端返回的商户，不使用尚未查询的输入值。外部业务插件仍按此前约定暂缓。

**Tech Stack:** Go、PostgreSQL、Vue 3、TypeScript、Vitest、Playwright。

**Spec:** 用户要求继续补齐剩余差异，重点跨商户分账总览及其它交互，全部测试后统一总结。

## Global Constraints

- 复用已有合成 PostgreSQL 容器，唯一测试库/模式隔离；零连接后清理自己的资源。保留现有冻结预览，不新建容器。
- 生产环境只读；不发起真实交易或修改线上配置。参考程序和敏感数据不上传。
- 不以未知字段、缺失商户号或非法 URL 退化为全商户查询；商户会话不得访问总览。
- 资金动作继续使用明确商户、原始业务号、原版本及既有资金授权；结果未知仅核对原始请求。

## Task 1: 管理员只读查询与权限

Files: `internal/payment/channel_sharing_admin_overview.go`、`channel_sharing_order_list.go`、`internal/server/channel_sharing_overview_console.go`、`channel_sharing_console.go`、`admin_menu_permissions.go` 及对应测试。

- [x] 写真实数据库失败用例：两个商户、当前规则版本、状态/规则/接收方/通道筛选、分页、真实子通道来源、脱敏、统计/CSV、日期边界；拒绝商户角色和无权限管理员。
- [x] 新增 `ListAdminChannelSharingRules`、`ListAdminChannelSharingOrders`、`AdminChannelSharingOrderStatistics` 和独立 CSV 构造函数；保持原单商户方法拒绝缺失商户。
- [x] 暴露 GET `/channel-sharing/rule-overview`、`/order-overview`、`/order-overview/statistics`，精确解析可选正整数商户/通道/子通道筛选，拒绝空值/重复/未知参数及写方法；权限分别映射 PsReceiver/PsOrder。
- [x] 针对性回归通过后检查受影响渠道分账测试、权限与 CSV 路径。

## Task 2: 总览页面与操作上下文

Files: `web/src/components/AdminChannelSharingRules.vue`、`ChannelSharingRuleList.vue`、`ChannelSharingOrderList.vue`、`web/src/sharing-overview.ts`、`App.vue` 及 Vitest/Playwright。

- [x] 规则总览默认自动读取全部商户；显示用户ID、真实规则版本、通道和脱敏接收方、状态。支持已应用筛选、分页、失败重试及空态；配置复用明确目标商户的编辑组件。
- [x] 记录总览默认自动读取；显示用户ID与原订单选定的子通道（缺失显示“—”）。查询、刷新、翻页、统计和导出绑定同一已应用筛选，未提交草稿不影响结果。
- [x] 行结果查询、详情及回退捕获该行商户/原始请求；精确校验响应归属；相同分账号在不同商户间不能串用。非法路由参数不扩大范围，旧响应不得覆盖新范围。
- [x] 回退使用固定请求号；丢失或无法核实结果时保留原意图，仅允许查询原请求，不自动换号再发送。
- [x] 375/1280 宽度与键盘操作、本地合成读写、网络失败和未知状态回归。

## Task 3: 其它交互与整合验证

Files: `web/src/components/AnnouncementsPage.vue`、相关用例、`docs/verification/admin-node-parity-2026-11-01.md`。

补充范围：付款记录增加共用原列表筛选的精确全范围统计；分账记录勾选后提供最多20条原请求顺序核对，确认前零请求、失败即停。手机宽表格的空态保持可见，确认长列表可键盘滚动。

- [x] 对照已有证据将公告新增改为内联表单；编辑保留明确目标，保存中防重复，失败保留草稿，成功刷新一次。
- [x] 复核本轮总览与已有单商户配置、商户分账、其它菜单的交互，不以删除账务事实或虚设插件能力追求表面一致。
- [x] 付款统计、批量原分账核对及手机空态完成针对性单测与浏览器验证；精确金额、原商户与会话变更隔离一并覆盖。
- [x] 最终完整前端单测、类型/构建、浏览器套件，以及后台/渠道分账和服务器权限测试均记录当次结果；跳过项单列。前端1,611通过；浏览器610常规及13本地后端通过；Go清单2,892通过/28条件跳过/0失败/0遗漏，12项Go浏览器专项已启用验证。
- [x] 清理自己的测试库，检查差异与源码清单，更新矩阵；完成后统一总结，不在子任务结束时停工。最终验收脚本通过，登记123个本次数据库名称实际残留0，新增容器0。
