# 订单收入冻结与解冻（迁移33）

本功能冻结已支付订单在平台账本内归属商户的剩余净收入，与支付宝预授权、退款及T+1不同。冻结、解冻均不调用支付机构，不使用provider写开关，不改变orders.status=paid、支付流水或receipt。外部兼容查询/列表的status=3来自独立active hold投影。

## 明文依据与差异

`epay_release/includes/lib/Order.php::freeze/unfreeze` 从已支付订单读取getmoney、拒绝mode=1直清，然后减/加该净额并将旧status切换3/1。`includes/lib/api/Pay.php` 计算getmoney，`includes/functions.php::processOrder` 用getmoney入商户余额，故不能改用gross订单金额冻结。Pro明文`app/lib/Order.php`和`route/admin.php`同样有freeze/unfreeze；后台controller加密，未推断其内容。`epay_pro/app/data/menu_admin.json` 的OrderList角色含R_SUPER/R_ADMIN，原编译order客户端也有freeze/unfreeze接口，因此Go控制台super与operator可操作，商户不可操作。

Release原冻结实现没有现代并发幂等与账本隔离，而且冻结中退款会令reducemoney=0。本增量**暂不支持冻结期间退款**，必须先解冻；不能称为原冻结退款语义的完全兼容。后续需要单独实现冻结资金向真实退款的安全分配。已完成的部分退款则按实际原归属扣除，不再把原getmoney全额重复冻结。

原直清模式不会形成平台向该订单商户的净收入分录。Go没有独立直清结算业务，本功能只接受有原始`payment:{trade}` clearing→该商户available的精确净额证据；不能根据渠道名称/当前配置臆造历史mode。只导入期初余额、订单status或无法逐单证明原净入账的历史订单均拒绝，不能拿商户当前余额替代证据。充值intent也拒绝。

## 金额与账本

冻结金额等于订单不可变merchant_amount减去该订单成功退款的不可变merchant_amount之和。成功退款还须匹配原refund reserve的available→reserved两方和resolve的reserved→clearing两方分录；仅status=succeeded或仅商户扣方不足以证明退款归属。原payment journal也必须同时有平台clearing扣方和商户available贷方，错误标记为payment的商户间转账不能通过。平台手续费不重新计算、不转入hold；gross、net、refund fee payer历史快照原样保留。

金额必须为正，未支付、全部已退完商户收入、pending/unknown退款、证据不齐、已有active hold均不能创建新冻结。若商户净退款实际已超过原net（例如商户承担手续费），不存在正的该订单剩余收入，拒绝冻结，不制造负数或补贴。现有提现/转账/退款等预占导致available不足时明确拒绝，不借用reserved或允许负余额。

冻结以统一post写`available → order_income_hold`；解冻反向写回同一商户。order_income_hold是独立账户kind，不与普通reserved混用。每个hold有固定金额和快照，不可删除/改挂订单，状态只允许held→released。每个订单最多一条active hold。完整操作、两方journal、hold状态、不可变actor event和控制台审计在同一PG事务提交；任何审计失败全部回滚，没有“资金已移动但审计稍后补”的阶段。

原订单行锁序列化freeze/unfreeze/退款预占与退款结转。ReserveRefund只在旧请求幂等查询之后检查active hold，旧已完成/失败退款重放不受影响，新退款拒绝。数据库trigger同时锁原订单并兜底阻止held期间的新退款/状态更新。冻结前已存在pending/unknown退款会阻止freeze，因此不会把其reserved款吞入本账户。

## API与循环幂等

共享控制台登录、管理员类别、CSRF后：

- POST `/console/v1/admin/orders/{trade}/freeze-income`，JSON `{"external_no":"<稳定操作请求号>"}`。
- POST `/console/v1/admin/orders/{trade}/unfreeze-income`，JSON `{"hold_id":"<原hold ID>"}`。

成功200/code0返回hold，字段有id、trade_no、external_no、merchant_net、refund_merchant_amount、amount、state、created_at、released_at，金额与ID均字符串。freeze请求号按商户唯一；重复同号返回原hold，拿同号改订单冲突。一次freeze网络重试必须保留原external_no；已released的旧请求不能重启冻结。新一轮明确freeze用新external_no；unfreeze总引用那一轮hold ID，旧unfreeze重放不能释放后来创建的hold。无任何客户端自选金额。

GET订单详情返回income_hold（最新cycle）、income_freeze_amount及can_freeze_income/can_unfreeze_income。helper只读本地账本；商户只能查看，操作bool均false。HTTP失败后重新读取原订单/原请求，不能靠生成新external_no绕过结果不确定。保留原请求号使PG提交结果丢失也可安全重放。

## T+1组合

原Release `includes/lib/api/Transfer.php:128-133`、`includes/lib/Transfer.php:99-104/315-320`、`user/apply.php:37-42`、`user/transfer_add.php:70-75` 都只对status=1求SUM(realmoney)。旧冻结会变status3，因此收入已独立held时应排除对应今日gross，防止重复限制其它旧资金。

Go保留原gross、上海日界、充值排除和已有退款政策，仅给FundsAvailability的gross CTE加NOT EXISTS(active income hold)。例：旧余额100+今日gross100/net90，原T1账本190、可用90；freeze90后账本available100、hold90、今日gross排除该单为0、可用100；unfreeze后恢复账本190/gross100/可用90。没有用net替换T1的gross政策。

此增量无生产资金或真实通道验证。原冻结中退款、历史只含期初余额订单、直清业务和其它不能逐单证明净额归属的画像均没有伪造兼容。
