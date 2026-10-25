# 微信独立完结 PF55 专项证据

55 是未部署候选迁移。本专项仅使用合成密钥、本机TLS上游、PostgreSQL 55432随机schema，不访问生产、不发起真实资金交易。

## 实现边界

微信原支付必须有可信分账声明、原渠道支付凭证及有效专属reserve。只允许尚无PS，或原PS的keyed failure_proof证明未受理；pending/claimed/unknown PS不得改用PF。原PS已可信autoFinish并释放时，Preview/SubmitFinish只读返回Source=sharing原结果，不创建PF、不消耗新资金授权；后续普通退款、收入冻结、站内分配不阻止读取这个既有完成事实。支付宝独立PF因缺可信终态契约保持Unsupported，在授权和外呼之前拒绝。

PF封存原支付及原账号证明、gross/net/shared/expectedUnsplit、原actor、原120bit随机PF号的HMAC；原订单锁串行化PS/PF，数据库另行禁止两条释放路径并存。资金授权(kind=channel-sharing-finish,key=mid:externalNo)、claim、原actor审计、只读对账任务同事务提交。

提交受理（包括提交返回FINISHED）仍是pending。只读查询按原交易、原PF号、原账号、渠道号核验：唯一收款方为原MchID、成功金额等于原gross、终态SUCCESS/FINISHED，才从原专属reserve向原merchant.available释放net。平台费保留。每个request/result/failure有keyed证明，结果与原actor/worker审计同事务落库；查询错误、错误收款方/金额/交易、空终态、非终态、丢失结果审计都不能释放。

unknown永不换号或重发。可信failed允许操作者新externalNo+新授权+新PF号显式重申请；新意图封存previous_finish_id。每单至多1非failed，成功释放至多一次；旧externalNo只读原失败，不消耗新授权。Preview取latest，只有分页100复验全历史均可信failed后才返回eligible/ID0/previous_failed_id；较新的unknown不能被旧failed遮蔽。资金来源验证同样扫描全部PF历史。

统一channel_sharing_fund_releases汇总原PS completion与可信PF成功，funding invariant校验原reserve、最终释放journal及账户余额。退款/按单冻结/站内分配/receiver活跃引用/T1使用统一完成条件；PF释放后保留当日gross限制，按单source仍只认原net减普通退款/站内分配，不能借别单余额。

## 真实红绿与审查

- 首条真实TLS+PG：提交后只读worker核验释放一次，stub真实红4.109s，核心实现后4.445s绿。
- 初版全部PF guards 47.527s绿，覆盖12并发只一次grant/外呼、原号未知恢复、各种错误终态与Mch/amount/trade、pending/claimed PS屏障、可信未受理PS、假PS failure HMAC拒绝、PS partial autoFinish只读复用。
- 原PS完成后refund/hold/internal不应阻止读取，以及无obs unknown/failed缺actor结果审计：真实红19.142s；两P2修复后19.332s绿。DB追加max(last observation)、全历史渠道号/终态一致性及原actor结果审计约束。
- 可信failed不应永久冻结：真实红4.485s；新intent+previous_finish_id+全历史验证后，与正向/unknown/假failed组合14.064s绿。
- 真实结果审计故障恢复、gross100/net90只释放90、T1 gross100、退款10+站内20+收入冻结60：10.811s绿。
- 最终fresh `go test ./internal/payment -run '^TestChannelSharingFinish' -count=1`：106.505s/exit0。包含全部自有测试及独立review测试（PS/PF真实竞态、伪query终态+完整journal但随机HMAC被public/worker/source/refund拒绝、SQL birth不许跨pending/unknown PS、旧failed不遮蔽新unknown、原scope切换0错误外呼、可信签名PARAM_ERROR恢复、PF完成可解绑、Ali禁止）。
- 综合 `^PF|^52|^PR` 曾跑188.019s，唯一失败是启动时已编译的Ali fixture缺商品Name；该fixture修复后Ali+52升级6.638s绿，最终完整PF106.505s也已覆盖。该188s命令不能列作全绿证明。
- 55引入统一release函数后，51升级fixture原先“仅skip51、先执行后续迁移”会污染测试：真实红3.642s；改为只载<51后绿3.373s。52升级fixture在断言52旧hold version1后再应用后续独立schema以调用当前Go。两项当前升级回归合跑6.361s/exit0；不修改冻结51/52 SQL。
- `go vet ./internal/payment ./db` exit0；新55历史索引后真实db重入/校验 `go test ./db -count=1` 5.209s/exit0。

根任务及PR agent做独立只读审查，发现的两项P2均已实际红绿修复；独立review回归已包含在106.505s全PF通过中。HTTP/RBAC/CSRF/二次授权、队列/CLI恢复、双后台由根任务与console agent接入，console实际TLS HTTP含failed后新grant恢复专项已7.724s绿（由该agent报告）。最终全库/race/完整本地部署仍由根任务从固定集成快照验证，本专项结果不替代它们。

独立review最终报告：无未解决重要finding。PR12+PFReview4合计16functions及子例71.748s/exit0，payment vet/exit0；其中PF review4项累计24.39s。该agent已停止源码/测试修改。独立证据位于 `.tools/checkpoints/53-audit-55-review-evidence-82532f24-d1b5-4bbf-8aa0-374ab3e2ff91/windows-pr-review-tests.log`。SQL只验证结构/账本，不声称能验证HMAC；原资金动用的Go路径负责完整keyed证明验证。

## 完整候选全库发现的后台测试语义修订

根任务固定55候选在Windows和Linux race均复现 `TestProfitRulesBudgetAndOrderAllocation`：52将“原单只余400但新规则申请600”归为 `payment.ErrInsufficient`，旧测试仍期待 `ErrInvalid`；现有HTTP handler本已将该错误映射409。源额度与余额没有业务回归，不修改任何payment源码或迁移。

专项独立真实红4.115s；仅修订 `internal/backoffice/service_test.go` 后绿5.247s。规则配置总比例非法仍保留原ErrInvalid断言；执行超原单额度改期待ErrInsufficient，并用另一原支付1000使钱包有1400，证明钱包有钱也不能扩大原单400预算。拒绝后保持owner1400/recipient600/reserved0、requests1/allocations1/journals4/entries8、两原单freezable分别400/1000以及账本0违规。整个backoffice包64.209s/exit0，vet backoffice/exit0。

最小源补丁仅上述一个测试文件（33行新增、2行删除）；完整固定候选和55冻结SQL未修改。根任务已将修订重新纳入663文件的55 verified候选，后续完整验证以该新候选为准。
