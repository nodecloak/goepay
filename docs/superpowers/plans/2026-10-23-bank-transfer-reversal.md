# 微信银行代付退票实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不改写已完成银行付款历史的前提下，持续核对并幂等处理已验证银行退票。

**Architecture:** 原转账和分录保持不可变；独立银行生命周期承载后续退票。通道银行费用与向商户收取的平台服务费分开记录，退票用新的平衡分录处理。证据不足只进入待核对状态，不增加商户可用余额。

**Tech Stack:** Go、PostgreSQL、pgx、现有持久任务队列；仅本地假上游与随机测试 schema。

**Spec:** 微信支付官方 V2 [查询付款银行](https://pay.wechatpay.cn/doc/v2/merchant/4011941066)、[付款到银行卡](https://pay.wechatpay.cn/doc/v2/merchant/4011939840)，2026-10-23 核对。

## 约束与状态

- 不访问生产或历史 `.secrets`，不发真实资金请求。
- 迁移 1–21 不修改，新增 22；微信零钱及支付宝终态不改变。
- 提交 `SUCCESS` 只表示受理，保持 processing 及原冻结资金。
- 查询 `SUCCESS` 校验原 TF、通道、收款卡和姓名摘要、金额、上游流水与 `cmms_amt` 后完成账务。平台服务费暂存在独立持有账户，通道手续费单独计入平台支出。
- `SUCCESS → BANK_FAIL` 是官方允许的退票路径，官方说明本金与通道费自动返还。只有身份、金额、原流水、固定通道费全部匹配，才冲回本金、原平台服务费及已记通道费用；反向事件键唯一。
- 若首次查询已是 BANK_FAIL 而本金尚未本地结算，只释放原冻结成本，不凭空记通道费用返还。
- 字段不足、费用或流水矛盾进入 reversal_pending，资金不补回，待下一次可信查询或人工核对。FAILED 不能替代已成功订单的 BANK_FAIL。
- 单独 `bank_lifecycle` 显示 reversed/reversal_pending；底层已 succeeded 的历史保持原值，API 不继续把退票当作支付成功。
- 成功后没有官方不可退票截止时间，不捏造自动最终成功。间隔六小时产生唯一下一轮查询任务；只对显式允许查询的原通道运行，不重发代付。

## 工作项

- [x] 写真实 PostgreSQL 红测：受理冻结、成功与通道费、20 路重复退票只入账一次、缺费/冲突不补钱、首次即退票、普通钱包终态不受影响。
- [x] 新增 `000022_bank_transfer_lifecycle.up.sql` 与 `internal/payment/bank_transfers.go`：独立生命周期、不可变观测与反向凭证、费用账户、原子状态与分录。
- [x] 在 `provider_transfers.go` 仅对 bank 分流，提交错误保持 unknown；读接口披露生命周期、通道费用和反向凭证。
- [x] 协议代理补 `transfer.bank_reconcile` 队列过滤、只读 handler、worker；`Store.ScheduleBankRecheck(ctx,id)` 在查询前保证下一轮持久化，终态不再排任务。
- [ ] 独立复审、定向与整包 PostgreSQL 回归及 vet；完成后冻结 22 并更新实际兼容边界。
