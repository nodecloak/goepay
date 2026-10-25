# T+1 可出账额度实施计划

> **For agentic workers:** 按下列任务逐项执行，先验证失败测试，再实现并复验。独立审阅由父任务安排。

**Goal:** 全局 T+1 策略下，外部代付、结算申请、站内转账和站内分账使用相同且并发安全的可出账额度。

**Architecture:** 账本余额保持原值。可出账金额为 `max(0, available - 当日非充值且未全退的成功订单 gross)`，默认 T+0。资金预占在商户锁和按账户 ID 排序的 available/reserved 行锁内重新计算，重复请求直接返回原意图。退款不受出账额度限制。

**Tech Stack:** Go、pgx/v5、PostgreSQL；整数分；北京时间 UTC+8。

**Spec:** 本机 `epay_release/includes/lib/Transfer.php:98` 与 `includes/lib/api/Transfer.php:133` 的全局 settle_type 规则；`includes/functions.php:616`、`:640` 明确 tid=2 是余额充值。这里的全局设置与商户资料中的收款账号类型同名字段完全独立。

## Global Constraints

- 仅本地 PostgreSQL 和假上游，无生产调用、无真实资金。
- 迁移 1–25 冻结；26 只添加查询索引。
- 不直接修改账户余额，资金变化必须经过现有平衡凭证。
- 当日 gross 使用 orders.amount，充值由 recharge_intents 标识，全退由成功退款累计金额判断。
- 部分退款和未知退款保持当日限制；已成功全退移除限制；缺省 settle_type=0 不改变行为。
- 保留原始账面余额单独展示，禁止用可出账计算覆写账户。

## Task 1: 核心额度与预占

**Files:** `internal/payment/funds_availability.go`、`funds_availability_test.go`、`reservations.go`、`ledger_requests.go`、迁移26。

- [x] 写真实 PG 红测：今日收入不能用于四类出账，拒绝不留冻结/请求。
- [x] 新增 `Store.FundsAvailability(context.Context,int64) (FundsAvailability,error)` 及事务内锁定检查。
- [x] 在外部转账和三个账务请求预占前调用同一检查，保持幂等。
- [x] 验证北京时间边界、充值、毛额/手续费、全退、溢出和并发退款/出账。

## Task 2: 配置与接口

**Files:** `internal/backoffice/transfer_settings.go`、`service.go`、`workflows.go`；`internal/server/transfer_http.go`、`merchant_api.go`；对应 PG 合约测试。

- [x] 设置注册 settle_type，仅接受字符串0/1。
- [x] 代付余额及能力接口返回一致 available_money；额外返回 ledger_available_money、today_blocked_money、settle_type。
- [x] 增加有租户权限检查的 `/workflows/funds-availability`，返回同一结果。
- [x] 跑真实 PG 合约、相关包回归和 vet，记录结果并冻结交父任务审阅。

## 验证记录

- 首轮红测 `TestT1RejectsEveryOutgoingReservationBeforeFreezing` 复现今日资金被外部转账预占；实现共享guard后通过。非法null设置复现被当成T+0后修正。实际支付账户锁等待复现 paid_at 早于锁释放后，markPaid单行改为 statement_timestamp()，回归通过。
- 最新 `go test ./internal/payment -run TestT1 -count=1`：7项通过，8.667秒。另有后台配置/租户权限和RSA余额/console一致性合约通过。
- `go test ./internal/payment ./internal/backoffice ./internal/server ./internal/reconciliation ./internal/platform/jobs ./tests/contract -count=1`：前5包全部通过；contract仅 `TestScanHTTPOriginalContractsUnknownRecoveryAndLateNotify` 失败，provider_scan_http_test.go:148 返回 payment channel not permitted，已交父任务及协议负责人处理，未改其源码。
- 相同6包 `go vet` 通过。只使用本机PG与假上游；迁移26未由本子任务部署到持久运行库。
