# Frozen Order Income Refunds Implementation Plan

> **执行方式：**使用本会话已有 test-driven-development、verification-before-completion 与 collaboration 工作方式。由父任务统一接线和独立审查，每项保留真实RED/GREEN和固定源码证据；不依赖未安装的技能。

**Goal:** 支持冻结订单收入期间的真实退款，同时保证每笔退款只使用已明确授权的本单冻结本金、商户可用补足、原手续费及原推广奖励回收来源。

**Architecture:** 新建按 hold ID 隔离的收入 reserve；冻结期间的退款另使用按 refund ID 隔离的 merchant reserve，来源份额和账本证据不可变并带 HMAC。继续复用现有退款号、ProviderFlow、schema59终态证明与schema58推广撤销，不另建通道退款流程。历史混合冻结池保留并通过显式、可审计的逐单迁移分离；未完成分离的旧 hold 继续拒绝冻结期间退款。

**Tech Stack:** Go、PostgreSQL、pgx、现有复式账本/资金授权/任务队列、本地合成 TLS 通道。

**Spec:** `docs/design/payment-fees-ledger.md`；本计划第二至六节是本增量的明确安全政策和验收规格。

## Global Constraints

- 工作目录 `E:\GOEPAY`；本次只读研究、只新增此计划，没有修改业务源码、迁移或测试。
- 不连接生产、不读取 `.secrets` 或实际支付配置/密钥、不访问外部交易接口、不发起真实资金交易。
- schema1..62 已冻结，不修改其文件或checksum；申请schema63新增并替换所需SQL函数/视图。
- 原订单 gross、merchantNet、fee、退款承担方快照不可修改；原支付状态继续为 paid，冻结状态独立。
- 每次资金变化都必须具备原主体审计、完整复式分录和可验证来源。不能仅因钱包或混合池有钱而消费其它原单冻结资金。
- 所有退款仍经现有原退款号、商户/通道身份校验、ProviderFlow提交及可信只读查询；accepted不等于成功，unknown不释放、不换号绕过、不重发。
- 不放宽原D1总额规则，不把冻结净额、剩余净额或推广回收额当作 gross；不自动发起分账回退或解冻。
- 每项实际TDD先记录真实RED、再最小实现GREEN；PostgreSQL测试采用独立本地端口、随机schema、正式 `db.Migrate` 和 `-p 1`。

---

## 1. 已证实行为与当前缺口

### PHP明文已证明

`epay_pro/app/lib/Order.php` 自身是明文，无需借Release推断：

| 原证据 | 可以证明 | 不能据此推定 |
|---|---|---|
| `:9..24` freeze | 只处理status1，排除商户直清，扣除getmoney后改status3 | 具备独立冻结账户、并发资金来源证明 |
| `:26..41` unfreeze | 只处理status3，按原getmoney返还余额 | 部分退款后的剩余冻结本金怎样恢复 |
| `:43..65` refund_info | status1/2/3均可查询可退款金额 | 有现代pending/unknown预占 |
| `:68..102` refund | status3可退款；`noreduce || status==3 || mode==1` 时 `reducemoney=0` | 可以从任意冻结订单池支付，或直接把费用/推广资金当商户本金 |
| `:105..123` | 调用插件后按reducemoney扣钱包；冻结分支跳过再次扣钱包 | 插件受理就是上游退款终态 |
| `:133..139` | API退款后更新status2、refundmoney和refundorder | 部分退款后允许继续调用要求status3的unfreeze |
| `epay_pro/app/common.php:425..454` | changeUserMoney锁商户余额，记录一次余额变动 | freeze、插件调用、退款状态、冻结余额形成跨步骤原子事务 |

Release `includes/lib/Order.php` 有相近分支，可作同版本旁证。Pro的上述方法不能证明通道插件的全部异步返回语义，也不能证明加密控制器的权限与接口返回结构。

**新政策差别必须公开说明：**保留“冻结期间可以退款”和“不重复扣除已冻结本金”的能力；不复制单一status改变、按原getmoney全额解冻或缺少剩余本金归属的做法。新实现部分退款成功后，未消费的本单本金继续冻结，直到原权限主体显式解冻；未决退款期间禁止解冻。新政策不是宣称Pro原有内部事务已如此实现。

### Go当前事实

- `order_income_hold.go:55..66` 和migration33的 `order_income_refund_guard` 目前一律拒绝held订单新退款。
- `FreezeOrderIncome` 将净剩余收入从available转入 `(merchant,order_income_hold,scope=0)`。池内总余额受migration52守护，但不同订单本金没有独立账户。
- `UnfreezeOrderIncome` 目前固定返还不可变 `h.amount`；不能直接加入退款后仍全额返还。
- `reservations.go` 已在原order锁下分配merchant/fee，非failed退款占用额度；失败释放，unknown保留。
- migration52的原收入视图明确扣除成功退款商户份额、可信渠道分账净出流、未拒绝内部收益分配；冻结不以当前钱包余额扩大原单预算。
- schema55要求原渠道分账资金有可信PS/PF放款证据后才允许普通退款；schema53仅可信成功PR恢复原收入净出流额度。
- schema58已实现原推广R全额预留及费用贡献K；schema59已实现provider/internal终态HMAC、缺证据拒绝和不可信operation cache的只读恢复。

## 2. 金额口径及分配算法

全部为非负int64分。计算先比较再做减法；SQL聚合用numeric验证边界后转bigint，不允许先溢出再截断。

记原支付 `G=orders.amount`，`N=merchant_amount`，`F=platform_fee`，始终 `G=N+F`。本次退款gross为q。读取并验签/验账全部历史退款后，令 `UG/UM/UF` 为非failed gross/merchant/fee累计。

```go
if UG > G || q <= 0 || q > G-UG { return ErrInvalid }
if payer == "platform" {
    if UM > N || UF > F { return ErrConflict }
    M = min(q, N-UM)
    f = q-M
    if f > F-UF { return ErrConflict }
} else { M, f = q, 0 }
```

`M` 是商户承担额，`f` 是平台手续费承担额。保持原收费规则，不能因held而把M设为0，也不能因推广回收而减少q/M。

某次冻结原本金固定为 `H0=h.amount`，其出生快照满足：

`H0 + 已成功退款商户额Q0 + 渠道净出流S0 + 内部未拒绝分配D0 = N`。

原快照不可改。当前本hold可用本金 `C=H0-Σ(H_i | refund_i.status != failed)`，仅统计绑定本hold的可信退款来源份额。未解冻时：

```go
H = min(M, C) // 本单冻结本金
A = M-H      // 商户available明确补足
// q = H + A + f
```

`pending/unknown/succeeded` 都占用H额度；仅可信failed归还本hold。没有active hold时H=0、A=M，保留原普通退款路径。相同退款号重放返回原H/A/f和原账户，不根据当前hold状态重新计算。

### 独立来源规则

| 来源 | 允许用途及限制 |
|---|---|
| 本hold专属reserve | 仅原order、原merchant、原hold ID；H不能超过C |
| merchant.available | 只为明确的A补足，须真实余额足够；可承担商户退款费用或已分账/已内部分配后的商户债务 |
| 其它订单hold、结算reserve、渠道sharing reserve | 不可借用，不自动解冻，不因总池余额足够而放行 |
| 原手续费F预算及platform.fees | 沿用原单UF/f上限；平台可用手续费钱包仍须足额，不能借fee_reserved或其它托管账户 |
| 原推广奖励R | 保持schema58原债务和一次性撤销，不当商户H/A；只扣原promoter.available，不借其它奖励escrow |
| 原渠道成功分账S/内部收益分配D | 不能伪装成H；完成放款后若商户自行用available承担退款，明确记为A，不自动替商户执行PR或撤销内部划拨 |

首次推广撤销仍有 `U=F-R-UF`、`K=max(0,f-U)`，并验证原58边界。K只给平台费用承担部分提供来源，不能扩大H、减少A或改变原M/f。

hold建立后有成功PR或内部划拨被拒绝时，返回资金进入available，不悄悄增加H0；后续A可使用它。必须先显式解冻当前hold，再按新的可信原收入预算建立新冻结周期，才能把新增可冻结收入纳入新hold。

## 3. 账户、记录和迁移设计

新增migration `000063_frozen_income_refunds.up.sql`；保留旧池 `order_income_hold(scope=0)` 及原journal，不重写历史。

### 新账户

- `(merchant,order_income_reserve,scope_id=holdID)`：本hold本金。
- `(merchant,order_income_refund_reserve,scope_id=refundID)`：仅冻结期间退款的M预留，A和H各自转入，终态后余额为0。M=0可保留0余额账户，但不制造0金额journal。
- 普通未冻结退款维持原available→reserved路径；fee_reserved和推广escrow不改变账户所有权/作用域。
- 更新账户kind/scope约束并增加不可变账户身份和父记录完整性守护。不得把新账户并入原transfer/settlement可用资金查询。

### 新不可变证据

1. `order_income_hold_fundings(hold_id PK,order_id,merchant_id,account_id UNIQUE,amount,source_mode,funding_journal_id,legacy_pool_journal_id nullable,actor_role/id/name/class,request_hash,created_at)`。
   - fresh：原available→本hold reserve，amount=H0。
   - legacy_adopt：原freeze journal仍为available→旧pool；另有旧pool→本hold reserve迁移journal。当前HMAC明确证明“本次受控adoption”，不冒充历史原freeze已有HMAC。
2. `order_income_hold_releases(hold_id PK,amount,consumed_amount,journal_id nullable,actor tuple,request_hash,result_hash,created_at)`。`amount+consumed_amount=H0`；amount0允许无journal，必须有权限、审计和HMAC。只允许无本hold pending/unknown退款时生成。
3. `refund_fundings(refund_id PK,order_id,merchant_id,hold_id nullable,hold_funding_hash,merchant_amount,hold_amount,available_amount,fee_amount,reserve_account_id,hold_reserve_journal_id nullable,available_reserve_journal_id nullable,request_hash,actor tuple,created_at)`。
   - 新退款均封正/负来源；无hold也封H0/A=M，避免SQL伪造“此单没用hold”绕过来源检查。
   - 新 `funding_evidence_version`：迁移存量为1、新出生为2且不可降版。新版本必须有funding，不允许missing当旧版。
4. `refund_funding_resolutions(refund_id PK,state,held_return_journal_id nullable,available_return_journal_id nullable,consume_journal_id nullable,request_hash,result_hash,actor/system source,created_at)`：仅可信终态；request_hash绑定原funding。与schema59终态证据相互匹配。

请求HMAC必须绑定订单原费用快照、G/N/F、退款号/q/M/f、H/A、原hold及funding HMAC、所有原账户和journal ID、原主体及版本。终态HMAC绑定原请求、state和实际结转/归还journal，不能只查32字节长度。

### legacy池分离

新增 `AdoptOrderIncomeHold(ctx,merchantID,tradeNo,holdID,externalNo,actor)`：仅原super管理员及资金授权，原hold必须仍held、原冻结/审计/原收入证据完整、无任何本hold未决退款，并验证旧pool精确覆盖全部未adopt的held本金。

同Tx将本hold的H0从旧pool转到专属reserve并封存adoption证据；旧pool余额守护改为未adopt held金额总和。相同externalNo/hold重放只读，不能重复转账。其它hold不改、不借、不重封历史签名。无法证明原冻结的hold拒绝adopt，不能靠super权限或池余额补造原来源。

未adopt旧hold继续支持原解冻流程；冻结期间新退款明确报 `income_hold_source_unprepared`。adopt完成之后，原旧解冻journal格式不能再用于该hold。

## 4. 状态、分录及原子性

冻结期间退款预占在一个Tx完成：原退款出生、funding、推广预留/贡献、H和A的merchant reserve、f的fee reserve、原主体审计、原只读reconcile job。任一步失败全部回滚，资金授权也同Tx回滚，不能先提交一部分再出网。

| 状态 | merchant源分录 | 费用及推广 | hold释放 |
|---|---|---|---|
| pending | H:本hold→本refund reserve；A:available→同reserve；余额M | 原fees→fee_reserved；原R进入escrow，按K贡献 | 禁止 |
| unknown | 不增加、不归还，保持同账户同金额 | f与R/K全部保留 | 禁止 |
| succeeded | 本refund reserve的M→clearing一次 | 原f→clearing；原58撤销成功 | H成为已消费本金；剩余C仍held |
| failed | H→原hold reserve；A→原available，各自原路一次 | 原f回fees；K/R按原58逆转 | 失败恢复后才可显式释放 |

不以退款失败为由自动解除订单冻结，不把H先作为普通可用收入解冻。人工解冻仅转C；原H0和已成功退款H分开保留。C=0也只生成一次显式release0证明，旧hold不能重新激活、旧解冻请求不能释放后续新hold。

外部提交保持原唯一claim及固定原出站号。只有可信submit/query可推进provider终态；已知未受理失败和内部合成终态仍使用schema59正确source。不得因可改的provider_operations cache改变H余额，后续原query只能恢复原退款状态。

### 锁顺序

1. 原order `FOR UPDATE`，所有freeze/adopt/reserve/resolve/unfreeze共享；读取等待后的新快照。
2. 原hold/原refund行按明确顺序锁定；不存在跨订单任意锁其它hold的业务动作。
3. payer和positive promoter FK `FOR KEY SHARE` 按ID全集预锁，不检查enabled抹除旧债，不在已有账户锁之后首次取得这些锁。
4. 创建本次专属账户并收集完整账户集合：available、旧pool/hold reserve、refund reserve、clearing、fees、fee_reserved、promoter available、推广escrow。
5. 一次 `lockLedgerAccountSet` 全集ID升序后才允许第一笔post。不能逐journal局部排序，也不能先post推广再补锁hold账户。
6. 读取与验证原HMAC/历史来源并在实际资金锁后重核余额及分配；修改来源/发出授权摘要必须来自同Tx。

所有需要失败回滚的源proof和审计，与原账本同事务；不得以异步任务补关键资金证据。

## 5. 必须同时更新的证据入口

新增SQL helpers：`refund_merchant_reserve_valid(rid)`、`refund_merchant_resolution_valid(rid)`，分别验证legacy普通退款与63新来源的完整两腿分录。SQL检查只有结构权限；Go仍必须验证HMAC。

在63替换而不编辑冻结迁移：

- `order_income_refund_guard`：不再笼统拒held；新held退款必须匹配同order的有效新funding和专属reserve，最终deferred验证，原sharing guard仍独立有效。
- `order_income_hold_funding_evidence`、旧pool/new reserve余额guard、hold释放guard：支持legacy/adopt/fresh三种明确证据模式，防止原H0全额二次返还。
- `order_income_source_evidence`：保持原gross和原source扣减口径，以新helper识别成功held退款，不能因新journal格式导致成功退款从来源扣减中消失。
- `referral_clawback_invariants`：仅原merchant资金证明两项改用helper；R/K原算法、平台费用和推广escrow证明不改。
- `provider_refund_result_invariants`：新merchant terminal来源通过helper；原source、原operation审计/terminal proof/version2守护不降级。

Go调用 `verifyRefundFundingEvidence` 接入 `verifyReferralRefundEvidence` 共同入口，覆盖公开退款读/旧号预占/claim/resolve/query；不制造新的绕过通道。terminal验证同时要求原schema59 proof。所有新退款、冻结/解冻和授权preview必须有界分页验证**全部相关历史，包含failed**，防止SQL伪failed释放预算后由应用给新退款封真HMAC。

低层分离以避免递归：`scanIncomeHoldFunding` 只验父proof/结构，`scanRefundFunding` 只验单条请求/终态及低层父proof；`verifyIncomeHoldRefundHistory` 单独分页计算摘要。禁止funding读取互相调用整段history形成递归或O(n²)数组。

原D1查询仍按gross，partial refund不按净额缩小当天阻塞。held仍从可用钱包中排除原gross；解冻后的今天订单重新按原规则计入。新scoped余额不进入可结算可用额。

## 6. API、授权、检查接口

现有支付接口的退款参数与返回兼容：新功能不接受客户传H/A/f或账户ID，不允许`noreduce=true`这种客户端免扣选择。仍由服务端原订单事实计算。

拟定包内/包外稳定接口：

```go
type RefundFundingSnapshot struct {
    ExistingRefundID, OrderID, HoldID, HoldFundingID int64
    Gross, MerchantNet, OriginalFee, MerchantAmount, FeeAmount int64
    HeldOriginal, HeldRemaining, HeldPending, HeldSucceeded int64
    HeldAmount, AvailableAmount int64
    FundingMode string // available | income_hold | legacy_existing
    SourceHash, HistoryHash, RequestHash, ResultHash []byte // 内部授权快照，不直接展示
}
func (s *Store) RefundFundingPreview(ctx context.Context, q referralReader,
    orderID int64, refundNo string, amount int64) (RefundFundingSnapshot,error)
func (s *Store) verifyRefundFundingEvidence(ctx context.Context,q referralReader,r Refund) error
func (s *Store) AdoptOrderIncomeHold(ctx context.Context,merchantID int64,
    tradeNo string,holdID int64,externalNo string,actor IncomeHoldActor) (OrderIncomeHold,error)
```

新增 `OrderIncomeHold` 只读字段：`remaining_amount`、`refund_reserved_amount`、`refund_succeeded_amount`、`source_mode`、`funding_id`。原amount继续指原冻结本金，不变更含义。钱包另显示 `income_hold_available` 和 `income_hold_refund_reserved`，不能误把新scope余额当作普通reserved或available。

退款资金授权resolver同一q/Tx中封 `RefundFundingPreview` + 原 `ReferralRefundPreview`；摘要明确显示本单冻结H、可用补足A、平台费用f及推广R/K。把hold ID、源HMAC、历史摘要、H/A/f、当前state纳入内部fingerprint。等待锁、失败退款释放空洞、解冻/新周期或PR/内部分配证据变更后必须重新核验，不凭旧钱包截图消费授权。相同退款号只返回已存在原funding。

采用现有原权限主体/CSRF/委托会话限制。新adopt动作仅super，不能通过普通通用记录API写source/hash/account字段。无需增加worker发送功能；原refund.reconcile保持只读补查，不给新号重发入口。

LedgerCheck新增 `InvalidOrderIncomeFundings`、`InvalidRefundFundings`，用父表NOT EXISTS检查缺行/缺投影、新scope孤儿/错误余额，再每100行实际验HMAC及历史。保留unknown统计，unknown是未决事实而非损坏。CLI与restore测试添加字段；只读检查不补签、不修余额。

## 7. TDD实施分工和文件

### Task 1：金额纯函数、来源类型和接口

**Files:** 新建 `internal/payment/refund_funding_math.go`、`refund_funding_math_test.go`、`refund_funding_types.go`。

**Produces:** `allocateRefundFunding(g,n,f,usedG,usedM,usedF,q,heldRemaining int64,payer string) (merchant,fee,held,available int64,err error)`，以及第六节Snapshot接口。

- [ ] 先写表驱动RED：G100/N90/F10，held90，退款98→M90/f8/H90/A0；merchant payer退款100→M100/f0/H90/A10。
- [ ] 写历史空洞RED：原failed95不能占用额度；successful20后held70、退款80→70/10/70/0；unknown仍占用；禁止q超过remainingGross。
- [ ] 写MaxInt64与非法负数/超原F/超原N/heldRemaining超H0等RED；避免乘法，比较后减法。
- [ ] 只实现上述纯函数并运行 `go test -p 1 ./internal/payment -run '^TestRefundFundingMath' -count=1`，保存真实日志。

### Task 2：63 schema、专属hold、新冻结/显式legacy adoption

**Files:** 新建 `db/migrations/000063_frozen_income_refunds.up.sql`、`internal/payment/order_income_funding.go`、`order_income_funding_test.go`；修改 `order_income_hold.go`、`order_income_evidence.go`。

**Consumes:** 原 `orderIncomeSource`、`IncomeHoldActor`、`lockMerchantForeignKeySet`、`lockLedgerAccountSet`、现有资金授权。

**Produces:** 新hold funding及HMAC读验证；`AdoptOrderIncomeHold`；只读剩余/预留/成功金额。

- [ ] 首先RED证明当前scope0只有池总量、不能从本hold获得专属账户；再写新freeze创建独立scope、跨订单source拒绝测试。
- [ ] 用source62派生旧fixture先建legacy冻结，再正式迁移63；验证新默认版本、旧pool账本不变，旧hold退款仍拒。
- [ ] adoption并发8次原号只转账一次，另外hold余额/记录完全不变；原source/actor journal缺失、审计失败、授权失败均全Tx回滚。
- [ ] 完成新freeze +adopt原子逻辑，正常所有数据库guard保持启用；仅损坏恢复专项可以明确降级对应新trigger，之后公开读和checker必须拒绝伪HMAC。

### Task 3：预占/终态分配闭环

**Files:** 新建 `internal/payment/refund_funding.go`、`refund_funding_test.go`；修改 `reservations.go`、`referral_clawback.go` 的全集预锁入口与共同验证调用；63内更新相关视图，不改15/33/52/58/59。

**Produces:** 新funding请求、scoped reserve和terminal resolution；普通未held退款保持原路径。

- [ ] 真RED：原held退款被拒；新期望H→本refund reserve，不再次扣available。
- [ ] 分别验证pending、unknown、failed、succeeded全部账户精确余额/事件号/HMAC；同号并发退款只1次预占，unknown重放不发第二次。
- [ ] merchant payer本金补足A不足时，即便其它hold/PS reserve/推广escrow足额也拒绝，所有新source/退款/推广扣款/任务/资金授权均回滚。
- [ ] 平台承担完整案例G100/N90/F10/R5、退款98：H90/f8/K3，pending时本hold0、refund reserve90、fee_reserved8、escrow2；success后仅fees2、clearing回流98，promotion void一次；failed全部原路还原。
- [ ] 聚合原PS已完成S20后只冻结70(N90)；全额退款要求A20，不自动PR。pending/unknown PS/PF即便wallet富余也拒，可信PR后只恢复真实available，不增旧H0。
- [ ] 保持原内部收益分配独立；pending分配被拒的返款只能成为A可用，不增旧hold本金。

### Task 4：解冻、重放和并发回滚

**Files:** 修改 `order_income_hold.go`；新建 `order_income_refund_race_test.go`。

- [ ] unknown/pending时unfreeze真RED，不能放出hold余款；失败后只还本hold，成功后只允许解冻余款。
- [ ] 退款成功20、原hold90→余款70；解冻只返70，原h.amount仍90；满额消费后显式release0不创建0金额journal。
- [ ] 同订单freeze/reserve/unfreeze/resolve四路竞争只允许与提交顺序一致的结果；无负余额、重复release或跨cycle使用。
- [ ] 商户提现/会员/充值/推广反向路径与本流程同时运行，以真实FK blocker验证merchant KEY SHARE在账户前、完整账户升序；不要靠放松deadlock重试掩盖锁反序。
- [ ] 每个journal/audit/result持久化步骤注入本地故障，验证整Tx回滚；provider已实际受理而DB结果失败时保留原unknown并用原只读query恢复，不能释放或新号再发。

### Task 5：授权preview、HTTP/UI安全展示

**Files:** 新建 `internal/payment/refund_funding_preview.go`、`refund_funding_preview_test.go`；修改父任务拥有的 `internal/server/funds_action_resolver.go`、订单冻结/退款handler及对应Vue组件、OpenAPI和严格client测试。

- [ ] 授权摘要原金额H/A/f/R/K与实际reserve完全一致；同q/Tx，不另开Pool读或假actor；缺grant零IO。
- [ ] preview后另一退款成功/失败/unknown、hold解冻/新周期、可信PR变化使旧snapshot失配，实际事务拒绝旧授权；已有退款号返回原来源不重算。
- [ ] 商户跨owner、委托会话、operator对adopt、body重复键/未知字段、非规范金额/ID均拒绝；普通商户不能传账户或H/A。
- [ ] 真正本地WeChat/Alipay签名退款请求与只读query恢复闭环；不会访问真实host或使用真实证书。
- [ ] UI明确原冻结额/可用余款/退款预留/成功消耗；unknown不显示可解冻；无automatic retry/new number按钮。

### Task 6：证据检查、伪造攻击和升级验收

**Files:** 修改 `ledger_check.go`、`cmd/cli/main.go`、`scripts/restore-test.ps1`；新建 `refund_funding_evidence_test.go`、`order_income_funding_migration_test.go`；父任务接线新检查字段。

- [ ] 正常SQL约束开启：缺funding、跨order/hold/account、伪source mode、旧refund降版本、无正确来源journal或缺actor审计都在commit拒绝。
- [ ] 仅独立schema明确降级新immutable guard模拟损坏恢复：SQL形状完整但zero HMAC的failed不能重开H额度，公共读/preview/ReserveRefund/claim/resolve全部拒绝；checker对应字段>0。
- [ ] 删除投影行或父记录、孤立新scope账户、转账去wrong counterparty、旧pool与专属账户合计不符均不能因JOIN消失。
- [ ] 对任意历史部分退款failed空洞与后续pending组合，验证G/N/F上限、H0守恒；检查不使用array_agg全量历史，100行有界分页。
- [ ] 证明D1：当天部分退款解冻后仍按原gross阻塞；跨日后只发生既有gross规则变化；全退不制造可冻结收入。
- [ ] 普通未冻结退款、历史v1退款、已释放旧hold重放、schema58/59故障恢复以及全部PS/PR/PF回归保持通过。

### Task 7：独立审查和固定候选

**Files:** `docs/verification/frozen-income-refunds-2026-10-23.md`；独立完整候选与SHA256 manifest；不混入61/62冻结候选。

- [ ] 分工建议：一位负责63+hold账户/adoption，一位负责refund来源/推广整合，父任务负责授权/HTTP/UI/checker接线与独立审查；先确认共享 `reservations.go` 的唯一owner，再并行。
- [ ] 用专属本地测试PG跑上述专项真实RED/GREEN，记录source hash/command/exit/duration，文档区分生产能力和合成契约验证。
- [ ] 固定候选再跑Windows全库 `-p 1`、Linux完整race/vet、CLI ledger、restore和相关前端/HTTP；无源码变化后才给最终结论。
- [ ] 完成63全部闭环后才开放冻结期间退款；不得部署只允许reserve但不能可信resolve/failed restore的中间实现。

## 8. 自查结论及待实施状态

已覆盖PHP明确能力、原金额/费用、原hold隔离、旧池迁移、普通available补足、分账/推广独立来源、全部退款状态、actor/HMAC/审计、并发锁和失败回滚、授权/界面/CLI/恢复验证。

2026-10-23 执行记录：父任务已审阅并授权63。Task1–4/6核心已实现，新hold和held-refund使用独立账户；fresh/legacy_adopt、H/A/f分配、58奖励回收、59可信终态、失败原路恢复、pending/unknown禁止解冻、成功后剩余/零额释放已形成闭环。金额函数、真实PG/TLS专项及独立攻击审查均已运行；采用旧池须super和不可缺失的请求局部资金guard，历史证据只封本次采用，不冒充历史签名。29个核心/相关测试文件已固定，具体路径及SHA见 `.tools/checkpoints/63-income-core-evidence-b3a1756c-4a4f-4afd-bc05-3ce0b3116a09/SOURCE_HASHES.json`。

父任务负责Task5的真实授权/HTTP/UI及CLI/restore接线；整个63/64固定候选的Windows全库、Linux race和本地部署验收仍由父任务另行记录。上方复选项保留原逐项规格，不以核心专项代替全库、界面或生产通道验收。最终核心验证记录写入 `docs/verification/frozen-income-refunds-2026-10-23.md`。
