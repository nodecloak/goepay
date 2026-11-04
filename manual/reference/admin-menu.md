# 管理员全部菜单

从当前菜单及补充入口同步。路径用于定位；账号权限、功能开关与已接入服务决定实际可用性。部分业务入口会按角色隐藏。

## 可见业务入口

共 61 个入口。阅读指南包含操作步骤与业务限制。

| 菜单 | 入口路径 | 阅读指南 | 范围 |
| --- | --- | --- | --- |
| 仪表盘 | `/admin/dashboard` | [操作指南](../admin/reports.md) | 按账号授权及功能条件 |
| 交易管理 → 订单管理 | `/admin/trade/order` | [操作指南](../admin/orders.md) | 按账号授权及功能条件 |
| 交易管理 → 退款记录 | `/admin/trade/refund` | [操作指南](../admin/refunds.md) | 按账号授权及功能条件 |
| 交易管理 → 黑名单管理 | `/admin/trade/blacklist` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 交易管理 → 交易统计 → 每日交易统计 | `/admin/trade/stat/daily` | [操作指南](../admin/reports.md) | 按账号授权及功能条件 |
| 交易管理 → 交易统计 → 商户交易统计 | `/admin/trade/stat/user` | [操作指南](../admin/reports.md) | 按账号授权及功能条件 |
| 交易管理 → 交易统计 → 通道数据统计 | `/admin/trade/stat/channel` | [操作指南](../admin/reports.md) | 按账号授权及功能条件 |
| 交易管理 → 交易统计 → 地区数据统计 | `/admin/trade/stat/province` | [操作指南](../admin/reports.md) | 按账号授权及功能条件 |
| 交易管理 → 交易统计 → 支付用户统计 | `/admin/trade/stat/buyer` | [操作指南](../admin/reports.md) | 按账号授权及功能条件 |
| 分账管理 → 分账规则 | `/admin/profitsharing/receiver` | [操作指南](../admin/sharing.md) | 按账号授权及功能条件 |
| 分账管理 → 分账记录 | `/admin/profitsharing/order` | [操作指南](../admin/sharing.md) | 按账号授权及功能条件 |
| 分账管理 → 站内分账规则 | `/admin/profitsharing/internal` | [操作指南](../admin/sharing.md) | 按账号授权及功能条件 |
| 转账付款 → 付款记录 | `/admin/transfer/list` | [操作指南](../admin/transfers.md) | 按账号授权及功能条件 |
| 转账付款 → 付款账号统计 | `/admin/transfer/stat` | [操作指南](../admin/transfers.md) | 按账号授权及功能条件 |
| 转账付款 → 通道数据统计 | `/admin/transfer/channel-stat` | [操作指南](../admin/transfers.md) | 按账号授权及功能条件 |
| 转账付款 → 商户代付统计 | `/admin/transfer/user-stat` | [操作指南](../admin/transfers.md) | 按账号授权及功能条件 |
| 结算管理 → 结算管理 | `/admin/settle/list` | [操作指南](../admin/settlements.md) | 按账号授权及功能条件 |
| 结算管理 → 批量结算 | `/admin/settle/batch` | [操作指南](../admin/settlements.md) | 按账号授权及功能条件 |
| 商户管理 → 商户列表 | `/admin/merchant/merchant-list` | [操作指南](../admin/merchants.md) | 按账号授权及功能条件 |
| 商户管理 → 用户组设置 | `/admin/merchant/group-list` | [操作指南](../admin/groups.md) | 按账号授权及功能条件 |
| 商户管理 → 用户组购买 | `/admin/merchant/group-buy` | [操作指南](../admin/reports.md) | 按账号授权及功能条件 |
| 商户管理 → 资金明细 | `/admin/merchant/record` | [操作指南](../admin/reports.md) | 按账号授权及功能条件 |
| 商户管理 → 站内转账 | `/admin/merchant/internal-transfer` | [操作指南](../admin/transfers.md) | 按账号授权及功能条件 |
| 商户管理 → 授权域名 | `/admin/merchant/domain` | [操作指南](../admin/merchants.md) | 按账号授权及功能条件 |
| 商户管理 → 邀请码管理 | `/admin/merchant/invite-code` | [操作指南](../admin/merchants.md) | 按账号授权及功能条件 |
| 支付接口 → 支付通道 | `/admin/payapi/pay-channel` | [操作指南](../admin/channels.md) | 按账号授权及功能条件 |
| 支付接口 → 支付方式 | `/admin/payapi/pay-type` | [操作指南](../admin/channels.md) | 按账号授权及功能条件 |
| 支付接口 → 支付通道轮询 | `/admin/payapi/pay-roll` | [操作指南](../admin/channels.md) | 按账号授权及功能条件 |
| 支付接口 → 公众号小程序 | `/admin/payapi/pay-weixin` | [操作指南](../admin/channels.md) | 按账号授权及功能条件 |
| 进件管理 → 进件渠道配置 | `/admin/applyments/channel` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 进件管理 → 进件商户管理 | `/admin/applyments/merchant` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 进件管理 → 子通道管理 | `/admin/applyments/subchannel` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 进件管理 → 商户管控记录 | `/admin/applyments/mchrisk` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 交易投诉 → 投诉记录 | `/admin/complain/list` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 交易投诉 → 投诉统计 | `/admin/complain/charts` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 交易投诉 → 同步任务 | `/admin/complain/task` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 其他功能 → 风控记录 | `/admin/other/risk` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 其他功能 → 获取用户标识 | `/admin/other/get-token` | [操作指南](../admin/risk-and-onboarding.md) | 按账号授权及功能条件 |
| 其他功能 → 聚合收款码样式 | `/admin/other/onecode` | [操作指南](../admin/appearance.md) | 按账号授权及功能条件 |
| 应用管理 → 应用市场 | `/admin/app/app-store` | [操作指南](../admin/appearance.md) | 超级管理员本人 |
| 应用管理 → 本地插件 | `/admin/app/plugin-list` | [操作指南](../admin/appearance.md) | 超级管理员本人 |
| 应用管理 → 模板配置 | `/admin/app/template` | [操作指南](../admin/appearance.md) | 超级管理员本人 |
| 应用管理 → 扩展配置 | `/admin/app/addon` | [操作指南](../admin/appearance.md) | 超级管理员本人 |
| 系统管理 → 系统设置 | `/admin/system/settings` | [操作指南](../admin/settings.md) | 超级管理员本人 |
| 系统管理 → 计划任务 | `/admin/system/cron` | [操作指南](../admin/system.md) | 超级管理员本人 |
| 系统管理 → 公告配置 | `/admin/system/anounce` | [操作指南](../admin/system.md) | 按账号授权及功能条件 |
| 系统管理 → 管理员列表 | `/admin/system/admin-user` | [操作指南](../admin/system.md) | 超级管理员本人 |
| 系统管理 → 登录日志 | `/admin/system/log` | [操作指南](../admin/system.md) | 按账号授权及功能条件 |
| 系统管理 → 运行日志 | `/admin/system/runtime-log` | [操作指南](../admin/system.md) | 超级管理员本人 |
| 系统管理 → 数据清理 | `/admin/system/clean` | [操作指南](../admin/system.md) | 超级管理员本人 |
| 系统管理 → 系统更新 | `/admin/system/update` | [操作指南](../admin/system.md) | 超级管理员本人 |
| 易支付迁移 | `/admin/system/migration` | [操作指南](../admin/migration.md) | 超级管理员本人 |
| 商户消息提醒 | `/admin/merchant/notice` | [操作指南](../admin/notifications.md) | 按账号授权及功能条件 |
| 订单打印配置 | `/admin/system/printing` | [操作指南](../admin/notifications.md) | 超级管理员本人 |
| 结算计划 | `/admin/settle/plans` | [操作指南](../admin/settlements.md) | 超级管理员本人 |
| 邀请返现政策 | `/admin/system/referral-policies` | [操作指南](../admin/settings.md) | 超级管理员本人 |
| 创建红包 | `/admin/transfer/red` | [操作指南](../admin/transfers.md) | 按账号授权及功能条件 |
| 批量付款 | `/admin/transfer/batches` | [操作指南](../admin/transfers.md) | 按账号授权及功能条件 |
| 订单名称限制策略 | `/admin/system/order-subject-policies` | [操作指南](../admin/settings.md) | 超级管理员本人 |
| 站内分账审批 | `/admin/profitsharing/internal-orders` | [操作指南](../admin/sharing.md) | 按账号授权及功能条件 |
| 保证金审批 | `/admin/merchant/deposit-review` | [操作指南](../admin/settlements.md) | 按账号授权及功能条件 |

## 从业务上下文进入的页面

这些页面不一定作为独立菜单显示，从对应列表、安全入口或表单进入。

| 页面 | 路径 | 阅读指南 |
| --- | --- | --- |
| 进件管理 → 商户进件表单 | `/admin/applyments/form` | [操作指南](../admin/risk-and-onboarding.md) |
| 其他功能 → 编辑收款码样式 | `/admin/other/onecode/edit` | [操作指南](../admin/appearance.md) |
| 修改密码 | `/admin/system/change-password` | [操作指南](../admin/system.md) |

菜单不存在时，先核对角色与功能开放条件，见[页面操作说明](../start/navigation.md)。
