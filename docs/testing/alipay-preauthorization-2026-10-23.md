# 预授权本地验证 2026-10-23

测试使用 `127.0.0.1:55432` 本地 PostgreSQL，每个 fixture 新建随机 schema；上游只使用 `httptest`，签名密钥在测试中临时生成。没有真实资金、生产请求或真实凭证。

已观察到的红测：构造器尚未启用 preauth 时拒绝能力；Store/Flow 尚不存在时编译失败；未知扣款经查询入账后 capture 仍 unknown；普通 close 能把授权订单错误标成 closed；丢失冻结通知后普通 query 无法恢复本地授权证据。后续检查以实际命令结果为准。

定向测试覆盖：冻结 SDK 签名与无支付流水；授权通知金额/应用/收款方/请求号/买家/操作类型拒绝；capture 支付回执与未知结果；授权查询/解冻/撤销的严格协议画像；真实 PG 下授权零账务、20 并发单 owner、身份不变、capture 核对任务、停写、unknown 不重发、统一 receipt 幂等入账、只读追回成功、close 拒绝和丢失通知只读恢复；旧路由 HMAC 与方法限制；真实 HTTP V2 创建→旧通知重复→HTTP 503 unknown→状态4→停写只读查询恢复账本闭环。

最终定向命令：

```powershell
$env:GOTMPDIR='E:\GOEPAY\.tools\go-tmp'
$env:GOCACHE='E:\GOEPAY\.tools\go-cache'
$env:GOEPAY_TEST_DATABASE_URL='<本地 PostgreSQL 测试 DSN>'
& D:\GoLang\sdk\go1.21.3\bin\go.exe test ./internal/payment ./internal/adapters/payment ./internal/server ./internal/channelconfig ./tests/contract -run 'TestPreauth|TestAlipayPreauth|TestPreauthorization' -count=1
& D:\GoLang\sdk\go1.21.3\bin\go.exe vet ./internal/payment ./internal/adapters/payment ./internal/server ./internal/channelconfig ./tests/contract
```

`channelconfig` 的该名称过滤可能无匹配测试，不能据此声称工厂已全覆盖；工厂/guard 的综合测试由协议增量另行验证。接口边界见 [预授权设计](../design/alipay-preauthorization.md)。解冻/撤销仅适配器画像已测，完整持久化业务入口尚未实现。

本次最终执行结果：上述定向测试退出 0，`internal/payment` 6.108s、`internal/adapters/payment` 0.926s、`internal/server` 0.761s、`tests/contract` 4.369s；`channelconfig` 0.581s 但无匹配 case。上述五包 `go vet` 退出 0、无输出。额外真实签名/PG 查询 case 覆盖认证的 `40004:ACQ.TRADE_NOT_EXIST` 在已有可信授权时返回 AUTHORIZED，而坏签名与其他上游拒绝继续失败；授权查询不可用时，独立认证的真实支付成功仍可入账。此记录只宣称该增量范围，不能替代全项目验收。
