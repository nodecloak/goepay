# Settlement Scheduling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 本轮仅规划；实际实施由根任务在59完整候选冻结后分配，禁止据此自动开始60源码。

**Goal:** 根据本机 Pro 明文结算cron证据，补齐可恢复、可审计、有明确平台计划授权的结算申请调度，复用现有单笔结算及原号出款核心。

**Architecture:** PostgreSQL保存版本化政策、计划授权、到期slot、运行与逐商户原意图，现有jobs只承载已持久化ID。首个可执行阶段只自动生成待审核申请并预留原商户资金；人工审批与出款资金授权继续保留。无人值守付款是另一项有限授权能力，必须单独验收后开放，worker不得伪装管理员或重复消费人工一次grant。

**Tech Stack:** Go、pgx、PostgreSQL、既有`internal/platform/jobs`与原单结算资金核心；Vue管理界面。首版无须Redis或新消息队列。

**Spec:** `docs/design/remaining-console-evidence.md`，并以下述本次重新读取的明文文件补证；59之前已实现资金功能不是本计划的缺失项。

## Global Constraints

- 工作目录`E:\GOEPAY`；只读原PHP、本地随机schema和合成TLS测试；不访问生产、`.secrets`、`.env`、导入凭据或真实资金。
- 用户已明确“其它插件先不做”。不新增QQ/进件/实名插件，不把未实现实名校验当作已通过；实名必需但无可信凭据时该商户应跳过并报告固定reason。
- 所有资金用整数分，比例整数BPS；ID和金额在JSON使用十进制字符串，禁止浮点计算。
- 59完整候选冻结后才实施新迁移；不修改已冻结5、48–59 SQL。本文件只描述拟建`000060_settlement_scheduling.up.sql`，没有创建该源码。
- 所有新功能默认关闭；保存旧`cron-tasks`资料不产生真实任务或资金权限，不自动迁移为已授权计划。
- 单笔结算已经有原受款快照、专属reserve、原TF号、真实签验查询、银行退票闭环；新调度不能再次扣钱包、调用普通代付绕过原申请，或把审批/受理当作到账。
- 本计划的新字段/API/周期精确定义是新的安全规范；不宣称加密PHP后台JSON或加密runner内部规则完全兼容。

## 1. 证据与实际差异

| 来源 | 可确认事实 | 实施界限 |
| --- | --- | --- |
| `epay_pro/app/cron/Settle.php:28–136` | 先生成结算列表；全局最低额、商户status/settle/账号姓名、组自动开关/费率、固定/百分比留存、全局fee min/max；小时模式只处理当前指定hour且商户当天已有任何settle则跳过 | 原循环末全局日期标记不是跨worker幂等；插行后再扣款不是新资金事务规范；生成函数没有读取D1可出账额度 |
| `Settle.php:141–288` | 到自动付款hour之后处理当天status0结算，毛额上限，原行账号/姓名/净额；组`transfer_app`非null/空/`_`时优先，0也覆盖；新时间随机号提交 | 不照搬异常即失败、code0即完成或每次新随机号；只用原payout请求/原TF和可信查询 |
| `epay_pro/app/cron/Transfer.php:26–106` | 独立阈值路径，仅支付宝、余额严格`>`阈值，UID倒序每次最多5个；组禁自动/费率覆盖；费用只用max；先外呼成功后记录扣款 | 与Settle族不是相同公式；不能暗中套用另一套fee min/留存。新实现必须先持久化/预留；未知不换号；不因普通失败自动关闭通道 |
| `epay_pro/app/common.php:746–769` | 组不存在回退组0；空值不覆盖；`settle_open/type/transfer/user_transfer`的0被当继承；`settle_type`再减1 | 新typed政策用nil继承、0显式值，旧格式转换需独立明确映射，不能把所有0丢弃 |
| `epay_pro/app/sql/install.sql:69–103` | 周期族second/minute/hour/day/once，interval、execute_at、lock_token/locked_until；默认settle hour1启用、transfer minute1关闭 | 能证明存储/默认值，不能证明加密`nextRunTime`的精确锚点与补跑语义 |
| `epay_pro/app/command/Cron.php:45–153,174–224` | 明文守护循环每60秒刷新、按next_run_time、调用runner claim/runScheduled；单次CLI也claim/release | `app/logic/CronTaskRunner.php`、两个Cron controller和SettleQueryService是Swoole编码，不能猜测claim原子性/周期算法 |
| `epay_pro/config/app.php:14` | 安装配置默认时区`Asia/Shanghai` | 补充既有研究时点：这是本机安装默认UTC+8证据，仍未读取生产运行时区 |
| 文件实际存在性 | 没有`app/cron/Printer.php`；实际`app/lib/Printer.php`仅设备print/HMAC适配器 | 不伪造“打印cron”；打印、日报、投诉、插件任务不自动纳入结算计划范围 |

原UI周期选项、cron配置API索引见`remaining-console-evidence.md`。`runTaskNow`显示“1分钟后执行”，是排队结果，不能返回“结算完成”。旧URL cronkey方式不在新安全入口上接受query bearer；如增加兼容触发，仅创建受限运行意图，不能直接外呼。

## 2. 复用当前Go能力，准确列出增量

- `settlement_terms.go`已有最低/最高、费率、fee min/max、固定留存、日次数及不可变申请快照；`SettlementPolicy.Quote`不能复制成第二套静默不同公式。
- `settlement_quote.go`已经在只读RR事务报价；`ledger_requests.go:requestFundsTxSettlement`重新锁商户、政策、可出账账户，按锁等待后的PG时钟检查日限，创建申请、专属reserve、原recipient快照、审计同事务。
- `funds_availability.go`已有UTC8零点D0/D1、原PS/PF/PR净出流、held income和充值等预算边界；所有新计划仍必须调用同一可出账预算，禁止只读钱包balance就扣款。
- `settlement_recipient.go`已有配置版本、AEAD和HMAC；计划需明确选定合法原config，不从普通`settlement-settings`资料抄自由账号，不将masked值当真实受款账户。
- `settlement_payout.go`、`settlement_payout_flow.go`、`settlement_bank_payout.go`已有prepare/execute/query/cancel及原TF号。当前执行actor是admin且执行时有`requireFundsAction("settlement-payout",id)`，不能直接给scheduler传伪admin actor。
- `cmd/worker/main.go`是只读provider reconciliation worker；`jobs.Queue`已有lease token、SKIP LOCKED、Complete/Retry CAS。增加生成scheduler不等于允许这个只读worker提交付款。`backoffice/extended.go`的cron-tasks仍为资料，`server/operations.go`重试已知dead jobs也不是创建计划权限。

新增：typed有效政策与商户自动结算选择、周期计算器、计划版本和授权、运行slot/逐项结果、稳定原意图键、生成处理器与专用console。自动审批/付款和阈值Transfer族在独立任务中开放。

## 3. 时间、政策和预算的确定规则

### 3.1 周期族与日界

新规范固定时区Asia/Shanghai（UTC+8，无DST）；数据库存timestamptz，日标签存date，所有业务时间取PG `clock_timestamp()`，测试可给纯函数传at，不从HTTP任意传“当前时间”。

| family | 首版明确算法 | 限制/补跑 |
| --- | --- | --- |
| second/minute/hour | 固定`anchor_at`加N×interval×单位秒；slot键是原anchor序号，`next_due`取严格大于last_slot的下一格 | second interval为10..3600、minute为1..1440、hour为1..168；生成结算不允许second，minute仅独立threshold族；迟到只恢复已记录slot，离线缺口默认跳过并记missed计数 |
| day | 本地日期每N天的`execute_hour:minute:second`，锚定本地anchor_date | interval为1..366且必须明确hour；一次日内重复扫描同一slot不会新增intent |
| once | 显式execute_at映射AnchorAt、interval=0，仅slot0 | 原slot已完成/取消/未知不可用run-now重新造第二笔 |

普通`settle`业务的生成hour与技术scan周期分开：小时扫描可找到不同组的hour，但每商户每业务日最多一个自动生成原意图。组hour=0是明确午夜，nil才继承。不得重现PHP `empty("0")`错误。

slot_no从0开始，未运行时lastSlot=-1；periodic的next函数返回lastSlot+1，once在lastSlot>=0时返回ErrState。

业务日定义：`business_date(t,h)=date((t at time zone 'Asia/Shanghai') - h hours)`，区间`[date@h, next_date@h)`。**生成hour**与**D1资金日切hour**是两个字段，不混用。首个安全阶段允许固定零点D1并只配置生成hour；开放组D1日切前，必须统一改Quote/RequestFunds/全部OutgoingFunds消费者，不能仅让scheduler获得更宽松额度。跨午夜等待锁后若业务日已变，返回day_changed，不在同号下挪日期或生成昨日新预留。

当前Go日限按全部结算请求计算，Pro前端称手动次数、小时自动又遇任何当天settle跳过。新增`request_source=manual|scheduled`并保留旧total-limit作为总上限；新manual-limit另记，仅手动计数，自动仍受总上限和自动日意图唯一限制。旧字段不静默改义。

### 3.2 有效政策和原受款范围

typed政策头采用global→有效商户组（行政base group或未到期membership，与已有费用组同一读取规则）；商户只控制自动结算opt-in、自己的recipient config与留存额/比例，不赋予其放宽平台金额/时段/通道上限的能力。组字段nil继承，false/0显式覆盖；所有引用版本、有效组ID、membership截止时间、merchant config revision在运行item和原申请中封存。

首个生成族只开放明文cron已经证明的组automatic mode/rate/generation hour覆盖；全局最低额与fee min/max保持原已证明来源。组minimum/maturity/方式开关由typed政策清晰开放并单独验收，不能假称Settle.php原函数已经使用。自动禁用优先于plan allow；merchant disabled、opt-out、recipient缺失/类型关闭、group unresolved、实名要求无可信证据均固定reason跳过，不生成“已通过”。

自动付款通道映射按已审授权的recipient type→channel ID和真实Application/TransferAccountScope绑定，0明确禁用，不从当前任意轮询临时选新账户。QQ无现有受信适配，始终unsupported；银行只用现有bank生命周期。

### 3.3 留存、费用、可出账预算

记L为原钱包ledger available、E为同事务`FundsAvailability.Available`。固定留存R直接整数分；百分比沿明文Pro“先舍入可结算金额”口径计算：

```go
func retainedByBPS(balance, bps int64) (int64, error) {
    if balance < 0 || bps < 0 || bps > 10000 { return 0, ErrInvalid }
    keepRate := 10000 - bps
    payable := balance/10000*keepRate + (balance%10000*keepRate+5000)/10000
    return balance-payable, nil
}
```

这是分级舍入：L=1分、50%BPS时可结算1、留存0；不可在UI承诺至少保留50%而隐藏此舍入。0/100%为明确合法边界，100%不生成申请；不复刻PHP无效百分比字符串退化成固定元数的行为。留存mode=`fixed|percentage`，不在字符串中猜`%`。

新普通计划G上限为`max(0,E-R)`，再受原policy.MaximumAmount、单项授权及剩余run/day预算限制；费用继续原`SettlementPolicy.Quote(G)`（rate0时fee0，不凭feeMinimum强行收费），`Net=G-Fee>0`，`Gross=Net+Fee`。留存属于可出账额之外的额外保留，与当前固定留存检查一致；D1被阻塞资金不拿来代替R。因此比PHP直接用L可更保守，需清楚展示L/E/R/G/Fee/Net。

创建必须在同事务核对E≥G+R，不能把referral escrow、PS未释放预留、另一结算reserve、另一单收入当可用。原申请专属reserve只转G一次，付款只花原Net，settlement Fee由原Gross预算结算，不用平台普通fees池补齐。费用、留存、recipient、scope或预算在preview后变化即重新核对，不能按旧报价自动扩大。

`Transfer`阈值族明确单列：余额**严格大于**threshold，支付宝，最多5个/slot，按merchant ID降序，fee只上限的独立规则版本；首版保持关闭。若落地，须完整新原申请+专属reserve后才调用原payout。新安全规范默认每商户每天一个threshold intent，并与普通生成族互斥；这比原每分钟可能多次付款更保守，不能声称逐分支完全同义。增加同日多意图必须新有限次数/预算授权和明确sequence，不能由失败或配置版本变化自动重臂。

## 4. 持久模型与授权，不复用人工grant

拟在新迁移增加以下专表，禁止用`backoffice_records` JSON“启用”作为资金凭证：

| 表 | 关键不可变内容/约束 |
| --- | --- |
| `settlement_plan_versions` / `settlement_plan_heads` | plan ID、revision、family、timezone、anchor/hour、scope、mode=`generate_only|auto_payout`、policy引用、canonical hash、原super admin actor、HMAC；CAS+原审计，默认disabled |
| `settlement_plan_authorizations` / revocations | plan/version/scope hash、valid_from/expires_at、商户/组范围快照、recipient types/config范围、原通道物理身份、每项/每天/run次数和gross/net/fee预算、mode、authorizer actor、原funds action challenge摘要、HMAC；撤销另行append，不改原证据 |
| `settlement_plan_slots` | plan ID、原schedule序号、scheduled_at、business_date、原revision/auth ID；`UNIQUE(plan_id,slot_no)`，改版本/run-now不能重开同slot |
| `settlement_plan_runs` | slot唯一、稳定run_no、授权引用、固定scope policy摘要、扫描水位及计数、状态、HMAC与系统actor审计；不只写一个全局last_day标志 |
| `settlement_plan_items` | run+merchant唯一、原business_date/hour、来源、L/E/R/G/fee/net、政策/recipient config/通道identity版本、request ID或固定skip reason、HMAC；成功已入账项不可删除重置 |
| `settlement_scheduled_intents` | `UNIQUE(merchant_id,business_date,intent_family)`并按默认互斥组限制普通/threshold并行；稳定external_no由durable intent ID生成，不含随机时间号；绑定原申请、funding、recipient及item |
| `settlement_plan_budget_uses` | auth+intent唯一，原Gross/Fee/Net、day累计、用途、HMAC与原审计，同reserve事务扣预算；未知/失败不自动释放次数重造新意图 |

所有父子完整性触发器从父表查缺行，不能INNER JOIN后让缺证据消失；金额非负且净/费相加等于Gross。SQL仅验证结构，Store读验HMAC；restore/checker新增计数并用分页验证。新自动申请必须有源intent，不能任意SQL插source=scheduled绕人工授权。

**授权actor：** 已登录super admin在专用console明确确认计划范围、有效期和限额，使用现有资金密码/OTP流程，`kind=settlement-plan-authorize`、key=`planID:revision`。这次一次grant只授权创建这一条持久、有限、可撤销计划authority，不是让worker保存grant、会话cookie、资金密码或原OTP。调整扩大范围必须新版本+新确认；撤销是独立审计动作。

首版授权scope冻结1..10000个merchant IDs；group IDs仅筛选这批已授权商户的资格，新加入该组的商户不能暗中加入旧授权。扩大集合需要新版本/新确认。动态组授权若以后开放，必须明确允许未来成员，并为每个run固定成员digest；不在首阶段悄悄添加此能力。

授权动作在同一事务将该definition revision的head置active。修改definition必须revision+1、旧authority失效、head回disabled，直至新授权；不以修改enabled布尔值绕过授权。

**执行actor：** `system/settlement-scheduler`，记录原authorizer及auth ID，不能构造`TransferBatchActor{Role:"admin",ID:authorizer}`冒充本人。新增private `planExecutionAuthority`只由同事务读取/校验active授权、HMAC、当前scope与剩余预算的factory产生；仅可消费确定kind/key的一个原intent，不做exported任意context guard绕过。人工路径仍调用原`requireFundsAction`。

首个安全阶段只允许authority用途`reserve_pending_request`，生成申请状态pending，不能自行approved或execute。未来自动审批/付款必须扩大typed authority及现有usecase的私有受限入口，并新增system actor↔plan proof审计关联约束；不能仅把原`payoutActor`放宽允许任何system。

撤销/到期阻止新的item reserve及未claim付款；已reserved申请保留原款，管理员可按原单reject释放。已claim/unknown只能原号query，核对不因授权到期停摆；授权恢复/新revision不重臂原claim。更换recipient、物理通道或credential identity使未发送执行失效，需要新显式范围确认，不能偷偷改原申请快照。

## 5. 运行事务与多worker恢复

1. scheduler取PG时钟，按已启用计划算到期slot；事务锁plan head/授权，插唯一slot/run及`settlement-plan.generate` job，提交后再由handler处理。第二scheduler冲突只读原slot，不追加资金意图。
2. 按merchant ID分段扫描；每商户一事务，锁计划/授权/预算→商户域锁→原intent/申请→稳定完整账户set。不得在已持账户锁后因FK、操作审计再等待merchant FOR UPDATE；需要沿59形成的FK预锁规则复用。跨商户单大事务不用。
3. 重新读取政策、可出账额、recipient、授权与业务日，先验证或写原intent，调用**共用申请事务primitive**完成原专属reserve、terms、recipient与审计，再写item/request关系与budget use；任一故障整体回滚，包括授权预算。
4. 若item已有request，按原数额/原recipient/原scope强验证并返回，不使用当前余额重新报价。若crash在commit之后job complete之前，重领job只返回原ID，零重复预留。扫描游标仅在已提交item/skip之后推进，失败保留原游标。
5. worker lease失效仅影响谁处理job，不影响资金send owner。原slot job可安全重跑到原items；已claimed payout不因lease超时重发。Generate worker不加载付款凭据；未来write worker与既有只读reconciler能力分离。
6. run状态由真实items聚合：pending/running/completed/partial/blocked/revoked；“completed”只指扫描/申请阶段完成，不能等于已付款。记录created/skipped/blocked/errors的真实数，避免PHP预先count包含跳过项的统计问题。
7. 默认不自动补齐历史缺日；管理员可选一个已存在slot恢复，或为允许的单一业务日创建有界run intent。过期日期不能按今天余额倒造昨日申请；预留发生日在原intent内固定，跨日返回day_changed并可只读归档，不换号。

## 6. 可执行工作拆分与接口

后续实施先读本文件与spec；每个任务必须先真实red，再green，再独立review与固定候选。当前目录不是Git仓库，不假造git commit；用源码manifest/checkpoint保存证据。测试PG为55432，测试shell沿现有Go环境前缀；所有命令本轮只是计划，未执行或声称通过。

### Task 1：typed政策、留存与业务日纯函数

**Files:** 新`internal/payment/settlement_schedule_policy.go`、`settlement_schedule_policy_test.go`；拟新60政策表。组日切若不同时改全量消费者，只允许零点且拒绝其它maturity hour。

**Interfaces:**

```go
type SettlementScheduleSpec struct {
    Frequency string
    Interval int64
    AnchorAt time.Time
    ExecuteHour, ExecuteMinute, ExecuteSecond int
}
type SettlementScheduledQuote struct {
    LedgerAvailable, Eligible, Retained, Gross, Fee, Net int64
    BusinessDate string
    PolicyHash []byte
}
func nextSettlementSlot(spec SettlementScheduleSpec, lastSlot int64) (slot int64, at time.Time, err error)
func settlementBusinessDate(at time.Time, cutHour int) (string, error)
func retainedByBPS(balance, bps int64) (int64, error)
```

- [ ] 写`TestSettlementScheduleUTC8BoundariesAndFamilies`：UTC15:59:59/16:00:00、leap day、hour0显式覆盖、once唯一、interval0/越界拒绝；验证slot严格递增而非按完成时间漂移。
- [ ] 写`TestSettlementScheduleRetainedAndFeeBudgets`：L1/50%→R0，L100/100%→G0，L200/E100/R20→G80；MaxInt64比例无溢出；fee0费min>0仍0，fee>=gross拒绝。
- [ ] 执行`go test ./internal/payment -run '^TestSettlementSchedule(UTC8|Retained)' -count=1`确认真实red；实现上述纯函数，再运行同命令green。
- [ ] 若开放组D1日界，先新增quote/request/withdraw/transfer/settlement实际边界红测，再用同一有效policy reader替换`funds_availability.go`与日限窗口；旧已reserved申请不重算。没有这组测试就保持fixed-midnight。

### Task 2：不可变计划/授权与数据库完整性

**Files:** 新60迁移、`internal/payment/settlement_plan.go`、`settlement_plan_authority.go`、对应独立tests；新`internal/server/settlement_plan_funds.go`接入资金确认resolver。

**Interfaces:**

```go
type SettlementPlanActor struct { Role, Class, Name string; ID int64 }
type SettlementPlanInput struct { Name string; Schedule SettlementScheduleSpec; Mode string; Scope json.RawMessage }
type SettlementPlanView struct { ID, Revision int64; Enabled bool; Mode string; PolicyHash []byte }
type SettlementPlanAuthorizationInput struct {
    PlanRevision int64
    ValidFrom, ExpiresAt time.Time
    MaxItemGross, MaxDayGross, MaxRunGross, MaxDayItems int64
    ScopeHash []byte
}
func (s *Store) CreateSettlementPlan(ctx context.Context, in SettlementPlanInput, actor SettlementPlanActor) (SettlementPlanView,error)
func (s *Store) UpdateSettlementPlan(ctx context.Context, id, expectedRevision int64, in SettlementPlanInput, actor SettlementPlanActor) (SettlementPlanView,error)
func (s *Store) AuthorizeSettlementPlan(ctx context.Context, id, expectedRevision int64, in SettlementPlanAuthorizationInput, actor SettlementPlanActor) (int64,error)
func (s *Store) RevokeSettlementPlanAuthorization(ctx context.Context, authorizationID int64, actor SettlementPlanActor) error
```

- [ ] 真实PG红测`TestSettlementPlanAuthorizationOwnerCASAndNoImplicitGrant`：非super/Issuer拒绝、缺grant不创建authority、两个并发CAS仅一个版本、audit故障全回滚、普通cron资料不能触发任何权限。
- [ ] 实现immutable版本/授权/HMAC/撤销与父表完整性guard；JSON/日志只显示脱敏scope，无受款账号或secret。
- [ ] `go test ./internal/payment ./internal/server -run '^TestSettlementPlanAuthorization' -count=1`先red后green；用零HMAC完整SQL形状证明公读/执行/checker拒绝。

### Task 3：只产生run的调度与恢复

**Files:** 新`internal/settlementscheduler/scheduler.go`、`scheduler_test.go`、`handler.go`；修改`internal/platform/jobs/queue.go`严格kind路由；新`cmd/settlement-scheduler/main.go`。

**Interfaces:** `Scheduler{Pool,Store}.Tick(ctx) (created int,error)`只创建slot/run/job；`Handler{Store}.Handle(ctx,jobs.Job) error`处理`settlement-plan.generate`且payload只能有canonical run_id；其他kind拒绝。

- [ ] 多scheduler同时Tick同due slot、crash after job insert、once重领/时钟跨日/disabled/auth expired红测；断言原slot/run/job各1，journals0。
- [ ] 实现唯一约束与job lease CAS；不复用原`cron-tasks`资料、不调用provider。
- [ ] 执行`go test ./internal/settlementscheduler ./internal/platform/jobs -run 'SettlementPlan|Schedule' -count=1`确认red→green；失效lease不能complete另worker的job。

### Task 4：逐商户原意图与原申请同事务

**Files:** 新`internal/payment/settlement_plan_run.go`、`settlement_plan_run_test.go`；在`ledger_requests.go`提取受限共用事务输入，原人工wrapper保持原资金guard。禁止复制一份post逻辑或exported绕授权函数。

**Interface:** `Store.GenerateSettlementPlanRun(ctx,runID int64) error`只接受持久run ID，内部factory校验private authority，不接受调用者传actor、amount、recipient或当前政策来覆盖run。

- [ ] 红测`TestSettlementPlanGenerateReservesOriginalIntentOnce`：两个worker同商户/同day、两个重叠plan、政策revision变化均只一原申请、一reserve、一budget use；proof/audit故障事务全回滚。
- [ ] 红测`TestSettlementPlanGenerateUsesEligibleFundsAndOriginalRecipient`：D1旧钱/新钱、active PS/PF、referral escrow、已有结算reserve、recipient改版、disabled/optout、最低/最高/留存及原fee边界；不得借别单或已有reserve。
- [ ] 红测`TestSettlementPlanRevocationAndCrossMidnightWait`：授权撤销/到期、预算耗尽、锁等待跨日只产生固定skip/blocked或day_changed，不能换business_date/外部号。
- [ ] 实现原申请事务与item/budget use/HMAC同commit；所有结果固定reason enum，展示不含上游自由错误文本。
- [ ] `go test ./internal/payment -run '^TestSettlementPlanGenerate|^TestSettlementPlanRevocation' -count=1`red→green后，再回归已有`^TestSettlement(Policy|Quote|Recipient|Funding|Payout|Bank)`。

### Task 5：专用console与可观察运行记录

**Files:** 新`internal/server/settlement_plan_console.go`、`funds` resolver分支及HTTP contract tests；新Vue计划/运行组件，替换菜单映射时保留旧cron资料的明确说明。

**API:** `C=/console/v1/admin`；GET/POST `/settlement-plans`，GET/PATCH `/{id}`（expected_revision），POST `/{id}/authorizations`，POST `/settlement-plan-authorizations/{id}/revoke`，POST `/{id}/preview`，POST `/{id}/runs`（已有slot的恢复意图，不任意历史补款），GET `/{id}/runs`，GET `/settlement-plan-runs/{id}`。preview只读，默认generate_only，run创建返回202及run ID。

- [ ] 实际HTTP红测role/super/Issuer/CSRF/strict JSON/无grant/错版本，以及从preview→明确授权→run→原pending request的闭环；数据变化后旧授权不能放宽scope。
- [ ] UI展示L/E/R/G/Fee/Net、业务日、auth有效期/范围/余量、原recipient masked config、created/skipped/blocked及原request链接；按钮只显示已验收能力，不将pending表述为付款完成。
- [ ] Vue测试金额大整数、小时0/继承nil、run202不显示成功、未知原号回查；无token/凭据写URL或storage。

### Task 6：单独开放受限自动payout，不能成为首阶段隐含内容

**Files:** 新`settlement_plan_payout.go`与独立TLS tests；对既有`Prepare/ExecuteSettlementPayout`增加私有共用primitive和受计划proof约束的system审计分支；只读worker保持不提交。

- [ ] 先红：generate_only authority永远不能approve/prepare/execute；人工一次grant不能作为plan authority；撤销发生在claim前/claim后均不会重新外呼或重臂；audit失败claim事务回滚。
- [ ] 同事务消费具体原intent的有限auto_payout authority，固定channel/app/account scope、原申请Net和专属reserve；bank单独使用现有payout生命周期，provider/service fees上限计入授权。
- [ ] 真实本地TLS红→绿：accepted pending、unknown→query success/failure、错误签名/金额/TF/原物理身份、DB result故障、并发多worker、银行ACCEPTED/PAID/REVERSED；POST最多1、query可多次、原fee净额恒等且checker0。
- [ ] 自动付款不得扫描所有今天status0自由申请，只处理本auth/run绑定的原request；若兼容原“当天列表”范围，需管理员明确授权固定request集合，独立成员digest，不暗中拾取人工新申请。

### Task 7：阈值Transfer族与最终验收

**Files:** 新`settlement_threshold_policy.go`及tests，只在Task6通过后实施；使用独立family字段，不让普通Settle公式替代其费用规则。

- [ ] 红测严格`>`、最多5个、UID降序、仅支付宝、group禁自动/fee覆盖、仅fee max，以及普通/threshold原意图互斥；同日补款不自动第二次出款。
- [ ] 明确新安全限制与Pro差异，在有限日次数授权尚未提供前拒绝同日多意图；任何失败不自动关全局通道。
- [ ] 保存全部红绿、Windows全库/vet/build、Linux `-race`、迁移重入/checksum、旧单笔结算/退款/分账/奖励回归的固定候选结果。只有本地假通道验收，不做生产交易。

## 7. 阶段完成口径

首个可执行安全阶段由Task1–5构成，结果是**已授权自动生成待审核结算申请**，不是自动付款；可先固定零点D1且不开放组maturity hour。Task6/7分别有独立权限模型与验收门槛，默认关闭。结算批次、打印、模板与其它cron族不在本计划假装完成，继续按各自证据另行落地。

以上保留最初规划。2026-10-23 执行记录：Task1–5 已形成 schema60 开发候选，包含固定商户名单、商户主动加入、收款配置版本、有限金额/次数/期限授权、不可变运行与明细、独立默认关闭执行器、双后台和账本检查。专项测试及独立审查已经运行，完整候选回归和 Docker 部署验收仍须另行记录，不能由专项结论代替。

实际首阶段采用现有全局结算政策与固定金额留存；不开放组级成熟小时、比例留存或组费用新规则。执行时间只开放 hour/day/once，按当前 UTC+8 业务日生成，不补历史资金。创建运行的 HTTP 同步返回 200/201 及原运行，独立任务负责逐商户生成；未提供另一个容易过时的 preview 接口，资金确认挑战直接展示并绑定实际商户、掩码收款配置、费用、成熟与预算快照。Task6 自动出款和 Task7 阈值转账尚未实施，不在本候选验收范围。

独立审查发现并修复旧授权指针回退、损坏来源被人工审核继续使用、账户锁等待期间成熟政策变化及共享申请助手晚失败留下预留的问题。保留原失败断言和失败日志；新生成仅产生待审核申请，不表示付款成功。证据见 `docs/verification/settlement-plans-development-2026-10-23.md`。没有生产访问或真实资金/打印调用。
