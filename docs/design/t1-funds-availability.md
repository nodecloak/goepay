# T+1 可出账额度

此增量只使用本地源码、PostgreSQL 和假上游验证，没有生产操作或真实资金测试。

## 源码依据与映射

- `epay_release/includes/lib/Transfer.php:99–110`：全局 `settle_type=1` 时，持有商户行锁，扣除当天支付成功且 `tid<>2` 的 `realmoney` 毛额，负数归零；比较的是代付本金加服务费。
- `epay_release/includes/lib/api/Transfer.php:128–139`：余额接口返回相同 `available_money`。
- `epay_release/includes/functions.php:640–643`：`tid=2` 明确为余额充值；不是支付通道数字 ID。Go 使用 `recharge_intents.order_id` 排除充值，使用 `orders.amount` 对应支付毛额，`merchant_amount` 对应净入账。
- `epay_release/user/transfer_add.php:70–75`、`transfer_red.php:60–67`：页面展示也执行同一代付额度扣减；后者说明前日收入及充值可用。
- `epay_release/user/apply.php:37–44`：手动结算同样扣当天毛额，但此旧页面 SQL 没有充值排除项。Go 按本次明确设计统一四类资金出账的充值例外，避免接口与审批页额度不一致，不能据此声称旧页面全部细节完全相同。

全局设置 `settings.settle_type` 仅接受字符串 `"0"` / `"1"`，缺省为0。商户资料中的同名 `profile.settle_type` 是收款账号类别，不参与本策略。无法解析的已保存全局设置阻止新的出账，不能默认为0。

## 计算与账务边界

北京时间当天零点固定为 UTC+8，不使用连接或宿主机时区。T+1 的受限毛额为当日 `paid_at` 起、状态仍为 paid 的订单金额合计，排除充值意图及成功退款累计已达到订单毛额的订单。部分退款、待处理或未知退款仍限制原订单全部毛额。

```
账面可用 = accounts.available.balance
当日受限 = min(账面可用, 当日受限订单毛额)
可出账   = 账面可用 - 当日受限
```

默认 T+0 的受限金额为0。按毛额扣减会比净入账更保守，例如旧资金10元、今日毛额1元净入账0.9元，可出账为9.9元。充值净入账即时可用。SQL 用 numeric 求和并在转换 bigint 前限幅，不发生金额溢出。

外部转账检查本金加平台服务费；结算申请、站内转账和站内分账检查申请金额。它们在相同商户锁内去重，按账户 ID 顺序取得该商户 available/reserved 锁，再以新语句的同一快照读取余额、订单和退款并预占。不会锁订单行，避免与支付/退款的订单→账户锁序形成环。等待账户锁结束后才读聚合，避免旧聚合快照配上新余额。

已持久化的请求重复提交返回原意图，不因次日或配置变化再次预占。审批只处理先前已冻结资金。退款继续按原退款账务执行，不受可出账额度限制。所有余额变化仍使用平衡且不可变的凭证，没有新余额表或直接改余额路径。今日收入不能先经站内转账或分账转给另一商户解除限制。

支付成功时间在凭证入账并取得账户锁后，以最终更新语句的 `statement_timestamp()` 保存。预占计算也使用该时钟，避免23:59开始、跨零点等待账户锁的事务沿用昨日事务开始时间，提前成熟今日收入。

## 接口

- RSA `POST /api/transfer/balance`。
- `GET /console/v1/{merchant|admin}/transfers/capabilities`。
- `GET /console/v1/{merchant|admin}/workflows/funds-availability`，商户只能查看本租户，管理员必须提供 `merchant_id`。

三者均返回元字符串 `available_money`（可出账）、`ledger_available_money`（原账面available）、`today_blocked_money`（本次实际受限额），以及数值 `settle_type`（0/1）。它们是读取时的提示快照，提交时必须重新检查。普通商户信息接口原 `money` 和账本余额接口保留账面含义，不把其改成可提现金额。

迁移26仅增加 paid 订单与成功退款的聚合索引。现有结算审批依旧只冻结/审批本地账务，不代表完成外部银行卡出款；该限制没有因T+1计算而改变。
