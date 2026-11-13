# USDT Payment Channels Implementation Plan

**Goal:** 将 USDT 收款纳入统一支付方式、支付通道、商户授权与用户组规则，同时保留独立资产账本。

**Architecture:** 通道目录使用持久 ID 和修订，链上通道按能力在现有通道列表显示。收款事务验证通道、商户与策略并冻结原单信息，随后由 cryptopay 执行；人民币 Provider 不承接 USDT 入账。

**Tech Stack:** Go、PostgreSQL、Vue、TypeScript、现有 Playwright 模拟测试。

**Spec:** [审查报告](../../reviews/usdt-payment-channel-review.md)

## Global Constraints

- 主网收付款保持关闭；无真实资金交易，不新建容器。
- 使用现有测试 PostgreSQL 的独占数据库，保留当前预览及历史证据。
- 不修改已发布迁移；新增 000200_crypto_payment_channels.up.sql。
- 不合并人民币与 USDT 金额，不重绑历史订单。

## 1. 持久通道与收款准入

Owner: core agent. Files: 新迁移、internal/cryptopay/channels*.go、model.go、invoices.go、相关测试。

- [x] 测试先覆盖禁用、商户未授权、组禁用、显式通道冲突、USDT 限额、组费率及原单重放。
- [x] Channel 保存 id/revision/network/name/enabled/archived/fee_bps、六位 USDT 限额及商户授权；使用共享目录锁分配 ID。
- [x] SaveChannel(ctx, SaveChannelInput) 校验完整输入、预期修订和请求号；重放不得变更原请求。
- [x] AvailableChannels(ctx, merchantID, network) 只返回同时满足部署、支付类型、通道、商户和组权限的通道。
- [x] RequireChannels 的网站服务在 CreateInvoice 事务内执行准入并保存通道及费率快照；既有未绑定记录保持历史语义。
- [x] WithAuthority 组合而非覆盖已有身份验证，避免新增策略消除会话约束。

## 2. 管理接口与共享目录

Owner: console agent. Files: internal/server/crypto_channels.go、parent_channel_console.go、backoffice/group/pay-type 目录、cmd/api/crypto_configured.go。

- [x] 在修改前验证 USDT 目录和权限用例失败。
- [x] /payment-channels GET 合并 asset=USDT 的能力区分 DTO，不构造人民币 Provider。
- [x] POST /payment-channels/crypto 与 PATCH /payment-channels/crypto/{id} 使用本人超级管理员、CSRF、修订、请求幂等及审计。
- [x] USDT 支付类型默认关闭，组设置支持继承、关闭、固定通道、首个通道；不支持的轮询和子通道明确拒绝。
- [x] 新增通道与人民币通道共享 ID 分配锁并排除已有编号；加载时拒绝跨资产 ID 冲突。
- [x] cmd/api 加载的 crypto 服务强制 RequireChannels=true。

## 3. 三种收款入口统一

Owner: root. Files: internal/server/crypto_compatibility.go、crypto_console.go、cashier*.go、backoffice/policy.go、相关回归测试。

- [x] 红色用例：签名请求的错误 channel_id 不得被忽略；控制台收款受商户业务限制；关闭时可用通道列表为空。
- [x] 兼容入口严格解析通道 ID，拒绝子通道，固定已有订单的通道和网络；新单选择真实授权通道。
- [x] 控制台提交 channel_id，GET /crypto/channels 仅返回本人或管理员指定商户的安全通道摘要，费率按有效组计算。
- [x] CheckPaymentTx 在收款事务内检查共享业务规则；与本人会话验证叠加。
- [x] 收银台展示可用 USDT，选择后进入同一准入；已创建 USDT 原单继续原单查证。

## 4. 前端统一入口

Owner: frontend agent. Files: web/src/components/ParentChannelPage.vue、新 USDT 通道编辑组件、pay-types/group-payment-rates/menu/App、CryptoFundsPage 及测试。

- [x] 使用严格区分的 CNY/USDT 通道 DTO，USDT 不显示人民币付款和测试支付操作。
- [x] 支付通道内新增和编辑 USDT 的链、名称、状态、商户授权、费率及明确 USDT 单位的限额。
- [x] 原币收款改为选择真实可用通道，带入网络和有效费率，失败不回退自由选链。
- [x] 从订单管理进入 USDT 收款单，详情展示原通道及网络；独立资产页保留资金操作。
- [x] 单元与浏览器测试覆盖保存/取消/错误/关闭状态、长字段及手机宽度。

## 5. 集成验收

- [x] 运行 Go 受影响包与新迁移回归、前端全量单元/构建、模拟浏览器流程。
- [x] 本地模拟两链收款与回调，确认人民币账本不变、USDT 原链退款/结算不回归。
- [x] 独立复核准入、币种、身份、并发修订和请求未知状态。
- [x] 记录实际通过/跳过/失败及源代码版本；不得使用旧报告冒充本轮结果。

用户已授权实施，按上述分工直接执行，不在阶段间请求重复确认。
