# USDT 托管收付实施计划

> **For agentic workers:** Use subagent-driven development for the separate chain, signing and console boundaries; the primary worker implements the ledger and verifies the integrated result. Steps use checkbox syntax.

**Goal:** 实现 TRON、Ethereum 原生 USDT 收款、独立账本、隔离签名、归集、退款和原链结算。

**Architecture:** cryptopay 以 PostgreSQL 事务维护订单和资产账本，cryptochain 仅访问明确配置的节点。crypto-signer 是独立 mTLS 服务，持有密钥和持久化出款策略；网站不能取得私钥。两链分别计账，人民币余额不参与。

**Tech Stack:** Go、PostgreSQL、Vue、现有 Chromium 测试；成熟 secp256k1 实现。

**Spec:** 本会话已确认的平台代收、原链结算方案。链上资金操作只在本地替身验证；不访问生产、不发起真实资金交易、不新建容器。

## Global Constraints

- 复用 goepay-full195-windows-b378c4a3 的 loopback PostgreSQL，自有独占数据库；确认零连接后删除。
- Go 文件使用职责命名；不新增带阶段数字的 Go 文件。
- 金额使用整数原子单位，最多 6 位 USDT 小数；不使用浮点。
- 网络、代币合约、地址和报价在创建后固定；过期地址不分配给新订单。
- 未知转出结果保持预占，只核对原签名交易；退款不能默认退给发送地址。
- TRON 固化成功回执和 Ethereum finalized 成功回执才入账。

## Task 1: 独立资产事务

Files: `db/migrations/000199_crypto_custody.up.sql`, `internal/cryptopay/{model,money,ledger,invoices,withdrawals,scanner,execution}.go` 及对应测试。

- [x] 红测：同金额并发订单不同地址；重复链事件只入账一次；旧CNY账户不变。
- [x] 实现独占地址分配、独立network账户、不可变journal/entry和余额非负约束。
- [x] 红测：少付/补款、多付、迟到、错币、重组和节点分歧不能错误完成订单。
- [x] 实现最终事件摄入、过期异常、持久扫描游标和查询。
- [x] 红测：并发退款/结算不得超支，撤销后不可广播，未知交易不得生成替代付款。
- [x] 实现预占、审批、原交易身份、归集与出款核对；逐项验真资金分录。

## Task 2: 链适配器

Files: `internal/cryptochain/**`。

- [x] 写本地 HTTP 协议测试，拒绝错网络、伪币、失败/非最终回执。
- [x] 实现 TRON 固化块扫描、TRC20 事件、Prepare/Broadcast/Lookup。
- [x] 同样实现 Ethereum chainid、finalized、ERC20 日志与 legacy EIP155 交易。
- [x] 验证请求超时、扫描边界、事件索引和重复响应。

## Task 3: 隔离签名

Files: `internal/cryptosigner/**`, `cmd/crypto-signer/**`。

- [x] 红测：篡改地址/代币/金额/交易内容、无认证、重复请求不同内容、重启限额绕过。
- [x] 实现 mTLS 服务与客户端、严格交易解码、成熟 secp256k1 签名、持久幂等及日限额。
- [x] 限制只允许 USDT transfer、明确收款白名单和费用上限；没有任意消息签名入口。

## Task 4: 管理端、商户端与收银台

Files: `internal/server/crypto*.go`, `web/src/components/CryptoFundsPage.vue`、菜单与启动接线。

- [x] 红测：商户越权、管理员代登录资金写、CSRF、密码核验与撤权竞争。
- [x] 实现分链余额、订单、地址池、退款、结算及管理员审批；金额与网络清晰展示。
- [x] 实现公开令牌收银台、完整居中二维码、地址/金额复制和状态轮询。
- [x] 易支付兼容金额保留原 CNY 语义；原币 USDT 使用独立字段，不调用人民币入账函数。

## Task 5: 实际组合验收

- [x] TRON 本地节点替身 → 收银台 → 最终确认 → 入账 → 归集 → 退款 → 原链结算全链路。
- [x] Ethereum 同等验收；两链同商户余额及出账隔离。
- [x] 重启恢复、重复广播、签名不可用、手续费不足、SQL并发、回调失败重试。
- [x] 运行新增全套测试、受影响Go/前端回归、类型检查、构建、并发检测；记录真实通过/跳过/限制。

采用已获授权的会话内实施，不另行等待计划确认。本任务不自动发布主网通道。
