# 渠道分账后原单收入与 T1 专项证据

52 是未部署的候选迁移。本专项只使用合成密钥、临时 TLS 上游和本地 PostgreSQL 55432 独立随机 schema，不访问生产、不发起真实资金交易。

## 实现

原单可冻结/站内按单分配额度为 `max(0, 原 merchant_net - 成功普通退款 merchant_amount - 成功渠道 PS 净出流 - 未 rejected 的已有按单站内分配)`。其它订单、转账或充值形成的可用余额不能放大该源额度；普通 merchant-funded 退款在可信渠道完结后可使用商户自己的钱包，不自动触发 PR。

收入冻结保存独立且不可变的 `refund_merchant_amount`、`channel_sharing_amount`、`internal_allocated_amount`。旧数据新增字段为0、evidence_version=1，保留原证据；新数据必须version2并与原单 source projection完全对应。原订单锁串行化新分配与冻结。数据库同时约束原支付/退款/站内reserve和review journals、原hold账户余额、原freeze/unfreeze journals及同actor审计；不能仅改request状态恢复预算，不能从仍held的账户另写平衡journal取走金额。

T1只排除仍未可信释放的专属预留；释放后按 `原gross - 成功渠道分账净出流` 保守阻塞当日收入。保留原平台费、普通部分退款不减gross阻塞、全额普通退款后退出阻塞的政策。SQL helper `order_income_channel_sharing_outflow(bigint)` 是53可信PR回退的扩展入口；pending/unknown PR不恢复预算，53只减有完整证据的成功PR。

真实资金动用前，`orderIncomeRefundAllowed`、`orderIncomeSource` 在原事务内调用53提供的 `verifyOrderSharingReturnEvidence`，复验原PS及全部PR的keyed request/observation/completion链。SQL view和无Store key的T1 reader只做结构/账本验证，不声称能验证HMAC；后续PF统一资金释放投影另行集成。

## 实际测试

- 首两项真实红：完成PS后ReserveRefund仍ErrState；完成释放的今日收入跳过T1，6.510s/exit1。
- 首轮两项修复绿9.346s（当时53尚未可编译，仅用临时Go overlay排除其未完成return_test，未修改他人文件）。
- 无overlay新52专项20.509s/exit0；费用/普通退款T1独立4.521s/exit0。
- 最终无overlay：`go test ./internal/payment -run '^TestOrderIncomeChannelSharing|^TestOrderIncomeHold|^TestT1|^TestChannelSharingReturn' -count=1`，91.867s/exit0。覆盖真实TLS原PS/PR、旧普通收入与退款/T1、并发多规则源额度、多个用途的独立快照、拒绝分配返款证据、全额merchant-funded退款、审计故障回滚、SQL篡改及持有账户非法支出、pending和可信成功PR对原单预算及T1的区别。
- 旧hold证明升级52独立回归：3.555s/exit0。为当前Go查询purpose列，fixture先应用独立54 metadata迁移；原hold表仍为52以前格式，原freeze/journal/actor证据没有变化。升级后旧字段默认0且version1，可正常unfreeze；下一轮新hold为version2。
- `go vet ./internal/payment ./db` exit0；`go test ./db -count=1`，5.168s/exit0。

尚未把本专项结果当作50至54全库/race/部署通过证明。独立review、全库及本地完整部署由根任务汇总执行。52冻结后不改原48/49/51。
