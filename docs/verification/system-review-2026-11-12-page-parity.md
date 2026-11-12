# 前端管理端/商户端真实覆盖库存

基线：`d0064be`；审查使用 `E:/GOEPAY` 本地源码、Pro静态资源、合成组件/mock测试及隔离本机API/浏览器。没有访问生产、真实资金、邮件短信、OAuth或实体打印，没有新建容器。

## 证据口径

`已定位`表示前端动作/字段及对应后端代码已定位，**不代表所有动作本轮执行通过**。原Pro菜单54管理端+28商户端，另有Go补充15入口；按名称/路径/隐藏/角色规范化比较结果：admin 54项 一致；merchant 28项 一致。菜单存在不计功能完成。

原始动作依据是本地 `epay_pro/public/static/assets/*.js` 与 `epay_pro/route/{admin,user}.php`；本地资源能证明按钮/API调用名及部分输入，绝大多数Pro入口控制器加密，不能证明隐藏规则等价。旧 `web/capabilities.json` 的82项仍全标partial，是粗粒度历史索引，不能据此宣布全功能通过。

以下每行列出实际字段、动作、Go实现和明确差异。所有未单独注明本次通过的行状态均为“实现已定位／原版行为未全证”；写有暂缓的仅暂缓该外部动作，不能扩大为本地核心功能暂缓。

## 本轮发现与修复

1. 代付账户/通道统计的未提交草稿会改变分页、刷新与CSV范围。改为绑定已提交筛选，导出本页文件名也使用同一范围；新增交互回归先失败后通过。
2. 通用统计失败保留旧total/投诉summary；无效日期申请期间旧请求还能回填；更换report上下文不会重新读。已清除过期统计、终止旧请求并核对会话/报表上下文。注意 `/complain/charts` 当前走专用 `ComplaintsPage`，通用complaints分支测试不冒充实际菜单端到端验收。
3. KitSelect禁用时已展开选项仍可改值。禁用会关闭选项且选择处理器拒绝更新。
4. 通用ResourceFields在慢速保存期间仍可编辑，新输入会随成功关窗丢失。加入editingBusy锁定；明确422拒绝后恢复原草稿。
5. 全量模拟浏览器发现批量付款文件导入丢失原错误行号：TransferBatchImport把已经汉化的ApiError.message交给原文解析器。改为解析serverMessage，仍只显示固定诊断白名单。真实ApiClient错误解码＋文件change组件回归先失败后通过，原草稿不被应用/覆盖。

## 管理端原菜单（54）

| 入口 | 字段 | 动作 | 前端 / 后端证据 | 原Pro动作证据 | 状态/差异 |
|---|---|---|---|---|---|
| 仪表盘 <br>`/dashboard` | 日期、收款/订单/余额/结算指标 | 读取仪表盘、时间维度、公告、收款快捷入口 | DashboardOverview.vue<br>internal/server/dashboard.go | Index/getDashboardData | 原Pro服务端隐藏规则未证明完全等价；菜单只读巡检不等于逐项写操作或筛选组合通过，具体执行范围见本轮live记录。 |
| 订单管理 <br>`/trade/order` | 订单/商户/渠道号、订单状态、支付方式、日期、金额、退款余量 | 筛选/分页/统计/CSV、详情、查单、退款、关单、通知、收入冻结/解冻、黑名单、打印、子订单；管理/商户动作权限分别判断 | App.vue; OrderDetails.vue; AdminOrderActionDialog.vue; MerchantOrderActionDialog.vue<br>internal/server/order_operations.go; order_admin_actions.go; merchant_order_actions.go; order_income_console.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 真实流水不能仅凭前端改变；资金原意图及未知结果保护已定位。上游支付/退款/打印不在本轮执行；并非原Pro任意状态修改/物理删除的逐字复制。 |
| 退款记录 <br>`/trade/refund` | 退款号、订单号、商户号、状态、方式、起止日期、原退款金额 | 筛选/分页/统计/CSV、原退款结果与历史记录核对 | App.vue; RefundSummaryDialog.vue; RefundAttestationDialog.vue; LegacyRefundHistoryPanel.vue<br>internal/server/refund_summary.go; refund_search.go; legacy_refund_history.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 列表读取不代表新退款完成；原Pro隐藏退款统计口径需独立契约证明。 |
| 黑名单管理 <br>`/trade/blacklist` | 类型、内容、截止时间、备注、搜索 | 新增/编辑/详情/删除、批量操作、从订单添加 | ResourcePage.vue; BlacklistAddDialog.vue<br>internal/server/backoffice.go; order_blacklist.go | black-CPNx7x-L.js | 原Pro服务端隐藏规则未证明完全等价；菜单只读巡检不等于逐项写操作或筛选组合通过，具体执行范围见本轮live记录。 |
| 每日交易统计 <br>`/trade/stat/daily` | 业务日期、支付方式、通道、商户；支付/退款/手续费/成功率 | 查询/快捷日期/重置、趋势、CSV | OriginalOrderStatistics.vue<br>internal/server/original_order_statistics.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 按UTC+08:00；缺失成本不推算利润。已定位按筛选变化撤销旧结果；本轮未独立执行。 |
| 商户交易统计 <br>`/trade/stat/user` | 商户号、排序、日期、方式/通道、维度 | 查询/重置、矩阵统计与导出 | MerchantMatrixReport.vue<br>internal/server/merchant_matrix_report.go; merchant_matrix_catalog.go | order-Cn9IuGiH.js; transfer-DQfSWUDt.js | 原Pro服务端隐藏规则未证明完全等价；菜单只读巡检不等于逐项写操作或筛选组合通过，具体执行范围见本轮live记录。 |
| 通道数据统计 <br>`/trade/stat/channel` | 业务日期、支付方式、通道、商户；支付/退款/手续费/成功率 | 查询/快捷日期/重置、趋势、CSV | OriginalOrderStatistics.vue<br>internal/server/original_order_statistics.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 按UTC+08:00；缺失成本不推算利润。已定位按筛选变化撤销旧结果；本轮未独立执行。 |
| 地区数据统计 <br>`/trade/stat/province` | 日期、支付方式、地区、商户口径 | 查询/重置、地图/表格、加载失败、CSV | ProvinceStatisticsReport.vue<br>internal/server/reports.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 地图动态模块具备加载/错误反馈；IP归属地未知不虚构。相关加载回归本轮通过。 |
| 支付用户统计 <br>`/trade/stat/buyer` | 支付账号、方式、日期、次数/金额排序 | 查询/分页/重置、统计/CSV、身份查找和黑名单权限 | BuyerStatisticsReport.vue<br>internal/server/buyer_statistics.go | order-Cn9IuGiH.js | 原Pro服务端隐藏规则未证明完全等价；菜单只读巡检不等于逐项写操作或筛选组合通过，具体执行范围见本轮live记录。 |
| 分账规则 <br>`/profitsharing/receiver` | 商户、通道、接收方类型/账号、比例、最大比例、启用状态 | 接收方草稿/添加/删除、规则读取/保存/启停、管理员跨商户规则总览 | ChannelSharingPage.vue; AdminChannelSharingRules.vue; ChannelSharingRuleList.vue<br>internal/server/channel_sharing_console.go; channel_sharing_overview_console.go | profitsharing-YCDzcmdP.js; profitsharing-DjO4Lt6k.js | 支付宝/微信渠道分账与站内分账分开；真实远端接收方注册和分账未执行，其他插件继续暂缓。 |
| 分账记录 <br>`/profitsharing/order` | 原订单、商户/通道、分账请求号、回退号、金额、状态 | 统计/筛选/分页、提交/查单、完结、回退及原请求查询 | ChannelSharingPage.vue; ChannelSharingOrderList.vue; ChannelSharingFinishPanel.vue<br>internal/server/channel_sharing_console.go; channel_sharing_finish_console.go; channel_sharing_return_console.go | profitsharing-YCDzcmdP.js; profitsharing-DjO4Lt6k.js | 未知分账/回退只核对原请求。远端资金操作未执行；不可计作全部上游联调通过。 |
| 站内分账规则 <br>`/profitsharing/internal` | 规则名称、范围、收款方、比例/金额、启用 | 资料CRUD、校验后申请站内分账 | ResourcePage.vue; InternalSharingRuleFields.vue; RuleExecutionPanel.vue<br>internal/server/backoffice.go | profitsharing-YCDzcmdP.js | 该规则只操作平台账本，不等同渠道分账。审批入口是Go补充菜单。 |
| 付款记录 <br>`/transfer/list` | 通道、付款产品、收款账号/姓名、金额、手续费、银行编码、备注、请求号 | 列表/筛选/分页/统计/CSV、能力读取、准备/确认付款、查原单/凭证 | TransferPage.vue; TransferListFilters.vue; TransferListTable.vue<br>internal/server/transfer_http.go | transfer-DQfSWUDt.js | 不可把prepared/pending/unknown显示为到账；以分和十进制字符串保持精度。真实资金与未接入插件未执行。 |
| 付款账号统计 <br>`/transfer/stat` | 日期、付款方式；通道统计另有商户号；金额/手续费/数量/成功率 | 查询/快捷时间/重置、分页、刷新、本页与筛选CSV | ReportPage.vue<br>internal/server/reports.go | transfer-DQfSWUDt.js | 本轮修复草稿筛选污染分页/CSV、失败残留总计和上下文旧响应；相关组件回归通过。fee_money为手续费，不冒充上游成本后的利润。 |
| 通道数据统计 <br>`/transfer/channel-stat` | 日期、付款方式；通道统计另有商户号；金额/手续费/数量/成功率 | 查询/快捷时间/重置、分页、刷新、本页与筛选CSV | ReportPage.vue<br>internal/server/reports.go | transfer-DQfSWUDt.js | 本轮修复草稿筛选污染分页/CSV、失败残留总计和上下文旧响应；相关组件回归通过。fee_money为手续费，不冒充上游成本后的利润。 |
| 商户代付统计 <br>`/transfer/user-stat` | 商户号、排序、日期、方式/通道、维度 | 查询/重置、矩阵统计与导出 | MerchantMatrixReport.vue<br>internal/server/merchant_matrix_report.go; merchant_matrix_catalog.go | order-Cn9IuGiH.js; transfer-DQfSWUDt.js | 原Pro服务端隐藏规则未证明完全等价；菜单只读巡检不等于逐项写操作或筛选组合通过，具体执行范围见本轮live记录。 |
| 结算管理 <br>`/settle/list` | 申请号、商户、金额、费用、净额、收款版本、审批/付款状态 | 筛选/分页/统计/CSV、申请、批准/拒绝、原净额准备/发送/查询、取消未发送 | WorkflowPage.vue; SettlementRecordDetails.vue; SettlementPayoutPanel.vue<br>internal/server/backoffice.go; funds_settlement_payout.go | settle-C3-SgkfR.js; settle-DoI_EC2g.js | 审批不等于付款；收款不可变版本与原资金冻结保留。真实出款和原Pro隐藏费用规则未逐项联调。 |
| 批量结算 <br>`/settle/batch` | 筛选、批次号、原申请/收款版本、金额/手续费/净额 | 生成、详情、执行、查询、取消未发送、分页 | SettlementBatchesPage.vue<br>internal/server/settlement_batch_console.go | settle-C3-SgkfR.js | 持久批次不以逐行点击替代；本轮未执行真实付款。 |
| 商户列表 <br>`/merchant/merchant-list` | 商户号、手机/邮箱、组、QQ、域名、登录信息、费率/权限、结算资料 | 筛选/分页/CRUD、账户启停、余额调整、代提现、凭据轮换、代登录、统计/日志 | ResourcePage.vue; MerchantCreateFields.vue; MerchantEditorFields.vue; MerchantBalanceAdjustDialog.vue; MerchantManualWithdrawal.vue<br>internal/server/backoffice.go; admin_merchant_credentials.go; admin_signature_policy.go | user-C5COc9Vu.js | 停用保留财务历史；代登录和资金动作要求本人权限。慢速通用编辑锁定本轮修复；商户专用编辑原已锁定。 |
| 用户组设置 <br>`/merchant/group-list` | 名称、费率、路由、结算周期/费用/留存、限额、代付/保证金/风控 | CRUD、配置费率/路由、复制/校验、统计 | ResourcePage.vue; GroupEditorFields.vue; CoreRiskGroupPanel.vue<br>internal/server/backoffice.go; group_routing_catalog.go | group-JyxuqdgF.js | 默认组受保护；配置保存和运行时消费需配合后端契约，不能仅依据输入框判断。 |
| 用户组购买 <br>`/merchant/group-buy` | 组售价/有效期/描述、销售订单/日期 | 读取、售价/描述保存、销售统计/分页 | GroupSalesPage.vue<br>internal/server/membership_sales.go | group-JyxuqdgF.js | 会员余额购买已定位；外部付款购买不因售价配置可保存而视为实现。 |
| 资金明细 <br>`/merchant/record` | 商户、操作类型、金额、余额、订单引用、日期 | 只读筛选/分页/统计/CSV、引用核对 | App.vue; LedgerRecordsTable.vue; LedgerSummaryDialog.vue<br>internal/server/list.go; ledger_summary.go; ledger_reporting_projection.go | user-C5COc9Vu.js | 不提供账本任意修改/删除；账务正确性由主任务后端审查与对账验证。 |
| 站内转账 <br>`/merchant/internal-transfer` | 收款商户、金额、备注、原请求号、状态 | 申请/列表/筛选/详情、管理员批准/拒绝、未知原请求核对 | WorkflowPage.vue; InternalTransferListTable.vue; AdminInternalTransferListTable.vue<br>internal/server/backoffice.go | User/internalTransfer | 站内账本转账不代表对外付款；本轮不执行真实资金。 |
| 授权域名 <br>`/merchant/domain` | 商户、域名、审核/启用、日期 | 新增/编辑/审核/启停/删除/批量、筛选/分页 | ResourcePage.vue<br>internal/server/backoffice.go | user-C5COc9Vu.js | 商户修改需重新审核；通用表单慢写期间锁定已新增回归，相关域名测试本轮通过。 |
| 邀请码管理 <br>`/merchant/invite-code` | 邀请码、生成数量、使用状态、有效期 | 生成、查询、复制、删除/清理、分页 | InvitationCodesPage.vue<br>internal/server/invitation_codes.go | User/inviteCode | 生成未知结果与末页删除回退已有实现；本轮仅源码定位。 |
| 支付通道 <br>`/payapi/pay-channel` | 名称、支付方式/产品、应用授权、加密凭据、费率、额度、订单名称 | 新增/编辑/启停/归档、绑定、统计、测试付款和原结果查询 | ParentChannelPage.vue; ParentChannelExtra.vue; RuntimeChannelPolicyPanel.vue; ParentChannelTestPayment.vue<br>internal/server/parent_channel_console.go; parent_channel_runtime.go; parent_channel_test_payment.go | channel-DoFrrF6Z.js | 仅已移植支付宝/微信能力；未移植插件继续暂缓。测试付款未在本轮发送。 |
| 支付方式 <br>`/payapi/pay-type` | 调用值、显示名称、设备、状态/排序 | 新增/编辑/启停/删除/取消、未知创建只回读 | PayTypePage.vue<br>internal/server/pay_type_catalog.go | Channel/payType | 原Pro服务端隐藏规则未证明完全等价；菜单只读巡检不等于逐项写操作或筛选组合通过，具体执行范围见本轮live记录。 |
| 支付通道轮询 <br>`/payapi/pay-roll` | 名称、支付方式、轮询策略、通道、权重/顺序、状态 | 草稿创建、配置通道、编辑/启停、保留配置历史 | ResourcePage.vue; PayRollCreateForm.vue; PayRollChannelEditor.vue<br>internal/server/backoffice.go; provider_policy_routing.go | channel-DoFrrF6Z.js | 运行时固定订单分配与配额由后端执行；不据资料记录断言所有Pro隐含回退行为等价。 |
| 公众号小程序 <br>`/payapi/pay-weixin` | 类别、名称、APPID、APPSECRET、说明、状态、授权版本 | 新增/编辑、保留/轮换凭据、归档、授权/查询、未知原请求恢复 | WeixinAppsPage.vue; ApplicationAuthorizationPanel.vue<br>internal/server/weixin_applications_http.go; application_authorization.go | weixin-DscPhMHx.js | 只回显脱敏信息；外部OAuth授权未执行，其他应用插件暂缓。 |
| 进件渠道配置 <br>`/applyments/channel` | 渠道名称、类型、启用与本地配置 | 本地渠道资料CRUD | ResourcePage.vue<br>internal/server/backoffice.go | applyments-BPI27AXd.js | 本地已实现；外部进件插件暂缓。保存资料不表示取得上游产品权限或已开通。 |
| 进件商户管理 <br>`/applyments/merchant` | 商户归属、主体/联系资料、状态、审核意见/备注 | 新建/编辑/列表/详情、本地提交审核、审核和历史备注 | ResourcePage.vue; CasePanel.vue<br>internal/server/backoffice.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 本地已实现；上游submit/query/contract/upload/checkBankCard等外部动作继续暂缓，不伪造完成。隐藏表单直达也受schema权限限制。 |
| 子通道管理 <br>`/applyments/subchannel` | 商户、父通道/子商户号、状态、权重、限额/费率 | 筛选/分页/详情、受限配置、启停、统计/测试入口 | SubchannelPage.vue<br>internal/server/subchannel_console.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 运行策略支持与原Pro全部进件插件不同；远端开户/签约暂缓，不能把资料绑定视为完成。 |
| 商户管控记录 <br>`/applyments/mchrisk` | 商户、通道、风险类型/状态、时间、说明 | 查询/详情与schema允许的本地动作 | ResourcePage.vue<br>internal/server/backoffice.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 上游管控查询/申诉/通知设置暂缓；不得将本地记录当上游已处理。 |
| 商户进件表单 （隐藏直达）<br>`/applyments/form` | 商户归属、主体/联系资料、状态、审核意见/备注 | 新建/编辑/列表/详情、本地提交审核、审核和历史备注 | ResourcePage.vue; CasePanel.vue<br>internal/server/backoffice.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 本地已实现；上游submit/query/contract/upload/checkBankCard等外部动作继续暂缓，不伪造完成。隐藏表单直达也受schema权限限制。 |
| 投诉记录 <br>`/complain/list` | 投诉号/订单、商户/通道、状态、日期、内容/证据 | 筛选/详情、渠道能力反馈、处理动作/原结果核对 | ComplaintsPage.vue<br>internal/server/complaints_console.go | complain-BSfav2Sa.js | 外部投诉插件继续暂缓；不可把本地处理备注当已回复上游或已退款。 |
| 投诉统计 <br>`/complain/charts` | 日期、商户/通道、投诉数量/金额/比率 | 统计查询、筛选、汇总 | ComplaintsPage.vue<br>internal/server/complaint_reports.go | complain-BSfav2Sa.js | 当前路由实际是ComplaintsPage专用图表；ReportPage complaints分支是保留通用组件，不把它的测试冒充该路由全覆盖。 |
| 同步任务 <br>`/complain/task` | 通道、任务状态、同步范围 | 列表/筛选、本地任务配置、显式能力拒绝 | ComplaintsPage.vue<br>internal/server/complaints_console.go | complain-BSfav2Sa.js | 外部同步/自动回复/自动退款插件暂缓；任务保存不等于上游已同步。 |
| 风控记录 <br>`/other/risk` | 商户、订单、风险类型、IP/买家、时间 | 筛选/分页/详情、来源核对 | ResourcePage.vue<br>internal/server/backoffice.go | order-Cn9IuGiH.js | 自动检测只限已配置核心规则；不证明Pro上游风控算法等价。 |
| 获取用户标识 <br>`/other/get-token` | 渠道/应用、用户标识、二维码/授权链接、有效期 | 创建一次性查找、读取原状态、复制/取消、资料查询 | IdentityLookupPage.vue; IdentityProfileLookup.vue<br>internal/server/identity_lookup.go; identity_profile.go; identity_profile_link.go | settings-mNzlkkmh.js; account-DkHvjhqn.js | 不从付款信息猜测身份；外部授权/查询不在本轮执行。 |
| 聚合收款码样式 <br>`/other/onecode` | 名称、画布/背景、二维码位置/大小、文字/颜色、启用 | 新增/上传/编辑/预览/删除/分页、保存后ID路由 | CollectionStylesPage.vue<br>internal/server/collection_styles.go; collection_style_images.go | onecode-DIYKuVAe.js | 公开链接展示与样式保存分开；本轮只定位，不冒称逐图片浏览器验收。 |
| 编辑收款码样式 （隐藏直达）<br>`/other/onecode/edit` | 名称、画布/背景、二维码位置/大小、文字/颜色、启用 | 新增/上传/编辑/预览/删除/分页、保存后ID路由 | CollectionStylesPage.vue<br>internal/server/collection_styles.go; collection_style_images.go | onecode-DIYKuVAe.js | 公开链接展示与样式保存分开；本轮只定位，不冒称逐图片浏览器验收。 |
| 应用市场 <br>`/app/app-store` | 名称、类型、已移植/未接入状态 | 静态目录搜索/查看详情（无商城安装后端） | AppStorePage.vue; PluginDetailDrawer.vue<br>internal/server/backoffice.go | plugin-manage-B8CyiDnm.js | 本地目录已实现；外部商城下载/任意插件安装执行继续暂缓。 |
| 本地插件 <br>`/app/plugin-list` | 插件名、类型、安装/移植/启用状态 | 列表/详情、已支持本地管理能力 | LocalPluginsPage.vue; LocalPluginManagement.vue<br>internal/server/backoffice.go | plugin-manage-B8CyiDnm.js | 未移植插件不伪装可执行；第三方动态安装继续暂缓。 |
| 模板配置 <br>`/app/template` | 模板选择、参数、图片、CSS/HTML预览 | 读取/编辑/上传/预览/取消/确认保存 | TemplatePage.vue<br>internal/server/template_console.go | settings-mNzlkkmh.js | 受保护iframe与可信模板；不执行任意PHP模板代码。 |
| 扩展配置 <br>`/app/addon` | 名称、参数、启用状态 | 按启用分组、CRUD本地资料 | ResourcePage.vue; AddonConfigGroups.vue<br>internal/server/backoffice.go | settings-mNzlkkmh.js | 本地配置已定位；外部扩展安装/执行暂缓，资料enabled不等于插件运行。 |
| 系统设置 <br>`/system/settings` | 17分类：站点、商户、支付、投诉、风控、结算、代付、OAuth、通知、实名/OCR、公告、邮件/短信、IP、代理、高级 | schema读取、草稿/保存/清空、版本校验；凭据/风险/资金政策独立专用接口 | SettingsPage.vue; ConfigValueEditor.vue<br>internal/server/backoffice.go; core_payment_risk.go; payment_defend206.go; merchant_api_switches.go | settings-mNzlkkmh.js | 保存中KitSelect菜单原可改值，本轮已修复；部署项只读，外部插件暂缓；完整字段见本报告专项说明。 |
| 计划任务 <br>`/system/cron` | 任务种类、频率、首次执行/一次性执行、启用、版本 | 读取/配置/取消/保存、安排/查询执行记录 | TaskSchedulerPage.vue<br>internal/server/scheduled_tasks.go | System/cron | Go任务白名单和授权，不恢复任意PHP/Swoole执行；安排pending不视为完成。 |
| 公告配置 <br>`/system/anounce` | 富文本、排序、颜色、显隐、日期 | 新增/编辑/取消/显隐/删除/分页 | AnnouncementsPage.vue<br>internal/server/backoffice.go; announcement_layout.go | settings-mNzlkkmh.js | 内容净化与公开排版独立；本轮静态定位。 |
| 管理员列表 <br>`/system/admin-user` | 用户名、显示名、角色、菜单授权、启用、登录/TOTP状态 | 新增/编辑/停用/删除；本人资金密码/TOTP走专用流程 | ResourcePage.vue; AdminPermissionField.vue<br>internal/server/backoffice.go; admin_user_security.go; admin_menu_permissions.go | AdminUser | 受限菜单不替代服务端授权；升super清空权限提交且保留operator草稿。通用字段慢写锁定本轮修复。另发现管理员等待锁期间撤权仍可操作的后端漏洞，修复/HTTP证据见admin-mutations.md。 |
| 登录日志 <br>`/system/log` | 来源/用户号、操作类型、IP、时间 | 筛选/分页/读取；授权下商户跳转 | SystemLogPage.vue<br>internal/server/system_log.go | System/log | 数据日志只读；原Pro加密服务端的过滤细节未全证。 |
| 运行日志 <br>`/system/runtime-log` | 日期、类型、正文、归档计数/截止日 | 查询/重置、预览/确认归档、原请求核对 | RuntimeLogPage.vue; RuntimeLogCleanup.vue<br>internal/server/runtime_log.go | settings-mNzlkkmh.js | 只归档合规范围的运行记录，不删除账本/财务事实；本轮不执行清理。 |
| 数据清理 <br>`/system/clean` | 清理截止日、预览计数、原请求号 | 预览/取消/确认、结果核对、运行日志归档入口 | DataCleanupPage229.vue; RuntimeLogCleanup.vue<br>internal/server/operations.go | settings-mNzlkkmh.js | 拒绝财务数据物理清理；生产/当前预览未触碰。 |
| 系统更新 <br>`/system/update` | 当前/最新版本、来源配置、包签名、准备/部署状态 | 检测、准备可信包、撤销、重试/查询 | SystemUpdatePage.vue<br>internal/server/system_update.go | update-DXgX2Q7M.js | 准备包不等于部署；本轮不部署、不替换程序。 |
| 修改密码 （隐藏直达）<br>`/system/change-password` | 原/新密码、TOTP验证码、本人资金密码 | 修改登录密码、TOTP绑定/关闭、本人资金密码管理 | AccountPage.vue; TotpSettings.vue; PaymentPasswordSettings.vue<br>internal/server/account.go; admin_user_security.go | password-AHCghn-i.js; account-DkHvjhqn.js | 各安全动作独立权限，取消不提交；资金密码不等于普通登录密码。 |

## 商户端原菜单（28）

| 入口 | 字段 | 动作 | 前端 / 后端证据 | 原Pro动作证据 | 状态/差异 |
|---|---|---|---|---|---|
| 仪表盘 <br>`/dashboard` | 日期、收款/订单/余额/结算指标 | 读取仪表盘、时间维度、公告、收款快捷入口 | DashboardOverview.vue<br>internal/server/dashboard.go | Index/getDashboardData | 原Pro服务端隐藏规则未证明完全等价；菜单只读巡检不等于逐项写操作或筛选组合通过，具体执行范围见本轮live记录。 |
| API 信息 <br>`/account/api-info` | 商户号、接口地址、密钥状态、签名算法、RSA公钥/轮换草稿 | 读取、复制、密钥轮换/确认、算法切换、原请求核对 | AccountPage.vue; MerchantConnectionInfo.vue; MerchantRSAKeyRotation.vue; MerchantSignaturePolicy.vue<br>internal/server/account.go; account_rsa.go; account_credentials199.go | account-DkHvjhqn.js | 完整秘密不写通用设置；新私钥只在授权流程展示，未执行生产轮换。 |
| 账户设置 <br>`/account/settings` | QQ、邮箱/手机、登录绑定、金额上下限、手续费承担、API开关、密码/TOTP | 读取/编辑/取消/清空、绑定/解绑、验证与凭据安全流程 | MerchantAccountSettings.vue; MerchantPersonalSettings.vue; MerchantAPISwitches.vue; MerchantEmailBinding197.vue<br>internal/server/account_profile.go; merchant_personal_settings.go; merchant_api_switches.go; account_email197.go | account-DkHvjhqn.js | 外部短信/邮件/OAuth未发送；API自助开关不能提升管理员权限。 |
| 实名认证 <br>`/account/certificate` | 姓名/证件、认证方式、证件图片、核验费用、状态 | 资料草稿/提交/授权、查询原认证、取消/完成收费 | MerchantCertification233.vue; OCRRecognition233.vue<br>internal/server/kyc233.go; ocr_admission233.go | Account/certificate | 实名/OCR已实现：固定供应商适配、凭据加密、原请求查询、受控收费与OCR准入。live-kyc已验证配置保存/遮罩/编辑/清除；供应商替身契约另由Go测试验证。真实身份与付费供应商未联调，未知核验不冒充通过。 |
| 余额充值 <br>`/account/recharge` | 金额、支付方式/通道、原订单/状态 | 创建充值支付、展示付款材料、查询充值历史 | CommercePage.vue; MerchantRechargeBalance.vue<br>internal/server/backoffice.go | User/recharge | 生成支付材料不代表余额入账；真实付款回调不在本轮执行。 |
| 购买会员 <br>`/account/groupbuy` | 用户组、售价、期限、余额/奖励快照 | 余额购买、历史/权益读取、原请求核对 | CommercePage.vue<br>internal/server/backoffice.go; membership_referral_policy.go | User/groupbuy | 余额购买已实现；外部付款购买明确未开放。 |
| 保证金 <br>`/account/deposit` | 保证金金额、有效要求、释放窗口、申请/审批状态 | 缴纳、释放申请、管理员批准/拒绝、历史核对 | CommercePage.vue<br>internal/server/backoffice.go | User/deposit | 功能关闭仍可读历史；真实资金和上游支付未执行。 |
| 站内转账 <br>`/account/internal-transfer` | 收款商户、金额、备注、原请求号、状态 | 申请/列表/筛选/详情、管理员批准/拒绝、未知原请求核对 | WorkflowPage.vue; InternalTransferListTable.vue; AdminInternalTransferListTable.vue<br>internal/server/backoffice.go | User/internalTransfer | 站内账本转账不代表对外付款；本轮不执行真实资金。 |
| 订单管理 <br>`/trade/order` | 订单/商户/渠道号、订单状态、支付方式、日期、金额、退款余量 | 筛选/分页/统计/CSV、详情、查单、退款、关单、通知、收入冻结/解冻、黑名单、打印、子订单；管理/商户动作权限分别判断 | App.vue; OrderDetails.vue; AdminOrderActionDialog.vue; MerchantOrderActionDialog.vue<br>internal/server/order_operations.go; order_admin_actions.go; merchant_order_actions.go; order_income_console.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 真实流水不能仅凭前端改变；资金原意图及未知结果保护已定位。上游支付/退款/打印不在本轮执行；并非原Pro任意状态修改/物理删除的逐字复制。 |
| 退款记录 <br>`/trade/refund` | 退款号、订单号、商户号、状态、方式、起止日期、原退款金额 | 筛选/分页/统计/CSV、原退款结果与历史记录核对 | App.vue; RefundSummaryDialog.vue; RefundAttestationDialog.vue; LegacyRefundHistoryPanel.vue<br>internal/server/refund_summary.go; refund_search.go; legacy_refund_history.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 列表读取不代表新退款完成；原Pro隐藏退款统计口径需独立契约证明。 |
| 每日交易统计 <br>`/trade/daily-stat` | 业务日期、支付方式、通道、商户；支付/退款/手续费/成功率 | 查询/快捷日期/重置、趋势、CSV | OriginalOrderStatistics.vue<br>internal/server/original_order_statistics.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 按UTC+08:00；缺失成本不推算利润。已定位按筛选变化撤销旧结果；本轮未独立执行。 |
| 地区数据统计 <br>`/trade/province-stat` | 日期、支付方式、地区、商户口径 | 查询/重置、地图/表格、加载失败、CSV | ProvinceStatisticsReport.vue<br>internal/server/reports.go | order-Cn9IuGiH.js; order-B-UR7CeV.js | 地图动态模块具备加载/错误反馈；IP归属地未知不虚构。相关加载回归本轮通过。 |
| 结算记录 <br>`/settle/list` | 申请号、商户、金额、费用、净额、收款版本、审批/付款状态 | 筛选/分页/统计/CSV、申请、批准/拒绝、原净额准备/发送/查询、取消未发送 | WorkflowPage.vue; SettlementRecordDetails.vue; SettlementPayoutPanel.vue<br>internal/server/backoffice.go; funds_settlement_payout.go | settle-C3-SgkfR.js; settle-DoI_EC2g.js | 审批不等于付款；收款不可变版本与原资金冻结保留。真实出款和原Pro隐藏费用规则未逐项联调。 |
| 结算设置 <br>`/settle/settings` | 结算方式、账号/姓名、银行信息、留存余额、资料版本 | 读取脱敏资料、编辑/取消/保存、版本冲突和未知核对 | SettlementRecipientPage.vue; SettlementRecipientPanel.vue<br>internal/server/backoffice.go | account-DkHvjhqn.js | 收款快照与留存余额独立保存；完整私密资料不回显。 |
| 申请提现 <br>`/settle/apply` | 申请号、商户、金额、费用、净额、收款版本、审批/付款状态 | 筛选/分页/统计/CSV、申请、批准/拒绝、原净额准备/发送/查询、取消未发送 | WorkflowPage.vue; SettlementRecordDetails.vue; SettlementPayoutPanel.vue<br>internal/server/backoffice.go; funds_settlement_payout.go | settle-C3-SgkfR.js; settle-DoI_EC2g.js | 审批不等于付款；收款不可变版本与原资金冻结保留。真实出款和原Pro隐藏费用规则未逐项联调。 |
| 代付管理 <br>`/transfer` | 通道、付款产品、收款账号/姓名、金额、手续费、银行编码、备注、请求号 | 列表/筛选/分页/统计/CSV、能力读取、准备/确认付款、查原单/凭证 | TransferPage.vue; TransferListFilters.vue; TransferListTable.vue<br>internal/server/transfer_http.go | transfer-DQfSWUDt.js | 不可把prepared/pending/unknown显示为到账；以分和十进制字符串保持精度。真实资金与未接入插件未执行。 |
| 资金明细 <br>`/record` | 商户、操作类型、金额、余额、订单引用、日期 | 只读筛选/分页/统计/CSV、引用核对 | App.vue; LedgerRecordsTable.vue; LedgerSummaryDialog.vue<br>internal/server/list.go; ledger_summary.go; ledger_reporting_projection.go | user-C5COc9Vu.js | 不提供账本任意修改/删除；账务正确性由主任务后端审查与对账验证。 |
| 授权域名 <br>`/domain` | 商户、域名、审核/启用、日期 | 新增/编辑/审核/启停/删除/批量、筛选/分页 | ResourcePage.vue<br>internal/server/backoffice.go | user-C5COc9Vu.js | 商户修改需重新审核；通用表单慢写期间锁定已新增回归，相关域名测试本轮通过。 |
| 交易投诉 <br>`/complain` | 投诉号/订单、商户/通道、状态、日期、内容/证据 | 筛选/详情、渠道能力反馈、处理动作/原结果核对 | ComplaintsPage.vue<br>internal/server/complaints_console.go | complain-BSfav2Sa.js | 外部投诉插件继续暂缓；不可把本地处理备注当已回复上游或已退款。 |
| 进件商户管理 <br>`/applyments/merchant` | 商户归属、主体/联系资料、状态、审核意见/备注 | 新建/编辑/列表/详情、本地提交审核、审核和历史备注 | ResourcePage.vue; CasePanel.vue<br>internal/server/backoffice.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 本地已实现；上游submit/query/contract/upload/checkBankCard等外部动作继续暂缓，不伪造完成。隐藏表单直达也受schema权限限制。 |
| 新增商户进件 <br>`/applyments/create` | 商户归属、主体/联系资料、状态、审核意见/备注 | 新建/编辑/列表/详情、本地提交审核、审核和历史备注 | ResourcePage.vue; CasePanel.vue<br>internal/server/backoffice.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 本地已实现；上游submit/query/contract/upload/checkBankCard等外部动作继续暂缓，不伪造完成。隐藏表单直达也受schema权限限制。 |
| 商户管控记录 <br>`/applyments/mchrisk` | 商户、通道、风险类型/状态、时间、说明 | 查询/详情与schema允许的本地动作 | ResourcePage.vue<br>internal/server/backoffice.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 上游管控查询/申诉/通知设置暂缓；不得将本地记录当上游已处理。 |
| 商户进件表单 （隐藏直达）<br>`/applyments/form` | 商户归属、主体/联系资料、状态、审核意见/备注 | 新建/编辑/列表/详情、本地提交审核、审核和历史备注 | ResourcePage.vue; CasePanel.vue<br>internal/server/backoffice.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 本地已实现；上游submit/query/contract/upload/checkBankCard等外部动作继续暂缓，不伪造完成。隐藏表单直达也受schema权限限制。 |
| 分账规则 <br>`/profitsharing/receiver` | 商户、通道、接收方类型/账号、比例、最大比例、启用状态 | 接收方草稿/添加/删除、规则读取/保存/启停、管理员跨商户规则总览 | ChannelSharingPage.vue; AdminChannelSharingRules.vue; ChannelSharingRuleList.vue<br>internal/server/channel_sharing_console.go; channel_sharing_overview_console.go | profitsharing-YCDzcmdP.js; profitsharing-DjO4Lt6k.js | 支付宝/微信渠道分账与站内分账分开；真实远端接收方注册和分账未执行，其他插件继续暂缓。 |
| 分账记录 <br>`/profitsharing/order` | 原订单、商户/通道、分账请求号、回退号、金额、状态 | 统计/筛选/分页、提交/查单、完结、回退及原请求查询 | ChannelSharingPage.vue; ChannelSharingOrderList.vue; ChannelSharingFinishPanel.vue<br>internal/server/channel_sharing_console.go; channel_sharing_finish_console.go; channel_sharing_return_console.go | profitsharing-YCDzcmdP.js; profitsharing-DjO4Lt6k.js | 未知分账/回退只核对原请求。远端资金操作未执行；不可计作全部上游联调通过。 |
| 支付通道 <br>`/subchannel` | 商户、父通道/子商户号、状态、权重、限额/费率 | 筛选/分页/详情、受限配置、启停、统计/测试入口 | SubchannelPage.vue<br>internal/server/subchannel_console.go | applyments-BPI27AXd.js; applyments-DQ6qdjlw.js | 运行策略支持与原Pro全部进件插件不同；远端开户/签约暂缓，不能把资料绑定视为完成。 |
| 聚合收款 <br>`/onecode` | 收款名称、金额/可变金额、样式、方式、启用 | 创建/编辑/停用、生成链接/二维码、下载/复制 | CollectionGeneratorPage.vue; CollectionCodePanel.vue<br>internal/server/collection_generator.go; collection.go | onecode-DIYKuVAe.js; account-DkHvjhqn.js | 二维码生成不等于收款；只支持已实现浏览器支付产品。 |
| 邀请返现 <br>`/invite` | 邀请链接/令牌、受邀商户、奖励/政策版本、日期 | 复制/轮换链接、查询成员/奖励、队列/政策说明 | ReferralPage.vue; ReferralCurrentPolicyCard.vue; ReferralDailyQueue.vue<br>internal/server/referral_console.go; funds_referral_policy.go | User/invite | 当前政策与实际收益分开；未知手续费/成本不虚构利润或奖励。 |

## Go补充入口（管理10、商户5）

| 入口 | 字段 | 动作 | 前端 / 后端证据 | 原Pro动作证据 | 状态/差异 |
|---|---|---|---|---|---|
| 管理：易支付迁移 <br>`/system/migration` | 来源文件/格式、预检查结果、原任务、导入计数 | 上传预检查、确认导入、查询原任务、未知禁止重传 | MigrationWizard234.vue<br>internal/server/migration_wizard234.go | Go补充入口 | 本轮未导入真实数据；兼容范围需以迁移契约逐字段证明。 |
| 管理：商户消息提醒 <br>`/merchant/notice` | 商户、通知渠道、事件、接收目标、版本 | 读取/编辑/取消/保存、事件读取 | MerchantNoticeSettings.vue; MerchantNoticeEvents.vue<br>internal/server/merchant_notice_console.go | account-DkHvjhqn.js | 保存偏好不代表真实送达；邮件短信/机器人外部派送未执行。 |
| 管理：订单打印配置 <br>`/system/printing` | 打印机/模板、商户偏好、事件、原请求 | 设置读取/编辑/确认、打印意图/查询/恢复未发送 | PrintingSettingsPage.vue; OrderPrintingPanel.vue<br>internal/server/printing_console.go | account-DkHvjhqn.js; order-B-UR7CeV.js | 外部实体打印暂未开放；模拟受理不等于实体打印。 |
| 管理：结算计划 <br>`/settle/plans` | 范围/时间、费用/收款版本、授权额度/期限、计划版本 | 创建/编辑/取消、签发/撤销授权、安排/查询运行 | SettlementPlansPage.vue; SettlementAutoAuthorization.vue<br>internal/server/funds_settlement_plan.go | Go补充入口 | 自动结算与自动付款授权分开；本轮不签发生产授权。 |
| 管理：邀请返现政策 <br>`/system/referral-policies` | 全局/组范围、启用、比例BPS、版本、继承 | 读取/编辑/确认授权/保存、原版本冲突核对 | ReferralPolicyPage.vue; MembershipReferralPolicy.vue<br>internal/server/funds_referral_policy.go; membership_referral_policy.go | Go补充入口 | 当前版本只影响后续业务；不追发旧购买奖励。 |
| 管理：创建红包 <br>`/transfer/red` | 通道、金额、份数、名称/备注、原请求号、状态 | 创建/查询原红包、领取链接/历史 | RedpackPage.vue<br>internal/server/redpack_http.go | transfer-DQfSWUDt.js | 真实红包/付款未执行；未知结果不能创建替代付款。 |
| 管理：批量付款 <br>`/transfer/batches` | 通道、收款行/CSV、总额、批次/原请求、逐项状态 | 导入校验、准备、执行未发送、查询、取消未发送 | TransferBatchPage.vue; TransferBatchImport.vue; TransferBatchDetails.vue<br>internal/server/transfer_batch_console.go | transfer-DQfSWUDt.js | 1–100项持久批次；未知/已发送项只查询，不重发。 |
| 管理：订单名称限制策略 <br>`/system/order-subject-policies` | 作用域、商户/通道、标题模板、金额区间、版本 | CRUD配置、占位符与区间校验 | ResourcePage.vue<br>internal/server/parent_channel_subject.go; backoffice.go | Channel/ordername | 新首次发送固定上游标题；原订单名称/签名不更改。 |
| 管理：站内分账审批 <br>`/profitsharing/internal-orders` | 原分账请求、来源订单、接收方、金额、状态 | 列表/详情、批准/拒绝、原请求核对 | WorkflowPage.vue<br>internal/server/backoffice.go | Go补充入口 | 只处理站内账本分账；非支付宝/微信远端分账。 |
| 管理：保证金审批 <br>`/merchant/deposit-review` | 保证金金额、有效要求、释放窗口、申请/审批状态 | 缴纳、释放申请、管理员批准/拒绝、历史核对 | CommercePage.vue<br>internal/server/backoffice.go | User/deposit | 功能关闭仍可读历史；真实资金和上游支付未执行。 |
| 商户：消息提醒设置 <br>`/account/notice` | 商户、通知渠道、事件、接收目标、版本 | 读取/编辑/取消/保存、事件读取 | MerchantNoticeSettings.vue; MerchantNoticeEvents.vue<br>internal/server/merchant_notice_console.go | account-DkHvjhqn.js | 保存偏好不代表真实送达；邮件短信/机器人外部派送未执行。 |
| 商户：订单打印设置 <br>`/account/printing` | 打印机/模板、商户偏好、事件、原请求 | 设置读取/编辑/确认、打印意图/查询/恢复未发送 | PrintingSettingsPage.vue; OrderPrintingPanel.vue<br>internal/server/printing_console.go | account-DkHvjhqn.js; order-B-UR7CeV.js | 外部实体打印暂未开放；模拟受理不等于实体打印。 |
| 商户：自动结算参与设置 <br>`/account/settlement-schedule` | 参与状态、收款版本、授权同意 | 读取/修改/取消/确认参与 | SettlementSchedulePage.vue<br>internal/server/settlement_plan_console.go | Go补充入口 | 收款版本变化需重新明确确认，旧同意不覆盖新账户。 |
| 商户：创建红包 <br>`/transfer/red` | 通道、金额、份数、名称/备注、原请求号、状态 | 创建/查询原红包、领取链接/历史 | RedpackPage.vue<br>internal/server/redpack_http.go | transfer-DQfSWUDt.js | 真实红包/付款未执行；未知结果不能创建替代付款。 |
| 商户：批量付款 <br>`/transfer/batches` | 通道、收款行/CSV、总额、批次/原请求、逐项状态 | 导入校验、准备、执行未发送、查询、取消未发送 | TransferBatchPage.vue; TransferBatchImport.vue; TransferBatchDetails.vue<br>internal/server/transfer_batch_console.go | transfer-DQfSWUDt.js | 1–100项持久批次；未知/已发送项只查询，不重发。 |

## 全局逻辑审查与限制

- 金额/标识：资金模块使用十进制字符串和整数分；`validAmount`、`fundsAmountCents212`、`centsMoney`、`consoleValue`等负责校验与显示。统计CSV保留字符串。未将全仓每个算术分支静态扫描等同全金额边界测试。
- 分页/筛选：列表分页回落使用`listPageWithinTotal`；TransferPage使用appliedFilters。通用ReportPage发现的草稿/结果错配已修复。其余各专用列表的每种筛选组合仍需要主任务全量测试，不能由该修复推断全部已通过。
- 取消/清空：遍历Vue模板检查form/KitModal下缺失type的按钮，所见直接按钮均为提交动作；取消/清除按钮按type=button处理。专用凭据的保持/替换/清除与空值白名单不能统一改成“空即删除”。
- 权限：menuRole缺失管理员等级按operator、navigationFor/pageAuthorizedFor按角色/菜单grant与商户feature过滤，隐藏直达表单仍检查schema权限；后端才是权限边界。未把菜单隐藏当防越权测试；主任务后端代理负责实际HTTP越权验证。
- 系统设置17分类：字段由settings.ts与专用schema组成；站点/商户/支付/结算/通知可清空项走消费者白名单，密钥通过专用加密接口，后台路径/缓存驱动/IP代理信任等部署项只读。实名/OCR已按追加需求实现（internal/adapters/kyc/provider.go、cloud.go、ocr.go及server233），本地live验证配置持久化/遮罩/编辑/清除，Go替身契约覆盖供应商协议和图像边界；真实身份与外部收费服务未联调。其他未接入插件/第三方动态安装仍暂缓，不能扩大为实名/OCR未实现。历史2026-11-02对照报告仅作线索，本轮未访问报告中的生产站，也不复用其通过数。
- 上游不可证：本地API模块列出了fillorder、setStatus、refund、contract/upload、complete等原动作名，动作同名并不能证明隐藏状态机/权限/并发一致。Go保留查原请求、可信资金证据、会话及版本约束，不复制不可核实的任意状态修改。

## 最终验证与未执行范围

详细修复、分包回执来源和未执行边界见[系统复审报告](system-review-2026-11-12.md)。本表是逐入口功能索引，不能把 82 个原菜单和 15 个补充入口当作 97 条完整 CRUD 流程通过声明。

| 验证 | 最终结果 |
| --- | --- |
| Go 普通回归 | 79 包、454 批、3,219 根通过、43 根条件跳过，零失败及隐藏子测试跳过 |
| Linux race | 6 包、36 批、256 根通过，零失败、跳过及数据竞争 |
| 前端单元/组件 | 2,043/2,043 通过，类型检查及构建通过 |
| 模拟浏览器 | 748/748 通过，零失败、跳过、flaky；桌面与窄屏 |
| 本地实际 HTTP 与浏览器 | 19 个文件、52/52 通过，每文件 33 项业务/账务核对零异常 |
| 独立 opt-in | 15/15 根及 4/4 支付页根通过，主批次对应跳过保留 |
| 支持检查 | go vet、双平台构建、Linux 专属策略、SDK、文档和 OpenAPI 检查通过 |

最终 Go 输入清单 SHA：`606c134023021140a21300875510a47258232dad35ace20b38c59389e6cd3e73`；前端输入 SHA：`d72a285c3e3019cc3d41947c35b9cf741cd191b9d9f403d08161513b7da40a59`。最终运行分别位于本地 `.tools/system-review/go-suite-final-evidence`、`race-suite-final-evidence`、`web-finalcheck`、`mock-finalcheck`、`live-v8`、`remaining-optins-final-evidence` 及 `payment-browser/*-final-evidence-v1`。发布门禁已核验当前输入、原始日志、根测试唯一覆盖与独占数据库零连接清理。

早期失败和中断结果保留，不改记通过。测试夹具的精确文本定位、本地 TLS 证据、邮件频率入口及 Chromium 日期时间规范化修正，没有放松资金、归属、签名、原请求幂等、保存/取消和持久化断言。模拟静态服务器的只读 503 诊断不算实际 API 成功；实际 API 以独立本地流程结果为依据。

43 根主批次跳过中，19 根已独立执行，16 根 PHP 互操作因受限运行时不可用未执行，8 根预览/语料/导出工具未执行。未解密或执行原加密控制器，不能证明隐藏状态机全面等价。未发起真实资金交易，未新建容器；实体打印、真实邮件/短信/支付 App 与暂缓外部插件不属于本轮已联调范围。
