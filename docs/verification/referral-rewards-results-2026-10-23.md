# 邀请奖励、退款结果证据开发记录（57/59）

本记录是增量开发与专项证据，不替代下一固定候选全库、Linux race、部署和恢复。默认功能关闭；没有生产访问、真实凭据测试或真实资金交易。

## 57：原单费用预算与真实支付

- 56 关系独立于准入邀请码，旧商户不得补绑。57 每个新订单出生时固定正/负快照、原平台手续费、受益人、政策版本、组来源、整数 BPS 和退款撤销策略；旧订单没有条款不追补。
- `global → payer group → promoter group` 逐字段覆盖；nil 继承，0/false 是显式覆盖。只开放 immediate/fee，拒绝未证实的 daily/profit/member-reward；任何成功退款撤销整笔仍有效奖励是 Go 明示政策，不冒充 Pro 隐藏实现。
- 奖励对原手续费作 half-up 舍入并受其上限约束，不复用净额留存舍入公式。1/3 分的 50%、0/10000 BPS、int64 最大值和非法输入：先 undefined 红，函数实现后 1.498s 绿。
- `CreateRechargeOrder` 将订单、充值意图、负快照放进同一出生事务；不按名称/订单号猜目的。普通/充值不能相互重放，快照故障整事务回滚：接口缺失红 → 真实 PG 4.167s 绿。旧充值夹具改用真实入口。
- 正奖励与净额/费用/原支付回执/Inbox/Outbox 同事务。12 并发回调一笔奖励，修改政策/停用推广人不改旧债；组继承/0覆盖/CAS/operator拒绝/资金 guard：首红发现政策 head 的 UPSERT 先触发 INSERT guard，改为明确 CAS INSERT/UPDATE 后 8.197s 绿，不放松 SQL guard。
- 所有 SQL 约束开启时造出原费用、余额、分录均平衡但 HMAC 为零的奖励，公读/付款重放/退款拒绝且 checker reward=1：4.500s 绿。
- 数据库已有 receipt 投影不能替代本次验签：缓存 receipt 后直接调用 sandbox `MarkPaid` 曾被追认为新奖励，真实红 3.959s；现在第一次正奖励要求本次 `PaidReceipt`，与正常回调合跑 7.402s 绿。旧 paid 重放只验原证明。
- 真实本地微信 TLS 下单 + RSA/AES-GCM HTTP 回调，8 并发通知只有一笔奖励；退款授权明确 R=5/K=3 分、未知/恢复后指纹变化、14 项账本核对全0：6.089s 绿。首轮合成报文漏 payer 被 400 拒绝，补足真实协议字段后通过，未放松验签。
- 邀请 overview/分页读在同一只读快照中验证关系、奖励及回收历史：累计已发、已撤销、回收中、今日/昨日发放分开；不称可提现余额。owner/分页/状态恢复及账本：5.752s 绿。
- 57 独立审查：四项额外真实 PG 回归 13.245s 通过，详见独立审查文档。

## 58/59：未知托管、可信结果与旧缓存

58 的独占回收账户、U/K 预算、保留余奖、失败恢复/成功撤销及 FK 锁红绿另见 58 专项与独立审查。59 修复同号重放误信旧 `provider_operations.state` 的问题：

1. 独审仅改 unsigned op state=failed，原 unknown 回收托管被重放释放并封成真 HMAC，4.851s/4.877s 红。全部退款 owner=false 重放统一只读并要求原 Query，不只特判邀请退款；4.510s 绿。
2. 扩展独审原 RSA 查询 SUCCESS 被旧迁移5 terminal guard 阻止，12.583s 红。新增59有限结果证据，保留已部署迁移 checksum；只有本次可信非终态→终态与同事务 audit/原分录/原号/HMAC 支持的缓存纠正才允许，未加 GUC 或关闭生产 guard。
3. 新结果证据最初“有则验证”会让缺行被冒充历史，独审 4.189s 红。59 明确存量退款 version1，新出生 version2 不可降级；新终态必须 provider 或 explicit internal 证明，SQL 延迟约束和父表缺行 checker 双重检查。内部 `ResolveRefund` 仍受信且同事务封 internal 证明，不能为已经未证实的终态补 seal。
4. 两种伪 cache 终态保持托管，原可信查询恢复且 POST1/QUERY1；可信失败重放不外呼；缺行 SQL 拒绝后原 unknown/40/60 回滚；broken restore 缺行、公读/internal补seal/重放均拒且 checker1；完整 SQL 形状但零 HMAC 的 provider/internal 证明均拒。最终独立组合 35.040s + vet exit0。

新增 CLI `invalid_referral_terms/rewards/clawbacks` 与 `invalid_provider_refund_results`，共14字段；恢复脚本只作结构检查，Store/CLI 分页验证 HMAC。没有把 SQL 约束说成验签，也没有把本地账本检查说成银行对账。
