# 协议与支付链路增量验证（2026-10-23）

工作目录：E:\GOEPAY。仅测试本地模拟通道、临时密钥与 Docker PostgreSQL，没有真实支付、退款或出款。

## 已验证链路

- V1 submit/mapi、V2 create 根据显式商户通道白名单选择微信/支付宝；未知通道/未授权商户拒绝。
- 支付材料存储前有持久化通道尝试，重试同一订单不重复请求上游。checkout/return/二维码使用用途绑定、带到期时间的 HMAC token；缺失/篡改 token 拒绝，二维码输出 PNG。
- 微信 XML SUCCESS/OK 与支付宝 success ACK 仅在回调验签、商户/订单/金额校验和账务提交后返回。注入账本提交失败时不 ACK；重复回调只入账一次。
- 同步返回忽略外部自报成功和 return_url，先查可信状态，未支付不跳转/不入账；已验证支付后使用数据库保存的商户 URL，经 notification.URL 构造 MD5/RSA 返回参数。V1/V2 回归通过。
- 支付查询可通过可信上游查证补录支付；关闭后晚到的真实支付成功，使用相同账务事务补录并写不可变 reconciliation 审计，避免丢弃实际资金责任。
- 退款外部编号使用 RF + 平台退款记录 ID，避免不同商户在共享上游商户号下复用同一 out_refund_no。上游退款查询还绑定原平台订单号和订单总额；不能仅依赖金额/客户端退款号。
- 20 个并发同退款请求仅外呼一次。未知结果保留预占；查询成功后冻结款结转。退款结果、操作状态和账本在同一事务中提交；注入退款状态更新失败时全部回滚。
- 未支付/余额不足退款返回失败且没有退款记录；实际外呼结果未知但已持久化预占返回已受理及平台退款号，供后续查证，不能当成退款最终成功。
- 微信/支付宝官方协议使用 httptest 上游和临时密钥验证。支付宝 Native precreate 的 notify_url 顶层字段有独立失败再修复回归。
- 商户 info/orders 与 legacy act=query/ orders 的字段、分页、隔离和 V2 数组排除签名按本地文档建立测试；Release/Pro 已知字段与默认分页差异按 profile 处理，未取得黑盒行为的细节仍是待验证项。
- transfer balance/query 只报告现有本地余额/记录；无配置的 transfer submit/proof 明确失败，不模拟真实出款成功。
- 商户 profile、只显示掩码的 API 信息、当前密码验证的密钥轮换，以及双后台修改密码后撤销旧会话有 PostgreSQL 回归。
- 已验证支付回调中的买家标识与账务同事务落库；账务回滚时买家资料也回滚，重复通知不会覆盖不同买家。
- 同一微信/支付宝插件内增加零钱转账、公钥模式支付宝账号转账与结果查询。模拟上游验证正确请求字段、签名、金额/平台编号绑定；真实 TLS 测试服务器要求客户端证书，无证书不能进入业务处理。银行卡、证书模式未配置的能力明确拒绝。
- 支付宝回单申请/查询及通道账户余额/额度有独立签名协议测试，回单下载仅接受 HTTPS。通道余额不能替代商户账本余额。
- 核对任务在请求前持久化，独立 worker 只查询可信结果，不重复发起支付/退款/转账。已关闭订单的晚到成功、未知资金保留、重复任务、缺少操作记录和不同通道任务过滤有 PostgreSQL 回归。

## 默认关闭真实写操作

部署测试环境继续使用 simulation，不读取任何真实通道凭据。另提供 configured 启动路径供后续受控部署：必须显式给定加密配置文件、HTTPS 公共 origin、商户通道 JSON 白名单，且不能与 Sandbox 混用。没有显式启用写开关时，ProviderFlow 和客户端工厂分别拦截 Create/Close/Refund/Transfer/回单申请。配置校验测试只使用临时生成的加密测试配置，没有访问官方网关。

独立 worker 默认只领取商户通知。必须同时设置 `GOEPAY_PROVIDER_MODE=configured`、`GOEPAY_ENABLE_PROVIDER_QUERIES=true`、显式加密配置文件和匹配的主密钥，才会领取所配置通道的 payment/refund/transfer.reconcile；始终 `Build(false)`，即使环境中存在 API 写开关也不能发起外部资金写请求。失败原因使用固定诊断，不保存上游原始敏感错误。查证达到重试上限后仍保留未知状态及资金预占，管理员重试仍只做查询。

当前范围只覆盖微信与支付宝两个支付插件。其他支付插件/外部服务适配均延后，不作为已完成能力统计。

## 本地 PostgreSQL 基准

AMD Ryzen 9 5950X、Windows Go 工具链、本地 Docker PostgreSQL、独立随机 schema、单客户端顺序操作，benchtime=2s：

| 项目 | 迭代 | 每次耗时 | 观察吞吐 |
|---|---:|---:|---:|
| CreateOrder | 842 | 2,846,212 ns | 约 351 次/秒 |
| Order 查询 | 3444 | 778,106 ns | 约 1285 次/秒 |

```powershell
& D:/GoLang/sdk/go1.21.3/bin/go.exe test ./internal/payment -run '^$' -bench BenchmarkPGOrder -benchtime=2s -count=1
```

这不是最大吞吐或真实支付压测，也没有 PHP 同等硬件基线，不能据此宣称已比原版提升某个百分比。

离线迁移演练与运行方式见 `docs/migration/RUNBOOK.md`。
