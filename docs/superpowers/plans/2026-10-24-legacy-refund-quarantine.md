# 历史退款导入隔离 Implementation Plan

> **For agentic workers:** 根任务按真实全库失败直接修复，连续实施，不改64固定候选或已冻结迁移。

**Goal:** 在保留全部旧退款及opening reserved义务的同时，禁止无可信支付/来源证明的导入记录进入可执行退款核心。

**Architecture:** 新迁移65增加专门不可变、原export关联的legacy refund档案。导入同一事务保存历史原状、HMAC与原opening账户；不创建refund/provider job，不假造63来源。提供本人范围只读历史，CLI单独计数损坏档案及未知义务。

**Tech Stack:** Go、pgx、PostgreSQL、既有迁移校验与双后台会话。

**Spec:** `internal/migration/validate.go` 原导入校验与schema13不可变导入run；63出生来源约束。全库64实际失败：`TestImportOpeningBalancesHistoryAndIdempotency`，`missing valid refund source funding`。

## Global Constraints

- 只在E:\GOEPAY使用合成snapshot和随机schema；不读取或访问生产、实际密钥、原数据库。
- 不修改迁移1..64、不放宽funding_evidence_version出生约束、不把导入paid当可信支付。
- 旧退款原merchant/trade/refund/amount/status完整保存；未知义务仍在opening reserved中，CLI显示非零，不能静默消失或转可用余额。
- 导入全事务和export digest幂等；归档行与run不可变，HMAC绑定来源digest、原订单及原merchant；不同导出不能重用同一历史商户退款号。

## Task 1：真实失败与隔离导入

Files: 新 `db/migrations/000065_legacy_refund_history.up.sql`、`internal/migration/refund_history.go`；修改 `import.go` 与 `import_test.go`。

- [ ] 保留64两平台原始失败日志。测试导入后同样12.34可用+0.50预留、完整旧退款历史、active refunds/新增jobs均0；先运行确认RED。
- [ ] 新专表插入在原import事务，deferred export外键与原订单归属检查；HMAC不作为执行权限。
- [ ] 原迟发故障注入改在真实archive插入处，仍证明merchant、journal、run、archive全部回滚；幂等/冲突断言不减弱。
- [ ] 增加SQL不可变与绕guard损坏HMAC拒绝、同refund跨tenant拒绝和全量导入包回归。

## Task 2：读取与账本报告

Files: 修改 `cmd/cli/main.go`；新只读console history handler及对应HTTP合同；更新OpenAPI/迁移说明。

- [ ] `ListRefundHistory(ctx,pool,master,merchantID,afterID,limit)` 只读分页验证来源HMAC及parent export digest，public DTO没有proof/key。
- [ ] 原会话/owner/CSRF规则保持；只读历史不提供query/resolve/refund动作，不能把旧status解释成Go已验证资金结果。
- [ ] CLI增加 `invalid_legacy_refund_history`、`unknown_legacy_refunds`；损坏或未结义务非零退出，备份恢复继续执行完整CLI。
- [ ] 固定修复候选，与64源逐项比较；Windows完整PG/vet/build及Linux race重新执行。其间打印连接器开发源码不混入修复候选。
