# 订单收入冻结33：本地验证记录

本增量不调用支付机构。所有验证均为本机55432 PostgreSQL独立随机schema和httptest控制台请求，没有生产、真实通道、真实密钥或资金。

实际红测：

- 新Store/API方法尚未实现；console未挂接时501。
- 仅核对商户贷方时，错标payment key的商户间转账被误认成原支付收入；新增真实PG断言先失败，再补clearing扣方核对后通过。
- 仅核对退款商户扣方时，错误贷给其它商户的分录被误认成成功退款；新增真实PG断言先失败，再核对reserve/resolve双边原归属后通过。
- T1组合在freeze后错误得到ledger100/gross100/available0；根据原Release status=1求gross证据、经root确认，只加active hold排除后得到100/0/100，unfreeze恢复190/100/90。

覆盖：

- merchant net90、原成功merchant refund20，只冻结70；不改paid、普通reserved或平台手续费。
- 12并发同号freeze、并发同holdID unfreeze各只入一组journal；同号返回原cycle，旧freeze/旧unfreeze不能影响新cycle。
- pending/unknown退款、未支付、余额已预占不足、原payment/refund journal证据不全拒绝；多次freeze与退款预占真实并发只能一方成功。
- held期间新refund拒绝，旧已成功refund相同请求仍可幂等读取；错误退款对手账户不能被当成原成功退款。
- freeze与unfreeze的审计插入故障均回滚资金、状态和journal；CheckLedger无无效journal或账户投影。
- 原R_ADMIN对应operator允许，merchant禁止，CSRF阻断；GET展示income_hold和两项backend action bool，商户bool均false。
- T1组合恢复gross规则，held期间真实ReserveTransfer仍能预占旧可结算100，失败返还后解冻恢复原190/100/90。
- root的兼容投影测试验证status3/query/list过滤与解冻恢复；数据库订单保持paid，未重写payment/receipt。

最终实际命令：

```powershell
$env:GOTMPDIR='E:\GOEPAY\.tools\go-tmp'
$env:GOCACHE='E:\GOEPAY\.tools\go-cache'
$env:GOEPAY_TEST_DATABASE_URL='<本机 PostgreSQL 测试 DSN>'
& D:\GoLang\sdk\go1.21.3\bin\go.exe test ./internal/payment ./internal/server ./tests/contract -run 'Test.*(OrderIncome|IncomeHold|FundsAvailability|Refund)' -count=1
& D:\GoLang\sdk\go1.21.3\bin\go.exe vet ./internal/payment ./internal/server ./tests/contract
```

实际全部退出0：payment35.029s、contract10.094s；server0.997s但过滤后无直接case，控制台HTTP由contract覆盖。vet无输出。

迁移33冻结SHA-256：`9103D682E54E3745734016AD53FC0AD1EB68FDAB4CE38BAA30454EE1B1199DB0`。旧冻结迁移未改。语义差异、权限依据、历史数据和冻结期间退款边界见[设计](../design/order-income-holds.md)。独立复审结论由后续实际审阅记录补充。
