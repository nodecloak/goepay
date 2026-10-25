# Restricted Settlement Auto Payout Implementation Plan

> **执行方式：**使用本会话已有 test-driven-development、verification-before-completion 与 collaboration。先由父任务审阅此设计，再分配唯一文件owner实施；当前只写设计，不修改业务源码或迁移。保留真实RED/GREEN、明确独立schema的故障降级及固定源码SHA。

**Goal:** 只允许专用、有限、可撤销的自动出款授权，审批、准备和提交原结算计划生成的原申请；每笔请求仅一个TF和一次发送机会，未知结果只查原号。

**Architecture:** schema65在schema60生成授权之外增加独立的 `auto_payout` 执行授权、固定产品/物理付款账户路由、不可变执行意图、预算消费、唯一发送claim及可信结果HMAC。原39/40/43/44结算金额、专属reserve和银行生命周期继续共用；人工入口保持原权限和资金确认，系统入口只能消费自己的原计划证明。

**Tech Stack:** Go、PostgreSQL/pgx、既有任务队列、AEAD/HMAC与本地TLS模拟通道。

**Spec:** `docs/superpowers/plans/2026-10-23-settlement-scheduling.md` Task6；`docs/design/settlement-wallet-payout.md`、`settlement-bank-payout.md`、`settlement-funding.md`、`settlement-terms.md`。

## 1. 已有能力和不能作出的推断

schema60已经提供固定商户集合、主动加入的收款配置版本、D1/留存/费用原快照、有限金额/次数/有效期的生成授权、按原item生成的唯一申请及跨计划商户/业务日去重。它的权限是 `reserve_pending_request`；即使原授权仍有效，也不能自动批准或发送付款。修改设置里的cron资料不创建系统资金权限。

`PrepareSettlementPayout` 目前只接受真实admin actor，检查原gross专属reserve、原recipient、channel/provider/AppID/产品，然后创建唯一TF，TF amount=cost=net且普通transfer fee=0。`ExecuteSettlementPayout` 另消费一次资金授权，提交claim、审计、只读补查任务后才发送；unknown和claim后崩溃不会重新置unsent。`QueryTransfer` 及原银行退票规则能够核对原TF、物理账户、金额和可信通道响应。

自动入口不能把上述人工方法的actor随意改成 `system`，不能在context内伪造admin/grant，不能让worker保存资金密码、OTP、cookie或旧一次性grant。新增执行授权只允许完成本文列出的固定动作。

真实物理付款身份必须比当前 `adapter.TransferAccountScope` 的弱fallback更严格：自动产品必须同时实现 `ApplicationIdentity` 与 `TransferAccountIdentity`，两者均非空，scope必须为account级。只宣称provider/application级身份的测试或插件不能获自动权限。

**费用能力边界：**现有银行adapter只提供事后 `BankFeeKnown` 或支付宝estimated fee，没有发送前可验证且可执行的费用上限/报价。付款后发现超帽不能算发送前遵守授权。65首版建议只开放经代码证明没有额外自动扣款、费用明确的钱包产品；银行卡自动出款返回 `ErrUnsupported`，保留现有人工银行出款/退票能力。若父任务要求同阶段开放银行卡，必须先增加能证明发送前费帽的真实产品capability及本地签名契约，不能把unknown fee写0，也不能将estimated fee当actual。此项为需要审阅的产品边界，不是假称Pro已证明的语义。

## 2. 最小权限和类型模型

保留原60 definition和生成授权不变。新增的执行授权 `Purpose="auto_payout"` 是独立权限，精确引用 `plan_id + plan_revision + generation_authorization_id + definition_hash + generation_authorization_hash`。原plan view仍可显示generate_only；UI另明确显示“已授权自动付款”及其执行授权ID，不把60原权限升级。无需修改60历史HMAC算法或迁移旧计划。

执行授权允许未来由这一个原生成授权产生的created items；不能收养历史人工申请、其它生成授权的申请、manual已approved/prepare的申请或没有可信scheduled source的记录。60授权变更/撤销、definition新版本、商户偏好/收款版本/全局政策变化均阻止新的自动动作。已完成金融事实仍按原证明读取，不因当前配置失效而变成坏账或伪造历史。

每个merchant指定一个固定route：merchant ID、channel ID、provider、AppID、物理payer account digest、精确product、recipient config ID及hash。账户密钥可轮换，但真实付款账户改变使旧执行授权失效；不能失败后自动切备用channel或同provider另一个Mch。收款身份始终解密原申请快照，不从当前商户资料重建。

输入限额全部使用规范整数分/正整数ID字符串。有效期整微秒UTC保存，最长31天，必须是原生成授权有效窗口的子区间，起点可在未来。每笔、每业务日和本授权累计都有gross/net/settlement fee/provider fee上限及claim次数上限，所有上限必须显式有限；不允许0表示无限。provider fee允许明确0帽，仅用于已证明不产生额外费用的产品。自动付款不得提高原60的单笔/日gross帽或日次数，D1与零点业务日不变。

```go
type SettlementPlanPayoutRouteInput struct {
    MerchantID int64 `json:"merchant_id,string"`
    ChannelID  int64 `json:"channel_id,string"`
    Product    string `json:"product"`
}
type SettlementPlanPayoutCaps struct {
    Gross, Net, SettlementFee, ProviderFee int64 // 全部最终DTO使用,string
    Claims int64
}
type SettlementPlanPayoutAuthorizationInput struct {
    ExpectedAuthorizationID int64
    ExpectedPlanRevision int64
    GenerationAuthorizationID int64
    ValidFrom, ExpiresAt time.Time
    PerItem, PerDay, Total SettlementPlanPayoutCaps
    Routes []SettlementPlanPayoutRouteInput
}
```

内部resolved scope另含route物理账户/product能力证明及原60全scope hash，全部HMAC封存。公开view只返回masked付款账户、masked recipient、产品、限额/已用/剩余、有效期、原生成授权链接；不返回AEAD、HMAC、完整payer/receiver账号或密钥。外部输入不能直接指定account scope/hash。

## 3. schema65持久层

冻结1..64不改。新增 `000065_restricted_settlement_auto_payout.up.sql`：

| 表 | 必须事实与约束 |
| --- | --- |
| `settlement_plan_payout_authorizations` | append-only；原plan/revision/generation auth、前一执行auth ID、有效期、caps、resolved scope JSON、super actor及HMAC；单plan前驱链严格线性 |
| `settlement_plan_payout_heads` | 每plan唯一当前指针；CAS；只能指向同Tx合法新授权/显式撤销后的disabled；延迟校验latest，不允许SQL rewind旧授权 |
| `settlement_plan_payout_revocations` | 每执行auth唯一撤销，actor/时间/原hash/HMAC，append-only |
| `settlement_plan_payout_intents` | request ID唯一、原item/run/source/generation auth/执行auth/route/金额、原funding account+journal+recipient hash、queued时HMAC；不能另选externalNo/TF |
| `settlement_plan_payout_claims` | intent/request/TF唯一；原pre-IO金额/caps/route/hash、business date、claim clock及proof；claim后不能重置，不能DELETE后重领 |
| `settlement_plan_payout_budget_uses` | 唯一claim/intent/request，对原gross/net/两类fee与count的不可变消费；消费不因失败/取消/退票/新授权而回收 |
| `settlement_plan_payout_observations` | 仅可信submit/query结果，原claim/TF/account/product、provider ID/金额/fee known/fee amount、原state→新state、source及HMAC；不能以SQL状态缓存倒推可信结果 |
| `settlement_plan_payout_blocks` | 未claim的持久blocked原因/原head或scope；不删除intent或更换auth，授权丢失响应仍原号只读恢复 |

辅助投影 `settlement_plan_payout_authorization_invariants`、`...intent_invariants`、`...claim_invariants` 从父表出发，检查跨order/merchant/run/request、原预算、真实系统审计、outbox、唯一TF及原reserve，不以INNER JOIN缺行消失当有效。

新intent的自动审批必须有原intent+本Tx系统审计+私有权限证明；claim必须有原prepared payout和本Txbudget use+query job+系统claim审计。新自动源都有固定birth-version，迁移前无该源的人工/60记录保持原语义，不能允许新INSERT显式填写legacy版。

SQL检查HMAC仅形状，Go公开读和关键写入口必须重算；扫描分页100，不能 `array_agg` 无限历史。checker从auth/head/intent/claim/预算/job/原payout父表发现缺行、孤儿和零HMAC。至少新增 `InvalidSettlementPlanPayoutAuthorities` 与 `InvalidSettlementPlanPayoutIntents`，CLI遇非0失败，restore沿用同读验，不生成修复proof。

## 4. 资金公式和原来源

只使用原request的原专属 `settlement_reserve(scope=requestID)`。原39terms固定 `G=N+Fsettlement`；TF传N，cost=N、普通transfer fee=0，不再次扣merchant.available，不套普通代付transfer_rate。60已核实的D1、留存、日去重不因65重复计算而放宽；65原auth scope须与当前有效scope一致，不能把退款/充值/hold/PS/推广escrow当新出款钱。

钱包可信success：原scope N→clearing，原Fsettlement→fees，按原生命周期一次入账。明确失败：原gross回原merchant.available。pending/accepted/unknown保持原gross，不提前确认手续费、不创建新TF。原bank若未来满足能力门槛，仍单独走44hold-and-return-v1，不复用钱包success费用确认规则。

provider fee是付款方产品成本，和merchant settlement fee独立。首版自动钱包实际额外provider fee必须为已证明的0；如将来扩展非0产品，执行授权须有可信pre-send quote或服务端可执行fee limit、币种/有效期/原请求digest，额外费用由平台独立来源/预算承担，不能挪用merchant gross或未授权available。quote过期、unknown、费用维度不明或不能证明最坏费用上限，claim前拒绝；事后回报超限只进入审查，不伪造退款/成功。

## 5. 授权、锁和撤销边界

新授权/扩大范围必须真实super、无Issuer委托会话、CSRF通过，且非nil request-local资金guard，**即使全局资金确认off也必须确认**。`kind=settlement-plan-payout-authorize`，key=`planID:planRevision:generationAuthID`；同q封全原60scope、route身份、caps和expected execution auth ID。同号授权响应丢失只能读原不可变授权，不用另一grant悄悄追加第二条。撤销同plan锁、真实super与审计；不需要外呼。

统一新自动写锁顺序：plan及执行head → 原60授权/head → 该plan/day预算门 → merchant(升序) → 原request → transfer → bank生命周期 → merchant FK KEY SHARE(如前面已锁则无需重复) → 本动作所需完整账户ID升序。原生成器已经plan→merchants；新增自动路径沿用。禁止先锁账户后审批audit隐式等待merchant。只读query/resolver不反向获取plan/head排它锁；只验不可变原claim与结果，避免transfer→plan反序。

claim事务在全部等待锁后使用 `clock_timestamp()` 再核有效期、当前head、原60授权活性、业务日、caps余量、current route和商户开关。事务级 `now()` 不作为过期/跨日依据。日预算以同plan/UTC+8业务日所有历史执行auth累计，切新auth/definition不清零；60跨计划同merchant/day唯一intent仍独立有效。授权累计限额以同执行auth全部历史claim累计，不使用仅当前pending查询。

**撤销线性化点是唯一发送claim提交。** 撤销先获得plan锁则新claim失败、零IO；claim先提交则最多授权这一笔原TF的一次尝试，撤销不承诺撤回已开始的远端动作。提交后、socket IO前再做新鲜只读head/时间/物理账户/AllowLive检查，若已知失效则不发送；原claim仍保留，只读原TF，不重臂。DB检查与实际网络之间不可原子，UI和文档必须诚实说明这极小in-flight窗口，不能宣称“撤销响应后必定没有此前已领取的付款”。不在数据库事务内等待外部网络。

claim后进程崩溃/ctx取消/预发送复核失败，没有新的owner可发送。reconcile只能query原TF；原通道not-found不是“未发送”的可信证明，不自动回unsent，不更换externalNo或provider。只读查不受当前auth撤销/到期阻断，但必须原物理账户与真实签名匹配。明确人工取消仅原unsent、无claim的证明，使用现有真实admin资金guard；claimed不能执行unsent取消。

## 6. 执行器和私有权限

新增默认关闭的独立 `cmd/settlement-payout-worker`，开关 `GOEPAY_SETTLEMENT_AUTO_PAYOUT_ENABLED=false`。任务payload只允许规范 `{"intent_id":"正整数"}`，拒绝重复键/未知字段/多JSON/超长；不接actor/channel/amount覆盖。现有provider只读worker继续只查，不增加发送分支。开发环境仅合成本地connector；生产/真实凭据/真实资金交易不属于本次验收。

coordinator只枚举由指定run的created items生成且request仍pending的原记录，分页100；为通过当前65scope的记录原子写intent+唯一outbox。不能扫描“今天所有pending/approved申请”。60生成与65enroll解耦时，pending仍真实显示“待审核”，不能显示自动出款成功；coordinator失败可幂等重新enroll，原intent存在只读返回。任务lease超时并不重置business claim，dead任务只在未claim时允许CAS原task的幂等人工恢复，恢复不换intent/auth/TF、不重置预算。

执行时同一Tx完成原pending审批、准备TF、领取唯一claim、预算消费、系统原意图审计和只读query outbox。其中任何一步失败回滚整个Tx，原request仍pending、reserve不变、零IO。提交后由唯一claimant调用原adapter。这样撤销不留下“已自动approved但尚未获得发送权限”的中间权限。

抽取原人工共享primitive，将权限检查留在不同入口。私有 `settlementPayoutPermit` 不导出，无公共constructor；只能在本Tx已验证65intent后创建，保存exact pgx.Tx、request/item/intent/auth ID、action，且单次消费。primitive核对action、实际Tx指针、原request等，不能靠system actor字符串放行。固定审计主体 `system:settlement-auto-payout`，其每个approve/prepare/claim/result事件都必须关联原intent/proof；原super授权actor单独保存，不伪装worker为super。

普通人工申请和仅60 generate-only的申请继续人工审批/准备/确认；没有65绑定时不受本功能侵入。已绑定65intent的记录禁止从人工execute旁路制造第二个无65claim的发送；真实admin仍可对**未claim**进行原unsent取消，原审计和资金确认不能省。自动intent失败不暗中转人工自动发送，UI明确显示原失败/unknown及只读恢复。

```go
// proposed public interfaces; no implementation authorized yet
func (g ProviderFlow) SettlementPlanPayoutAuthorizationSnapshot(ctx context.Context,
    q referralReader, planID int64, in SettlementPlanPayoutAuthorizationInput) (json.RawMessage,error)
func (g ProviderFlow) AuthorizeSettlementPlanPayout(ctx context.Context, planID int64,
    in SettlementPlanPayoutAuthorizationInput, actor SettlementPlanActor) (SettlementPlanPayoutAuthorizationView,error)
func (s *Store) RevokeSettlementPlanPayoutAuthorization(ctx context.Context,
    planID, authorizationID int64, actor SettlementPlanActor) error
func (s *Store) SettlementPlanPayoutAuthorization(ctx context.Context,
    planID int64, actor SettlementPlanActor) (SettlementPlanPayoutAuthorizationView,error)
func (s *Store) EnqueueSettlementPlanPayouts(ctx context.Context,
    runID int64) ([]SettlementPlanPayoutIntentView,error)
func (g ProviderFlow) ExecuteSettlementPlanPayout(ctx context.Context,
    intentID int64) (SettlementPayout,error)
```

`SettlementPlanPayoutAuthorizationView`包含id/plan/revision/generation auth ID、上述input、masked resolved routes、revoked、created_at以及已用/剩余caps；内部scope/proof均 `json:"-"`。`SettlementPlanPayoutIntentView`包含id/item/run/request/authorization ID、state/reason、G/N/两类fee帽、product/channel、task ID/state、原TF和masked recipient。所有ID/金额为字符串；state queued/claimed/pending/unknown/succeeded/failed/blocked不互相冒充。公开授权snapshot仅供funds resolver，不能直接回JSON给前端。

## 7. 可信结果封存和缓存防追认

新增自动源的可信submit/query必须在同一结果事务先验原claim/TF/recipient/account/amount，再封HMAC observation并推进原resolver/账本，最终还核对应journals。不能在读到`provider_transfer_attempts.state=succeeded|failed`时补生成结果proof；这会重演59发现的未签名op cache追认问题。缺新自动结果证据或SQL形状32bytes伪proof都返回ErrConflict，checker>0。

普通QueryTransfer/Transfer/SettlementPayout、unsent cancel、resolve共享入口必须先读验65源（无65绑定的历史人工路径返回nil），防从另一入口绕过证明。可信query可以纠正不可信缓存，但只能以本次已验签结果及原非终态事实为依据，受专门恢复证明/触发器约束，不能把已证实终态任意反转。银行reversal是独立可信观察，不当作同一钱包终态反转。

可信财务结果、结果proof和其必要审计同Tx；通道已受理而DB失败则原claim和reserve保留，返回unknown，后续只查原TF。另一次展示用结果audit失败不能触发重发。查询签名、TF、provider trade ID、net、付款account/app/product以及可证明的fee任意不符，不释放也不重发。

## 8. TDD任务和唯一文件owner建议

### Task1：类型化执行授权、有限caps、完整历史与mandatory资金确认

**Create:** migration65、`settlement_plan_payout_authority.go`、`settlement_plan_payout_history.go`、`settlement_plan_payout_authority_test.go`。
**Modify:** 父任务的funds resolver/strict HTTP与OpenAPI，不改60历史proof算法。

- [ ] 先真实RED：只有60授权或传system/admin伪actor、Issuer、operator、全局确认off但无guard，一律零授权记录/零IO；严格duplicate keys/未知字段/非规范ID/金额均拒。
- [ ] 无有限caps、超31日、超过原60有效期/金额限制、弱physical identity、未支持product、bank缺pre-send fee证据均ErrInvalid/ErrUnsupported；同q授权摘要精确绑定原scope和当前物理账户。
- [ ] 并发CAS只一新auth、旧授权重放不消耗第二次grant；audit故障无新head；正常SQL不能rewind/降出生版本，隔离schema明确降级head后reader/checker仍拒伪HMAC。
- [ ] 实现授权链、mandatory ConsumeRequiredTx及UTC微秒规范化；通过首RED原断言后才继续任务2。

### Task2：原item enroll和原子approve/prepare/claim

**Create:** `settlement_plan_payout.go`、`settlement_plan_payout_claim.go`、`settlement_plan_payout_test.go`。
**Modify:** `ledger_requests.go`、`settlement_payout.go`、`settlement_payout_flow.go`只提取私有共用primitive；人工权限判断原样保留。

- [ ] RED：manual申请、另一plan/run/auth、同merchant但另一recipient、manual已批准/prepare、伪造scheduled source均不能enroll；generate_only authority不能自动批准。
- [ ] 单Tx每步audit/recipient/intent/TF/claim/job故障注入，必须原request pending、原reserve完整、无TF/claim/预算、零发送；原资金完全不从available再扣。
- [ ] 8并发同intent只有1TF/1claim/1budget/1POST；不同plan/新auth/失败退回/取消都不能重置日额度或原merchant/day去重；MaxInt64与差值比较无溢出。
- [ ] 原单gross/net/fee/recipient/route任一变化使旧snapshot失效；人工普通流程和60人工审核/出款保持原测试通过。

### Task3：唯一claimant IO、可信结果、撤销/到期/锁等待

**Create:** `settlement_plan_payout_result.go`、`settlement_plan_payout_tls_test.go`、`settlement_plan_payout_lock_test.go`。
**Modify:** `transfer_batch_settlement.go`、`provider_transfers.go`的自动源共同读验/可信结果Tx钩子；银行方法只在能力已证实时接，不放宽费用。

- [ ] 本地Alipay RSA及WeChat签名TLS：accepted→pending，unknown→原号query成功/失败，POST≤1；错误签名/金额/TF/Mch/AppID/product/fee不推进账本。
- [ ] 实际PG blocker令claim等待跨有效期或跨UTC+8零点，使用clock_timestamp重新拒绝；撤销先于claim零IO，claim先提交后撤销不重发不重臂，预IO检查发现撤销则不发送，保留claim而不是释放预算。
- [ ] claim提交后ctx取消/进程中断、Provider accepted但result insert/audit故障，原reserve仍在，只读query恢复一次；不能not-found→换新号。
- [ ] SQL完整假failed/success+balanced journal+32bytes伪proof，所有公读、claim、resolve、query缓存短路与checker拒；第101条证明实际分页HMAC。

### Task4：outbox执行器、后台摘要、完整性与固定验收

**Create:** `cmd/settlement-payout-worker/main.go`、专用runner和纯payload测试、`settlement_plan_payout_check.go`及独立review tests。
**Parent modify:** 默认关闭compose profile、Docker binary、server/UI/CLI/restore、OpenAPI。

- [ ] JSON重复/未知键和多对象拒绝；lease丢失不重发，业务claim优先于queue状态；dead恢复必须原task CAS幂等，不能自动复位预算。
- [ ] UI展示有限系统权限原scope、两层授权有效期/撤销、已用和剩余、G/N/费用、masked付款/收款资料和原request/TF；202/queued/accepted/unknown均不显示到账成功。
- [ ] 正常guard开启验证缺intent/claim/result/job/预算/系统审计不能commit；限定隔离schema的坏恢复验证parent-based checker能发现所有缺投影和零HMAC，CLI/restore非0失败。
- [ ] 固定源码后Windows全库-p1、Linux完整race/vet、原人工结算/银行退票/普通退款/63held refund/58奖励回收/PS/PR/PF/60生成器均回归。只有本地Docker/TLS合成数据，未实际发送生产交易。

## 9. 开放门禁和分工

只有独立授权、同Tx原审批/准备/claim、唯一发送与可信结果闭环、只读恢复、checker以及撤销锁等待全部通过后才打开本地演示开关。不能部署只有autoapprove没有claim/result校验的中间能力。

建议核心owner独占65迁移、自动源/结果证据及其与payout私有primitive；第二owner独占授权/head/history/预算；父任务独占真实funds/HTTP/UI/CLI/Docker接线并组织独审。`settlement_payout*.go`、`ledger_requests.go`、`provider_transfers.go`改动必须先确认唯一owner。63核心已冻结，不能顺带改退款资金模型。

当前文档仅为可执行安全设计。65尚无迁移或业务代码、尚未运行65测试，也没有启用自动付款。需要父任务审阅费用产品门槛与claim线性化语义后才能实施；用户已授权本地连续开发，无需重新向用户申请普通实施权限。
