# 商户全部菜单

从当前菜单及补充入口同步。路径用于定位；账号权限、功能开关与已接入服务决定实际可用性。部分业务入口会按角色隐藏。

## 可见业务入口

共 32 个入口。阅读指南包含操作步骤与业务限制。

| 菜单 | 入口路径 | 阅读指南 | 范围 |
| --- | --- | --- | --- |
| 仪表盘 | `/merchant/dashboard` | [操作指南](../merchant/quickstart.md) | 按账号授权及功能条件 |
| 账户中心 → API 信息 | `/merchant/account/api-info` | [操作指南](../merchant/api.md) | 按账号授权及功能条件 |
| 账户中心 → 账户设置 | `/merchant/account/settings` | [操作指南](../merchant/account.md) | 按账号授权及功能条件 |
| 账户中心 → 实名认证 | `/merchant/account/certificate` | [操作指南](../merchant/certification.md) | 按账号授权及功能条件 |
| 账户中心 → 余额充值 | `/merchant/account/recharge` | [操作指南](../merchant/services.md) | 按账号授权及功能条件 |
| 账户中心 → 购买会员 | `/merchant/account/groupbuy` | [操作指南](../merchant/services.md) | 按账号授权及功能条件 |
| 账户中心 → 保证金 | `/merchant/account/deposit` | [操作指南](../merchant/services.md) | 按账号授权及功能条件 |
| 账户中心 → 站内转账 | `/merchant/account/internal-transfer` | [操作指南](../merchant/transfers.md) | 按账号授权及功能条件 |
| 交易管理 → 订单管理 | `/merchant/trade/order` | [操作指南](../merchant/orders.md) | 按账号授权及功能条件 |
| 交易管理 → 退款记录 | `/merchant/trade/refund` | [操作指南](../merchant/orders.md) | 按账号授权及功能条件 |
| 交易管理 → 每日交易统计 | `/merchant/trade/daily-stat` | [操作指南](../merchant/reports-and-support.md) | 按账号授权及功能条件 |
| 交易管理 → 地区数据统计 | `/merchant/trade/province-stat` | [操作指南](../merchant/reports-and-support.md) | 按账号授权及功能条件 |
| 结算管理 → 结算记录 | `/merchant/settle/list` | [操作指南](../merchant/settlements.md) | 按账号授权及功能条件 |
| 结算管理 → 结算设置 | `/merchant/settle/settings` | [操作指南](../merchant/settlements.md) | 按账号授权及功能条件 |
| 结算管理 → 申请提现 | `/merchant/settle/apply` | [操作指南](../merchant/settlements.md) | 按账号授权及功能条件 |
| 代付管理 | `/merchant/transfer` | [操作指南](../merchant/transfers.md) | 按账号授权及功能条件 |
| 资金明细 | `/merchant/record` | [操作指南](../merchant/reports-and-support.md) | 按账号授权及功能条件 |
| 授权域名 | `/merchant/domain` | [操作指南](../merchant/api.md) | 按账号授权及功能条件 |
| 交易投诉 | `/merchant/complain` | [操作指南](../merchant/reports-and-support.md) | 按账号授权及功能条件 |
| 商户进件 → 进件商户管理 | `/merchant/applyments/merchant` | [操作指南](../merchant/reports-and-support.md) | 按账号授权及功能条件 |
| 商户进件 → 新增商户进件 | `/merchant/applyments/create` | [操作指南](../merchant/reports-and-support.md) | 按账号授权及功能条件 |
| 商户进件 → 商户管控记录 | `/merchant/applyments/mchrisk` | [操作指南](../merchant/reports-and-support.md) | 按账号授权及功能条件 |
| 订单分账 → 分账规则 | `/merchant/profitsharing/receiver` | [操作指南](../merchant/sharing.md) | 按账号授权及功能条件 |
| 订单分账 → 分账记录 | `/merchant/profitsharing/order` | [操作指南](../merchant/sharing.md) | 按账号授权及功能条件 |
| 支付通道 | `/merchant/subchannel` | [操作指南](../merchant/collection.md) | 按账号授权及功能条件 |
| 聚合收款 | `/merchant/onecode` | [操作指南](../merchant/collection.md) | 按账号授权及功能条件 |
| 邀请返现 | `/merchant/invite` | [操作指南](../merchant/services.md) | 按账号授权及功能条件 |
| 消息提醒设置 | `/merchant/account/notice` | [操作指南](../merchant/notifications.md) | 按账号授权及功能条件 |
| 订单打印设置 | `/merchant/account/printing` | [操作指南](../merchant/notifications.md) | 按账号授权及功能条件 |
| 自动结算参与设置 | `/merchant/account/settlement-schedule` | [操作指南](../merchant/settlements.md) | 按账号授权及功能条件 |
| 创建红包 | `/merchant/transfer/red` | [操作指南](../merchant/transfers.md) | 按账号授权及功能条件 |
| 批量付款 | `/merchant/transfer/batches` | [操作指南](../merchant/transfers.md) | 按账号授权及功能条件 |

## 从业务上下文进入的页面

这些页面不一定作为独立菜单显示，从对应列表、安全入口或表单进入。

| 页面 | 路径 | 阅读指南 |
| --- | --- | --- |
| 商户进件 → 商户进件表单 | `/merchant/applyments/form` | [操作指南](../merchant/reports-and-support.md) |

菜单不存在时，先核对角色与功能开放条件，见[页面操作说明](../start/navigation.md)。
