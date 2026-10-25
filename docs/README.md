# GOEPAY 重构设计文档

日期：2026-10-23。有效工作目录：`E:\GOEPAY`。本目录同时保留原始设计基线和后续实施、验证资料。

后续实施已开始，当前进展以 [实施状态](implementation-status.md)、[测试结果](test-results.md) 及 [服务器通道移植记录](testing/server-channel-import-2026-10-23.md) 为准；以下设计资料保留原始规划基线。

建议阅读顺序：

1. [总体设计与落地规划](design/2026-10-23-goepay-design.md)
2. [接口兼容规范与差异清单](design/2026-10-23-compatibility.md)
3. [完整菜单与功能验收索引](baseline/feature-index.md)
4. [分阶段实施计划](superpowers/plans/2026-10-23-goepay-rewrite.md)
5. [静态盘点说明](baseline/README.md)
6. [实施决策与运行约定](design/implementation-decisions.md)
7. [菜单级实施映射](baseline/acceptance-map.csv)（不代表所有原版操作验收通过）
8. [已实施接口约定](design/api-conventions.md)
9. [后台 OpenAPI 文档](../api/README.md)

**当前范围：**微信、支付宝支付插件已进入实施和本地协议验证；用户已明确其他插件暂缓。缺失的同插件产品和原版业务语义仍如实列入实施状态，不能宣称已恢复所有加密逻辑或完全兼容。查看 [两插件方法映射](design/two-plugin-methods.md)、[资金规则](design/payment-fees-ledger.md)、[转账规则](design/provider-transfers.md) 和 [迁移说明](migration/RUNBOOK.md)。
