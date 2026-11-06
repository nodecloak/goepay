# GOEPAY 文档索引

按你要做的事选择入口。目录里还有大量按增量编号保存的实施与验收记录，它们只反映当时的状态，阅读方法见文末。

## 部署与运维

- [生产部署指南](deployment/production.md)：从本地演示切换到真实收款所需的环境变量、源通道文件、平台密钥和上线检查。
- [可信代理与客户端 IP](design/trusted-proxy.md)：反向代理后的登录限流、验证码和风控如何取得真实来源地址。
- [默认收款通道](deployment/default-payment-channels.md)：`GOEPAY_DEFAULT_PAYMENT_CHANNELS` 的继承规则。
- [易支付迁移向导](migration/WIZARD.md) 与 [迁移操作手册](migration/RUNBOOK.md)。
- [实名认证与 OCR](operations/certification-ocr233.md)。

## 商户接入

- 接口文档由 API 在 `/doc/` 提供（源文件在 `internal/devdoc/pages/`），包括签名规则、支付、查询、退款、关单与异步通知。
- [Go SDK](../sdk/goepay/README.md)。
- [后台 OpenAPI](../api/README.md)：管理员与商户后台接口。

## 架构与设计

- [总体设计与落地规划](design/2026-10-23-goepay-design.md)、[实施决策与运行约定](design/implementation-decisions.md)、[已实施接口约定](design/api-conventions.md)。
- 资金：[费用与账本](design/payment-fees-ledger.md)、[转账](design/provider-transfers.md)、[无法自动核对的未知退款](design/unknown-refund-resolution.md)。
- [接口兼容规范与差异清单](design/2026-10-23-compatibility.md)。

## 当前状态

- [实施状态](implementation-status.md)：各模块已实现与未实现的范围。
- [功能清单](../web/capabilities.json)：后台各菜单已接入的能力；目前全部标记为 `partial`（只有列出的能力已接入）。
- [测试结果](test-results.md)：测试范围与执行记录。

## 如何阅读编号记录

`verification/`、`superpowers/plans/`、`deployment/go-ma3fu-*.md` 和 [连续实施清单](remaining-core-work.md) 是按开发增量写下的流水记录：

- “84”“固定 98”“候选 94”等是当时的增量或测试候选编号；“schema83”指当时已应用的迁移数量。
- 记录中的“已部署”“通过”只代表该次快照，不代表当前状态；当前状态以上方“当前状态”中的文档为准。
- 源码中曾经用这些编号命名的包已经改名（见下表），记录里的旧路径以对照表为准。

| 旧包路径 | 现包路径 |
| --- | --- |
| `internal/businesssms231` | `internal/businesssms` |
| `internal/captcha203` | `internal/captcha` |
| `internal/connectstate135` | `internal/connectstate` |
| `internal/kyc233` | `internal/kyc` |
| `internal/adapters/kyc233` | `internal/adapters/kyc` |
| `internal/logincaptcha205` | `internal/logincaptcha` |
| `internal/mailaccount195` | `internal/mailaccount` |
| `internal/mailpolicy194` | `internal/mailpolicy` |
| `internal/noticeconfig231` | `internal/noticeconfig` |
| `internal/noticesource231` | `internal/noticesource` |
| `internal/orderalias120` | `internal/orderalias` |
| `internal/paymentdefend206` | `internal/paymentdefend` |
| `internal/publictestpay203` | `internal/publictestpay` |
| `internal/refundevent201` | `internal/refundevent` |
| `internal/sitebrand193` | `internal/sitebrand` |
| `internal/smsgateway134` | `internal/smsgateway` |
| `internal/verifycode134` | `internal/verifycode` |
| `internal/voiceconfig230` | `internal/voiceconfig` |
| `internal/wxtransferlink231` | `internal/wxtransferlink` |

新代码的命名约定见仓库根目录的 `AGENTS.md`（不再使用编号命名包、文件和标识符）。

## 原始设计基线（2026-10-23）

以下资料保留最初的规划，可用于了解设计来源：

1. [总体设计与落地规划](design/2026-10-23-goepay-design.md)
2. [接口兼容规范与差异清单](design/2026-10-23-compatibility.md)
3. [完整菜单与功能验收索引](baseline/feature-index.md)
4. [分阶段实施计划](superpowers/plans/2026-10-23-goepay-rewrite.md)
5. [静态盘点说明](baseline/README.md)
6. [菜单级实施映射](baseline/acceptance-map.csv)（不代表所有原版操作验收通过）
7. [两插件方法映射](design/two-plugin-methods.md)
