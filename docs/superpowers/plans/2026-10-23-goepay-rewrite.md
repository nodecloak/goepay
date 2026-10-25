# GOEPAY Go + PostgreSQL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> 执行说明：以上为规划技能的标准执行提示；当前会话未提供这两个子技能，也未获得启动开发或子代理的指令。本轮只交付设计。后续执行应遵循当时可用工具和用户要求，不因该提示自动启动子代理。本文件为阶段实施规划；涉及加密行为和缺失插件的编码细则必须以阶段0产出的契约为依据。

**Goal:** 使用 Go + PostgreSQL 完整重建 epay_pro，保持既有支付接入兼容、后台功能逐项对应，并提升资金正确性、安全性与可维护性。

**Architecture:** 模块化单体，API、Worker、CLI 三个入口；独立 V1/V2 兼容适配、统一领域服务与账本、显式 SQL。以 PostgreSQL 事务 Outbox 和持久任务实现异步闭环，Redis 按需启用。

**Tech Stack:** Go、PostgreSQL 18、pgx v5、sqlc、Vue 3、TypeScript、Vite、Element Plus；工具具体稳定版本在开工时锁定。

**Spec:** [总体设计](../../design/2026-10-23-goepay-design.md)、[接口兼容规范](../../design/2026-10-23-compatibility.md)、[功能索引](../../baseline/feature-index.md)。

## Global Constraints

- 有效项目目录是 `E:\GOEPAY`；`epay_pro/epay_release` 只读参考。
- Go + PostgreSQL 是最终业务与资金系统；初期不引入独立消息队列。
- 当前基线为82个菜单叶子页面、517条后台静态路由、22条开放API和33条其他静态路由；动态插件和非菜单流程必须追加。
- 对外保持原字段、签名、返回与通知语义；后台新API使用独立版本空间。
- 金额不经过二进制浮点，资金只能通过统一记账入口修改。
- 加密行为和缺失插件没有证据时不得标记已兼容、已实现或验收通过。
- 原商户ID、订单ID与密钥关系保持连续；原PHP插件不能被宣称能直接加载到Go。
- 本文中的未来路径与检查命令是实施约定，当前并不存在完整Go/前端工程，不代表这些检查已经执行。

## 阶段依赖和进度口径

```text
0 证据与契约冻结
  → 1 基础工程、身份、权限
  → 2 账本与持久任务
  → 3 首个真实通道的支付兼容闭环
  → 4 双后台账户、商户、交易与配置
  → 5 结算、提现、代付、红包、内部转账
  → 6 渠道分账、站内分账、预授权与延迟结算
  → 7 进件、投诉、商户管控
  → 8 全部通道与外部集成
  → 9 系统、扩展、模板、应用市场与更新
  → 10 报表、对账、容量、安全与故障验证
  → 11 迁移演练与完整验收
```

先完成一个纵向支付闭环，再扩展全范围；阶段交付是开发顺序，不是删减最终需求的MVP。若某外部依赖暂不可得，可做其他模块，但该功能保持未完成状态。

每阶段需要一个可独立审查的结果，记录实际文件、关联feature_id、验证命令和输出。工程建立后采用小步实现：先增加能捕获目标错误的测试，确认失败，再实现、验证、记录。纯样式或文档改动不机械增加镜像测试。

## 阶段0：证据与兼容契约冻结

**当前已交付文件：** `docs/tools/inventory.mjs`、`docs/baseline/*`、`docs/design/*`。

**后续新增：** `docs/baseline/acceptance-map.csv`、`docs/baseline/plugin-capabilities.csv`、`api/compatibility/pro-v1.yaml`、`api/compatibility/pro-v2.yaml`、`testdata/compat/manifest.json`、`tests/contract/compatibility_test.go`。

**输入：** 当前静态基线、Pro原系统可运行实例、实际插件与配置、脱敏数据结构。**输出：** 可重放协议样本、全部功能对应关系和有证据的规则。

- [ ] 保存源包指纹和当前生成基线，核查原站部署模式、PHP版本、业务时区、数据库版本及插件列表。
- [ ] 对82个页面逐个记录筛选项、字段、按钮、弹窗、枚举、校验与权限；operation等接口展开子动作。
- [ ] 为兼容文档C-01～C-15采集实际请求/响应；缺失项标记证据来源和影响，不能虚填。
- [ ] 补齐Pro缺失插件，建立每插件“支付/查询/退款/关闭/代付/分账/进件/投诉”能力矩阵，包含配置字段与版本差异。
- [ ] 从原V1/V2 SDK生成固定时间、固定测试密钥的签名样本；生产私钥不进入仓库。
- [ ] 建立验收映射，每条包括 feature_id、source_path、menu_id、route_id、action、module、contract_case、implementation、status、evidence。

**验收：** 静态计数可复现；所有已知功能有条目；高风险金额规则和首个支付通道的契约无未决冲突。未到位插件不能阻止已知协议设计，但阻止其自身编码细则和完整覆盖验收。

现有基线复现命令：

```powershell
Set-Location -LiteralPath 'E:\GOEPAY'
node docs/tools/inventory.mjs
```

## 阶段1：基础工程、身份与权限

**新增：** `cmd/api/main.go`、`cmd/worker/main.go`、`cmd/cli/main.go`、`internal/bootstrap/app.go`、`internal/platform/config/config.go`、`internal/platform/postgres/pool.go`、`internal/identity/session.go`、`internal/identity/authorization.go`、`db/migrations/000001_identity.sql`、`deploy/compose/compose.yaml`、`web/apps/admin/package.json`、`web/apps/merchant/package.json`、`web/packages/contracts/identity.ts`。

**测试：** `tests/integration/authorization_test.go`、`tests/e2e/auth.spec.ts`。

**输入：** 角色、商户状态、原登录流程与密码验证样本。**输出：** 可靠启动/退出、数据库迁移、身份上下文、后端权限裁决、双后台登录与菜单框架。

- [ ] 锁定工具链与依赖、建立Go模块/前端workspace、配置校验与健康检查；没有数据库时readiness失败且不泄露连接串。
- [ ] 实现管理员/商户独立会话、角色和数据范围、CSRF、登录限流、TOTP、退出与撤销。
- [ ] 实现旧密码验证与成功登录后升级；管理员SSO使用限时单次凭据并记录真实操作主体。
- [ ] 建立菜单映射，保留全部节点ID、隐藏页和角色；页面未实现时标记开发状态，不伪造已完成。
- [ ] 验证未登录、已封禁、跨商户、低权限管理员和会话失效场景，确保API拒绝而不仅仅是隐藏按钮。

**验收：** 两种身份不可互换；所有敏感操作需后端授权；启动失败和退出可预测；原密码迁移有真实样本。

## 阶段2：账本、幂等与持久任务

**新增：** `internal/ledger/money.go`、`internal/ledger/posting.go`、`internal/ledger/repository.go`、`internal/platform/jobs/queue.go`、`internal/platform/jobs/worker.go`、`db/migrations/000002_ledger.sql`、`db/migrations/000003_jobs.sql`、`db/queries/ledger.sql`、`db/queries/jobs.sql`。

**测试：** `internal/ledger/money_test.go`、`tests/integration/ledger_test.go`、`tests/integration/job_recovery_test.go`。

**输入：** 已确认金额、费率、负余额与业务记账规则。**输出：** Money、账本凭证与分录、业务幂等、outbox/inbox、租约任务和查证状态。

- [ ] 明确金额解析/格式化契约、币种精度和溢出策略，原协议特殊输入交给兼容层处理。
- [ ] 建立账户、余额、凭证、分录、唯一业务键、提交时借贷平衡约束，以及受限数据库角色。
- [ ] 实现同一事务中的分录、余额与outbox；多账户按固定顺序锁定；处理死锁与序列化失败时重试整个事务，且限制次数。
- [ ] 实现短事务领取任务、租约续期、条件完成、重试、死信与人工重放；外部操作在事务外执行。
- [ ] 真实PostgreSQL并发测试重复记账、超额预占、互转、工作进程崩溃和过期Worker提交。

基础Money的计划接口与最小测试如下，供后续实现直接固定契约；这不是在本轮建立Go工程：

```go
// internal/ledger/money.go
type Money int64 // 人民币分；其他币种使用单独币种精度模型。
func ParseCNY(s string) (Money, error)
func (m Money) CNYString() string
```

```go
// internal/ledger/money_test.go
package ledger

import "testing"

func TestParseCNY(t *testing.T) {
    for _, tc := range []struct{ input string; want Money }{
        {"0", 0}, {"1.0", 100}, {"0.01", 1}, {"99999999.99", 9999999999},
    } {
        got, err := ParseCNY(tc.input)
        if err != nil || got != tc.want {
            t.Fatalf("ParseCNY(%q) = %d, %v; want %d", tc.input, got, err, tc.want)
        }
    }
    for _, s := range []string{"", "1.001", "1e3", "NaN", "-1", "92233720368547758.08"} {
        if _, err := ParseCNY(s); err == nil { t.Fatalf("accepted invalid amount %q", s) }
    }
    if got := Money(101).CNYString(); got != "1.01" { t.Fatalf("formatted %q", got) }
}
```

**验收：** 每笔凭证借贷平衡，余额可从分录重建；任务重复执行只产生一次内部资金效果；外部未知结果进入查证流程。

## 阶段3：首个真实通道支付与兼容闭环

**新增：** `internal/compat/v1/handler.go`、`internal/compat/v2/handler.go`、`internal/compat/signature.go`、`internal/payment/create.go`、`internal/payment/callback.go`、`internal/payment/query.go`、`internal/refund/service.go`、`internal/channel/router.go`、`internal/adapters/payment/registry.go`、`internal/notification/payment_notify.go`、`web/apps/cashier/package.json`、`db/migrations/000004_payments.sql`。

**测试：** `tests/contract/sdk_compatibility_test.go`、`tests/integration/payment_race_test.go`、`tests/integration/refund_race_test.go`、`tests/e2e/cashier.spec.ts`。

**输入：** 阶段0首个完整通道协议、阶段1身份、阶段2账本/任务。**输出：** 原SDK可调用的下单—支付—查单—通知—退款—对账闭环。

- [ ] 分别实现原始参数解析、签名与响应序列化，重放V1/V2固定样本，优先验证成功码差异、空值和JSON字符串字段。
- [ ] 建立订单/尝试/事件、订单号唯一约束、固定费率快照和通道路由决策。
- [ ] 实现首个真实适配器及模拟上游故障；记录请求号后外呼，未知结果查证不盲目重下单。
- [ ] 实现原始报文回调验签、入账事务、outbox与同步返回页；回调提交失败不ACK成功。
- [ ] 实现所有支付材料的前端展示能力；首个通道不支持的材料通过能力测试标记，而不是伪造可用。
- [ ] 实现退款预占/查询/幂等；通知重试生成当前timestamp和签名，并用原SDK验证。
- [ ] 按兼容矩阵跑重复、冲突、回调/关闭竞态、延迟成功、外呼超时与跨商户场景。

**验收：** 原V1/V2 SDK无业务代码改动通过；无重复收款/入账/退款；unknown可通过查询收敛。该阶段不代表所有插件完成。

## 阶段4：双后台账户、商户、交易与配置

**新增：** `internal/merchant/service.go`、`internal/merchant/membership.go`、`internal/merchant/certificate.go`、`internal/risk/service.go`、`api/openapi/admin.yaml`、`api/openapi/merchant.yaml`、`web/apps/admin/src/features/merchant/`、`web/apps/admin/src/features/trade/`、`web/apps/admin/src/features/payapi/`、`web/apps/merchant/src/features/account/`、`web/apps/merchant/src/features/trade/`、`web/packages/api-client/`。

**测试：** `tests/e2e/merchant.spec.ts`、`tests/e2e/trade.spec.ts`、`tests/integration/business_order_effects_test.go`。

**输入：** 账户设置、商户/组/交易相关feature_id及原字段规则。**输出：** 管理和商户视角的完整交易与账户操作。

- [ ] 实现商户、用户组、购买会员、注册/审核、充值、保证金、实名、域名、邀请码与返现。
- [ ] 实现订单详情、子订单、筛选、补单、退款记录、查询、通知重发、打印、冻结/解冻、订单名规则。
- [ ] 实现支付方式、通道/子通道、轮询、证书、公众号小程序及测试支付配置，显示真实能力和校验结果。
- [ ] 将菜单、后端权限和生成客户端接入双后台，逐页核对字段、操作、导出、错误反馈和隐藏页。
- [ ] 验证各tid业务在回调重放下只产生一次商户、会员权益、充值、保证金或邀请奖励。

**验收：** 相关菜单和全部操作有闭环；商户不能操作其他商户资源；前端不能绕过资金服务直接改余额。

## 阶段5：结算、代付、红包与站内转账

**新增：** `internal/settlement/service.go`、`internal/settlement/batch.go`、`internal/transfer/service.go`、`internal/transfer/red_envelope.go`、`internal/transfer/internal_transfer.go`、`db/migrations/000005_payouts.sql`、`web/apps/admin/src/features/settle/`、`web/apps/admin/src/features/transfer/`、`web/apps/merchant/src/features/settle/`、`web/apps/merchant/src/features/transfer/`。

**测试：** `tests/integration/payout_test.go`、`tests/integration/settlement_batch_test.go`、`tests/e2e/payout.spec.ts`。

**输入：** 结算周期、手续费/限额、支付密码、代付与红包上游契约。**输出：** 预占、审批、执行、确认、查询、撤销/失败释放与凭证。

- [ ] 实现申请提现、结算设置、自动结算、批次生成/审核/执行/结果与逐项导出。
- [ ] 实现转账、批量代付、凭证、余额查询、接口开关，保持旧biz_no等字段。
- [ ] 实现红包领取与微信确认页，按上游状态决定可取消性；逾期和处理中不能假定失败。
- [ ] 实现站内转账审核，双方同事务记账；管理员与商户发起均记录主体。
- [ ] 验证并发提现、重复批次、未知出款、批量部分失败、重复审核和缺外部幂等号限制。

**验收：** 每一元出款可对应预占/凭证/上游结果；未知出款不会释放资金再花；相关菜单和统计均可用。

## 阶段6：分账、预授权与延迟结算

**新增：** `internal/profitsharing/rules.go`、`internal/profitsharing/service.go`、`internal/profitsharing/internal.go`、`internal/payment/preauthorization.go`、`db/migrations/000006_profitsharing.sql`、`web/apps/admin/src/features/profitsharing/`、`web/apps/merchant/src/features/profitsharing/`。

**测试：** `tests/integration/profitsharing_test.go`、`tests/contract/preauthorization_test.go`。

**输入：** 接收者规则、比例/固定值/尾差、退回策略、通道支持情况。**输出：** 渠道分账和站内分账独立闭环、预授权冻结/转支付/解冻、延迟结算。

- [ ] 实现分账规则版本快照、接收者维护与能力校验。
- [ ] 分别实现外部分账执行/查询/回退/解冻和站内账本分配，避免双重记账。
- [ ] 实现部分成功的逐接收者查证，不重新执行已成功部分。
- [ ] 实现预授权状态、资金责任、关闭与解冻，映射原status=4及后台动作。
- [ ] 校验多接收者尾差、重复任务、退款联动、部分失败和历史规则变化。

**验收：** 累计分账不超过可分余额，重放不重复；预授权不被错误视为普通实收。

## 阶段7：进件、投诉与商户管控

**新增：** `internal/onboarding/service.go`、`internal/onboarding/form_schema.go`、`internal/complaint/service.go`、`internal/complaint/sync.go`、`internal/risk/merchant_restriction.go`、`internal/adapters/storage/storage.go`、`db/migrations/000007_onboarding_complaints.sql`、`web/apps/admin/src/features/applyments/`、`web/apps/admin/src/features/complain/`、`web/apps/merchant/src/features/applyments/`、`web/apps/merchant/src/features/complain/`。

**测试：** `tests/contract/complaint_test.go`、`tests/e2e/onboarding.spec.ts`、`tests/integration/attachment_access_test.go`。

**输入：** 补齐的Pro进件/投诉插件、动态表单、外部审核及投诉showtype。**输出：** 全流程资料提交、签约、状态刷新、管控、投诉处理与图片保护。

- [ ] 实现渠道schema表单、材料上传、银行卡/支行、签约、审核退回再提交、子通道绑定和交易报表。
- [ ] 实现投诉列表/详情/同步/回复/举证/退款审批反馈/完结，以及批量任务与统计。
- [ ] 对images数组、图片代理、附件下载按商户、投诉/进件对象和角色绑定授权。
- [ ] 实现商户管控记录、刷新与允许操作，保留备注与通知配置。
- [ ] 验证隐藏进件表单页、各showtype按钮差异、重复同步、超时和证据图片越权。

**验收：** 与原版插件实际支持能力对应；同意退款反馈不冒充资金退款成功；附件不能跨商户访问。

## 阶段8：全量通道与外部集成

**新增：** `internal/adapters/payment/<plugin-id>/`、`internal/adapters/mail/`、`internal/adapters/sms/`、`internal/notification/subscriptions.go`、`internal/notification/devices.go`、`testdata/provider/<plugin-id>/`。

`<plugin-id>` 必须来自 `docs/baseline/plugin-capabilities.csv` 已确认行，不能凭空命名。每个适配器有独立配置/签名/解析/错误映射与契约测试文件。

**输入：** 阶段0完整插件能力矩阵和每个外部服务凭证/沙箱。**输出：** 全范围原能力可用，而不仅是接口返回成功。

- [ ] 按生产使用量安排迁移次序，逐一落实Release 60入口和Pro补充插件的差异与最终支持决策。
- [ ] 每个支付通道实现其已承诺能力，加入上游验签、证书更新、状态查询和未知结果恢复测试。
- [ ] 实现短信、邮件、第三方登录、QQ机器人、语音设备、打印、事件订阅、日报与余额提醒。
- [ ] 完成聚合收款码、官网/支付模板、小程序收银台及OpenID授权流程的真实集成。
- [ ] 对每个外部服务记录可测试环境、限制、错误重试与降级行为；临时桥接若启用必须隔离且不能写Go账本。

**验收：** 全部台账能力有正常/异常/回调/重试证据；未拿到材料或仍依赖未验证桥接的能力不算完成。

## 阶段9：系统、扩展、市场与更新

**新增：** `internal/system/settings.go`、`internal/system/scheduler.go`、`internal/system/audit.go`、`internal/system/cleanup.go`、`internal/system/update.go`、`internal/adapters/marketplace/client.go`、`internal/system/plugins.go`、`web/apps/admin/src/features/system/`、`web/apps/admin/src/features/app/`、`web/apps/admin/src/features/other/`。

**测试：** `tests/integration/scheduler_test.go`、`tests/integration/update_package_test.go`、`tests/e2e/system.spec.ts`。

**输入：** 原设置字段、11类任务、应用市场实际协议、模板与扩展包。**输出：** 全部系统管理与应用管理能力。

- [ ] 实现分域配置及版本、敏感值引用、动态菜单前缀、邮件短信测试、代理测试和受控缓存清理。
- [ ] 实现全部11类内置计划任务及频率/参数/启停/立即执行/记录，加入插件任务注册。
- [ ] 实现公告、管理员、登录/运行日志、数据归档清理、聚合码样式及隐藏编辑页。
- [ ] 实现市场账户/授权/购买/充值/下载、本地插件、模板和扩展；原厂不可用时记录真实依赖失败，不能做虚假成功占位。
- [ ] 实现更新包签名验证、兼容检查、数据库迁移预检与可恢复切换，清理操作不能破坏资金账本。

**验收：** 这些菜单都能完成原有业务目的；危险操作有权限、审计与影响预览；归档/冲正语义差异有书面记录。

## 阶段10：报表、对账、性能与安全验证

**新增：** `internal/reporting/service.go`、`internal/reporting/export.go`、`internal/ledger/reconcile.go`、`tests/load/payment.js`、`tests/integration/fault_recovery_test.go`、`docs/runbooks/reconciliation.md`、`docs/runbooks/incident-recovery.md`。

**输入：** 完整功能、真实数据分布、原系统性能基线。**输出：** 可审计报表、对账差异队列、性能/故障/安全报告。

- [ ] 实现每日、商户、通道、地区、买家、代付和投诉统计，定义各口径与业务时区。
- [ ] 实现数据库账本内部核对、上游账单核对、差异分类与人工处理；外部账单能力不足时采用明确的查询对账流程。
- [ ] 实现异步导出、权限和截止时间快照，校验大数据量不拖垮关键支付链路。
- [ ] 按总体设计实验条件压测，比较同硬件同功能的吞吐与p95/p99，保留完整报告而不是单一峰值数字。
- [ ] 演练数据库重启、Worker崩溃、Redis不可用、上游超时、重复通知、证书到期和备份恢复。
- [ ] 执行越权、SSRF、文件路径、压缩包、SQL注入、CSRF、XSS、密钥泄露和降级认证检查，修复后复验。

**验收：** 资金无漂移，故障可恢复，容量目标可实证；安全例外按兼容策略明确记录。

## 阶段11：迁移演练与完整验收

**新增：** `cmd/cli/migrate_legacy.go`、`internal/migration/importer.go`、`internal/migration/checks.go`、`docs/runbooks/cutover.md`、`docs/runbooks/rollback.md`、`docs/baseline/final-acceptance.csv`。

**输入：** 脱敏真实数据演练、生产切换配置、已验证全部功能。**输出：** 导入工具、逐商户对账、切换水位与恢复方案、最终验收记录。

- [ ] 对真实schema和历史update进行差分，清洗日期/编码/重复号/空值/序列化字段，生成失败行清单并可重跑。
- [ ] 迁移原ID、密钥、凭证和资料，保存旧流水与期初账；冻结/待结算/退款/代付责任独立核对。
- [ ] 在演练环境模拟暂停写入、回调持久缓冲、增量迁移、切换唯一写入方、恢复回调与存量订单归属。
- [ ] 演练“新系统没有写入”及“已经有新资金写入”两类回滚，证明不会因快照恢复丢交易。
- [ ] 逐项核对82页面、517后台动作映射、22开放API、33其他路由、PHP别名、动态插件和11类任务，缺项保持未完成。
- [ ] 最终交付源代码、生成契约、迁移与部署说明、监控告警、容量报告、恢复记录和验收清单。

**验收：** 功能/接口/资金/数据/运行五类验收同时通过；真实发布是后续单独执行动作，本轮没有部署。

## 工程建立后的检查命令约定

以下命令应由阶段1配置对应脚本，并在相关阶段执行；本次只运行了文档盘点工具。

```text
go vet ./...
go test -race ./internal/...
go test -tags=integration ./tests/integration/... -count=1
go test ./tests/contract/... -count=1
pnpm --dir web -r typecheck
pnpm --dir web -r build
pnpm --dir web exec playwright test
```

integration测试使用专用临时PostgreSQL数据库，contract测试使用固定时钟与测试密钥，e2e使用隔离租户和模拟/沙箱通道。禁止将生产DSN、真实支付密钥或实际商户通知地址作为默认测试配置。

## 本轮文档自检

- [x] 总体设计覆盖架构、目录、接口、资金、数据库、前端、安全、插件、任务、性能、迁移与回滚。
- [x] 从原菜单自动生成全部82个叶子页面，隐藏页未遗漏。
- [x] 逐项保存572条静态路由、67个安装表定义和插件入口，标注统计边界。
- [x] 对全部38张Pro安装表给出新模型归属。
- [x] 明确V1/V2成功码、签名与通知时间差异，列出15项兼容核实任务。
- [x] 明确加密代码、空插件目录、原厂服务和生产数据缺口，不把设计当成已验证兼容。
- [ ] 业务实现与运行验收：属于后续开发工作，本轮未执行。
