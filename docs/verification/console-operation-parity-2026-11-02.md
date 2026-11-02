# 管理端与商户端完整操作复查（2026-11-02，227）

本轮将页面检查延伸到实际填表、提交、取消、刷新、再次编辑、状态切换和删除结果。参考站 `p.ma3fu.com` 只读取表单和操作入口；全部写入使用现有本地模拟 PostgreSQL 的逐文件独立数据库。没有在两个生产站提交业务配置、权限、通知或资金操作。

## 本轮修复

- 管理员由受限角色改为超级管理员时，提交空的菜单授权集合，避免旧权限列表与新角色冲突；原受限角色草稿仍保留，后端权限规则继续校验。
- 支付方式和通用新增表单在请求已经送达、响应丢失时锁住原提交，提供关闭并回读列表；不会因再次点击保存重复新增。明确的业务校验错误仍可修改后提交。
- 邀请码、授权域名、公告和收款码样式删除末页最后一条时，回到有效页并重新读取，避免误以为其余记录丢失。
- 新增收款码样式后将新 ID 写入当前编辑地址，刷新仍能编辑原记录；自身地址更新保留已保存反馈和草稿。
- 共用开关组件以父组件接受的实际状态显示。通道启停取消确认后恢复原值；异步提交期间不假显示已生效。

## 参考站字段与交互

本轮实际展开新增商户、商户编辑、用户组、支付方式新增及编辑、轮询组新增及通道配置、邀请码生成、授权域名、管理员和黑名单表单，读取公告、账户设置、结算设置和聚合收款配置。只采集字段名与动作名，不保存字段值、密钥或收款资料；全部取消或直接离开，没有点击生产保存、确认删除或执行资金操作。

| 表单 | 对照字段及交互 |
| --- | --- |
| 新增商户 | 手机号/邮箱登录账号、登录密码、用户组、QQ、网站域名、结算方式/账号/姓名、手续费模式、商户/支付/结算权限；取消后无创建 |
| 用户组 | 名称、变量、结算周期与手续费、代付、保证金、风控、金额与日限额、合单及功能权限；配置费率与路由独立保存 |
| 支付方式 | 调用值、显示名称、支持设备；新增、编辑、状态切换、删除及取消 |
| 通道/轮询 | 授权来源、名称、分成、接口资料、轮询方式、通道与权重；启停确认/取消、归档后保留账户历史 |
| 邀请码/域名 | 生成个数、批量清理；商户ID、域名及审核状态；商户编辑后需重新审核 |
| 管理员 | 用户名、权限等级、菜单授权、登录及支付密码、二次验证；角色变更后重载确认 |
| 公众号小程序 | 类别、名称、APPID、APPSECRET、说明；密钥保留编辑与归档，未触发生产授权 |
| 黑名单 | 类型、内容、有效期、备注；前端验证、持久化编辑和删除 |
| 公告 | 公告内容、排序、文字颜色；商户可见、隐藏、编辑与删除 |
| 商户账户 | 联系QQ、密钥登录、退款API、最大/最小支付金额、手续费承担方；取消及清空可选值 |
| 商户结算 | 方式、收款账号/姓名、预留余额；收款账户版本与预留余额分别保存，不回显完整私密资料 |
| 聚合收款 | 收款方名称、样式、金额；创建/编辑/禁用后公开链接失效 |

参考站当前商户只开放部分菜单；未把未显示节点描述为已在该账户逐一对照。邀请码列表在参考站曾出现网络异常，只读取了生成确认表单。Go 额外节点通过本地实际流程和对应接口/组件回归验证。

## 菜单与操作证据映射

表中“真实本地”指浏览器操作真实 Go API 和 PostgreSQL，不用伪造的接口成功响应；“交互+接口”指受控浏览器状态测试与独立后端契约测试配合；“读取”仅适用于本来不提供新增/编辑/删除的报表和日志。不能把读取节点数量当作写操作通过数量。

用例简称：`node`=live-node-workflows，`business`=live-business-lifecycle，`admin`=live-admin-config-crud，`merchant`=live-merchant-config-crud，`ops`=live-operations-crud；其它简称均对应 web/e2e 下的 spec。具体操作均有断言，不以页面打开作为写入成功依据。

| 端 | 节点 | 路径 | 操作、边界与证据 |
| --- | --- | --- | --- |
| 管理端 | 仪表盘 | `/admin/dashboard` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 订单管理 | `/admin/trade/order` | 真实本地：订单菜单、取消、状态标记、冻结、删除恢复；签名支付→回调→退款→结算；node/business + order-actions |
| 管理端 | 退款记录 | `/admin/trade/refund` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 黑名单管理 | `/admin/trade/blacklist` | 真实本地：校验→新增→编辑→刷新→详情→删除；node + blacklist-parity |
| 管理端 | 每日交易统计 | `/admin/trade/stat/daily` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 商户交易统计 | `/admin/trade/stat/user` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 通道数据统计 | `/admin/trade/stat/channel` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 地区数据统计 | `/admin/trade/stat/province` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 支付用户统计 | `/admin/trade/stat/buyer` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 分账规则 | `/admin/profitsharing/receiver` | 本地模拟：跨商户归属/筛选、规则编辑、审批与分账账务；对应profitsharing/receiver/internal交互+接口回归；不派发生产分账 |
| 管理端 | 分账记录 | `/admin/profitsharing/order` | 本地模拟：跨商户归属/筛选、规则编辑、审批与分账账务；对应profitsharing/receiver/internal交互+接口回归；不派发生产分账 |
| 管理端 | 站内分账规则 | `/admin/profitsharing/internal` | 本地模拟：跨商户归属/筛选、规则编辑、审批与分账账务；对应profitsharing/receiver/internal交互+接口回归；不派发生产分账 |
| 管理端 | 站内分账审批 | `/admin/profitsharing/internal-orders` | 本地模拟：跨商户归属/筛选、规则编辑、审批与分账账务；对应profitsharing/receiver/internal交互+接口回归；不派发生产分账 |
| 管理端 | 付款记录 | `/admin/transfer/list` | 本地模拟：权限→意图→确认→查询→账务；live-transfer/business及批量/红包/内转接口和交互回归；不执行真实上游出款 |
| 管理端 | 付款账号统计 | `/admin/transfer/stat` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 通道数据统计 | `/admin/transfer/channel-stat` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 商户代付统计 | `/admin/transfer/user-stat` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 创建红包 | `/admin/transfer/red` | 本地模拟：权限→意图→确认→查询→账务；live-transfer/business及批量/红包/内转接口和交互回归；不执行真实上游出款 |
| 管理端 | 批量付款 | `/admin/transfer/batches` | 本地模拟：权限→意图→确认→查询→账务；live-transfer/business及批量/红包/内转接口和交互回归；不执行真实上游出款 |
| 管理端 | 结算管理 | `/admin/settle/list` | 真实本地：结算申请、预留、拒绝恢复、授权后原净额只付一次；business/expanded/live-settlement-authorization；计划编辑/取消/未知结果由专用回归 |
| 管理端 | 批量结算 | `/admin/settle/batch` | 真实本地：结算申请、预留、拒绝恢复、授权后原净额只付一次；business/expanded/live-settlement-authorization；计划编辑/取消/未知结果由专用回归 |
| 管理端 | 结算计划 | `/admin/settle/plans` | 真实本地：结算申请、预留、拒绝恢复、授权后原净额只付一次；business/expanded/live-settlement-authorization；计划编辑/取消/未知结果由专用回归 |
| 管理端 | 商户列表 | `/admin/merchant/merchant-list` | 真实本地：手机号/邮箱创建→独立登录→支付生命周期；node/business + 商户凭据/禁用/权限边界回归；账户账本不物理删除 |
| 管理端 | 用户组设置 | `/admin/merchant/group-list` | 真实本地：新增→费率编辑→刷新→详情→删除、响应丢失防重复；node/admin；GID0删除受保护 |
| 管理端 | 用户组购买 | `/admin/merchant/group-buy` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 资金明细 | `/admin/merchant/record` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 站内转账 | `/admin/merchant/internal-transfer` | 本地模拟：权限→意图→确认→查询→账务；live-transfer/business及批量/红包/内转接口和交互回归；不执行真实上游出款 |
| 管理端 | 授权域名 | `/admin/merchant/domain` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 管理端 | 邀请码管理 | `/admin/merchant/invite-code` | 真实本地：生成21→分页→取消/确认删除末条→回到有效页→清空；admin |
| 管理端 | 商户消息提醒 | `/admin/merchant/notice` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 管理端 | 保证金审批 | `/admin/merchant/deposit-review` | 本地模拟订单/金额/权限/回调及审批恢复；business及group-purchase/recharge/deposit专用交互+接口回归 |
| 管理端 | 支付通道 | `/admin/payapi/pay-channel` | 真实本地：双视口新增/改名/刷新、启停取消与确认、归档保留版本；parent180-test-http-browser；仅loopback TLS模拟付款/查询 |
| 管理端 | 支付方式 | `/admin/payapi/pay-type` | 真实本地：无效值不写入→新增→编辑设备/名称→刷新→切换→取消/确认删除；响应丢失只回读；admin |
| 管理端 | 支付通道轮询 | `/admin/payapi/pay-roll` | 真实本地：新增轮询草稿→权重配置→规则编辑→启用→刷新→停用；ops；保留配置历史，不物理删除 |
| 管理端 | 公众号小程序 | `/admin/payapi/pay-weixin` | 真实本地：应用草稿新增→保留密钥编辑→归档；node；授权外部调用未启用 |
| 管理端 | 进件渠道配置 | `/admin/applyments/channel` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 进件商户管理 | `/admin/applyments/merchant` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 子通道管理 | `/admin/applyments/subchannel` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 商户管控记录 | `/admin/applyments/mchrisk` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 投诉记录 | `/admin/complain/list` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 投诉统计 | `/admin/complain/charts` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 同步任务 | `/admin/complain/task` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 风控记录 | `/admin/other/risk` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 获取用户标识 | `/admin/other/get-token` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 聚合收款码样式 | `/admin/other/onecode` | 真实本地：样式上传/新增ID/刷新/编辑/末页删除；ops；商户收款码创建/改名/停用与公开链接撤销；live-collection |
| 管理端 | 应用市场 | `/admin/app/app-store` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 本地插件 | `/admin/app/plugin-list` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 模板配置 | `/admin/app/template` | 真实本地：模板编辑与保护iframe、打印偏好取消/确认/刷新；live-template-printing/merchant；不派发实体打印 |
| 管理端 | 扩展配置 | `/admin/app/addon` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 管理端 | 系统设置 | `/admin/system/settings` | 配置字段/取消/确认/刷新/冲突及未知响应专用交互+接口回归；生产不保存设置、不触发调度 |
| 管理端 | 计划任务 | `/admin/system/cron` | 读取配置；维护/任务危险动作仅测试确认、权限和服务器拒绝边界，不在生产执行 |
| 管理端 | 公告配置 | `/admin/system/anounce` | 真实本地：富文本新增→商户显示→隐藏→编辑→删除、排序颜色与分页；node/ops |
| 管理端 | 管理员列表 | `/admin/system/admin-user` | 真实本地：新增受限管理员→晋升→刷新→降级→删除→404；admin；服务端RBAC保护 |
| 管理端 | 登录日志 | `/admin/system/log` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 运行日志 | `/admin/system/runtime-log` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 管理端 | 数据清理 | `/admin/system/clean` | 读取配置；维护/任务危险动作仅测试确认、权限和服务器拒绝边界，不在生产执行 |
| 管理端 | 系统更新 | `/admin/system/update` | 读取配置；维护/任务危险动作仅测试确认、权限和服务器拒绝边界，不在生产执行 |
| 管理端 | 订单打印配置 | `/admin/system/printing` | 真实本地：模板编辑与保护iframe、打印偏好取消/确认/刷新；live-template-printing/merchant；不派发实体打印 |
| 管理端 | 邀请返现政策 | `/admin/system/referral-policies` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 管理端 | 订单名称限制策略 | `/admin/system/order-subject-policies` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 商户端 | 仪表盘 | `/merchant/dashboard` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 商户端 | API 信息 | `/merchant/account/api-info` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 商户端 | 账户设置 | `/merchant/account/settings` | 配置字段/取消/确认/刷新/冲突及未知响应专用交互+接口回归；生产不保存设置、不触发调度 |
| 商户端 | 实名认证 | `/merchant/account/certificate` | 按现有能力验证草稿/查询/权限及明确未接入反馈；对应交互+接口回归。外部进件、投诉、实名和插件执行仍按约定暂缓，不计作上游联调成功 |
| 商户端 | 余额充值 | `/merchant/account/recharge` | 本地模拟订单/金额/权限/回调及审批恢复；business及group-purchase/recharge/deposit专用交互+接口回归 |
| 商户端 | 购买会员 | `/merchant/account/groupbuy` | 本地模拟订单/金额/权限/回调及审批恢复；business及group-purchase/recharge/deposit专用交互+接口回归 |
| 商户端 | 保证金 | `/merchant/account/deposit` | 本地模拟订单/金额/权限/回调及审批恢复；business及group-purchase/recharge/deposit专用交互+接口回归 |
| 商户端 | 站内转账 | `/merchant/account/internal-transfer` | 本地模拟：权限→意图→确认→查询→账务；live-transfer/business及批量/红包/内转接口和交互回归；不执行真实上游出款 |
| 商户端 | 消息提醒设置 | `/merchant/account/notice` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 商户端 | 订单打印设置 | `/merchant/account/printing` | 真实本地：模板编辑与保护iframe、打印偏好取消/确认/刷新；live-template-printing/merchant；不派发实体打印 |
| 商户端 | 自动结算参与设置 | `/merchant/account/settlement-schedule` | 真实本地：结算申请、预留、拒绝恢复、授权后原净额只付一次；business/expanded/live-settlement-authorization；计划编辑/取消/未知结果由专用回归 |
| 商户端 | 订单管理 | `/merchant/trade/order` | 真实本地：订单菜单、取消、状态标记、冻结、删除恢复；签名支付→回调→退款→结算；node/business + order-actions |
| 商户端 | 退款记录 | `/merchant/trade/refund` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 商户端 | 每日交易统计 | `/merchant/trade/daily-stat` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 商户端 | 地区数据统计 | `/merchant/trade/province-stat` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 商户端 | 结算记录 | `/merchant/settle/list` | 真实本地：结算申请、预留、拒绝恢复、授权后原净额只付一次；business/expanded/live-settlement-authorization；计划编辑/取消/未知结果由专用回归 |
| 商户端 | 结算设置 | `/merchant/settle/settings` | 真实本地：结算申请、预留、拒绝恢复、授权后原净额只付一次；business/expanded/live-settlement-authorization；计划编辑/取消/未知结果由专用回归 |
| 商户端 | 申请提现 | `/merchant/settle/apply` | 真实本地：结算申请、预留、拒绝恢复、授权后原净额只付一次；business/expanded/live-settlement-authorization；计划编辑/取消/未知结果由专用回归 |
| 商户端 | 单笔付款 | `/merchant/transfer` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 商户端 | 创建红包 | `/merchant/transfer/red` | 本地模拟：权限→意图→确认→查询→账务；live-transfer/business及批量/红包/内转接口和交互回归；不执行真实上游出款 |
| 商户端 | 批量付款 | `/merchant/transfer/batches` | 本地模拟：权限→意图→确认→查询→账务；live-transfer/business及批量/红包/内转接口和交互回归；不执行真实上游出款 |
| 商户端 | 资金明细 | `/merchant/record` | 读取查询/筛选/详情；报表、退款记录及审计不提供任意增删；live-expanded/menu-audit + 对应筛选/导出回归 |
| 商户端 | 授权域名 | `/merchant/domain` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 商户端 | 交易投诉 | `/merchant/complain` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 商户端 | 分账规则 | `/merchant/profitsharing/receiver` | 本地模拟：跨商户归属/筛选、规则编辑、审批与分账账务；对应profitsharing/receiver/internal交互+接口回归；不派发生产分账 |
| 商户端 | 分账记录 | `/merchant/profitsharing/order` | 本地模拟：跨商户归属/筛选、规则编辑、审批与分账账务；对应profitsharing/receiver/internal交互+接口回归；不派发生产分账 |
| 商户端 | 支付通道 | `/merchant/subchannel` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |
| 商户端 | 聚合收款 | `/merchant/onecode` | 真实本地：样式上传/新增ID/刷新/编辑/末页删除；ops；商户收款码创建/改名/停用与公开链接撤销；live-collection |
| 商户端 | 邀请返现 | `/merchant/invite` | 对应查询、配置/取消/确认及授权边界交互+接口回归；生产只读取，不把外部未启用动作计作成功 |

## 验收记录与限制

最终验收已完成。失败尝试保留在 `.tools/ux227-20261102` 各 round 与初轮 final 目录。首轮全量中旧开关测试假定异步状态立即生效，旧分页夹具只有一条记录却要求停留第二页；修正测试前提后重跑。round5 一次 loopback 导航连接失败也保留原始结果，不自动重试掩盖，不将失败、跳过或中断重标为通过。末轮测试不得与修改中的源码混用。

受保护的商户账户、账本、默认用户组及付款历史不做破坏性物理删除。父通道采用归档；轮询采用停用。域名原系统“审核时间”没有相应已追踪数据，仍不捏造时间。以上是明确边界，而不是遗漏的删除按钮或成功数据。


## 最终验证结果

- 前端：320 个测试文件、1,836 个单元测试通过；类型检查、构建通过。
- 浏览器：662 个受控交互用例与32个真实本地用例通过，合计694个。受控套件的32个本地专用条件跳过，均与真实本地成功用例逐名匹配，不留未执行项。
- 父通道：375/1440 两视口执行真实 Go HTTP 配置增改、启停取消/确认、归档保留账户版本；原两次支付/查询只访问 loopback TLS 模拟上游，外部请求及页面异常均为0。
- 菜单：实际读取管理端60个、商户端29个节点。87个正常加载；商户红包未开启时提示原因且创建按钮禁用；实名认证按既有约定暂缓且有明确说明。以上两种边界不计作资金操作或上游业务成功。
- 13份最终真实本地规格回执，每份33项账务一致性检查均为0。39个本轮自有测试数据库全部在零连接后清理，未创建容器、网络或卷，既有8187预览身份保持不变。
- Go、嵌入数据及迁移的2,453个输入逐一匹配原已通过版本，复用该版本3,040个测试/示例、vet和Linux构建证据；本轮额外重跑上述父通道真实HTTP浏览器测试。没有把复用证据写成重新执行后端全量。
- 独立只读代码复查无未处理P1/P2；提交前目录/凭据扫描通过。

最终前端证据位于 `.tools/ux227-20261102/final-web-v2`。实际本地采用 `selected-local-receipts.json` 明确指向的13份成功回执：12份来自 `final-live-v2`，菜单规格使用 `all-menu-final-v2` 的新回执。初版补充检查曾把实名暂缓和未开启红包当成错误；保留这两轮失败，再明确断言暂缓说明及禁用按钮后重跑，没有改写旧失败聚合。

前端源码清单 SHA-256：`7ad3b899f3353c52f151a3da7cf8cab9fe55ec9362fb7d03713d9381e2787e88`。

前端113个静态产物清单 SHA-256：`badd98d2412f5e3f25f15244b6d938a2772ef825b2df3b6a64cc0a95f37d3ff5`。

发布目标为前端 `2026.11.02-227`。本次不需要后端重启或数据库迁移；发布工具14个隔离Linux用例通过。执行时验证固定提交、包摘要、生产并发变更、原版本回滚和全部公开产物摘要，实际发布状态以 `.tools/deploy227/deployment.json` 与最终线上复核回执为准。
