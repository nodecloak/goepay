# 支付宝银行卡转账实施计划

> **For agentic workers:** 按任务红绿实施；父任务协调独立审阅。

**Goal:** 为同一个支付宝插件提供显式启用的对私银行卡转账，并按可验证证据处理金额和晚退票。

**Architecture:** 现有预占、T+1和持久任务不变；新支付宝适配器分支和独立领域解析，31持久化产品/原提交绑定及观察。未知不重发，证据不足不释放资金。

**Tech Stack:** Go、PostgreSQL、现有支付宝证书验签。

**Spec:** `docs/design/alipay-bank-transfers.md`。

## Global Constraints

- 无生产/真实交易；测试使用临时证书、TLS假上游与本地PG。
- 不改冻结迁移；31归本增量；30归mini程序。
- 不复用微信BANK_FAIL/cmms_amt语义；order_fee只是预计，不进入实际账务。
- 默认enable_bank_transfer=false；当前仅证书模式；银行编码不使用微信枚举。

## Tasks

- [x] 适配器红测：默认关闭、参数/证书前置校验、签名请求产品/身份、缺金额和晚REFUND证据。
- [x] 实现配置/能力、支付宝银行卡请求及查询。
- [x] PG红测并实现原提交绑定、状态单调、持续只读对账、幂等反向凭证和证据不足pending。
- [x] factory与HTTP能力区分银行产品；回归T+1、费用、权限、审计和微信银行原语义。
- [x] 定向真实PG/临时TLS证书测试、相关包回归/vet，并通知父任务冻结审阅。

## 最终验证

- 8包完整回归的资金相关7包通过；最后一轮contract中支付宝native页面/scan页面两个非银行断言失败，已交父任务处理，其后父任务报告相关定向合同7.358秒通过。
- 银行/微信银行/T+1真实PG组合通过22.305秒。审阅发现正常预计费用0.00被正金额解析拒绝；实际adapter与TLS+PG/RSA合同先复现红测，再将仅order_fee改为ledger.Parse非负定点解析（金额正值要求不变），修复通过。
- 修复后5包银行组合：adapter1.431秒、factory1.039秒、payment14.099秒、reconciliation2.002秒、contract5.968秒均通过；相同8包fresh vet通过。
- 独立审阅再次5包AlipayBank定向通过，未发现剩余P1/P2；迁移31保持冻结，无运行库部署、无生产调用。
- 相关冻结摘要见 `docs/verification/alipay-bank-31-freeze.md`；passback_params增强经父任务决定留待后续，未混入31。
