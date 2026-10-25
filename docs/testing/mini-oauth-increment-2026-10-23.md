# 小程序身份绑定30：本地验证记录（2026-10-23）

本增量只在本地随机PG schema、临时签名密钥和真实 `httptest.NewTLSServer` 运行。测试transport先检查固定官方HTTPS目标，再将连接导向验证临时证书的本地TLS服务。没有真实code、token、通道、生产访问或资金。

实际红测：新增Store/adapter/config接口未实现；主HTTP尚未接completion时错误返回default200；新增真实PG安全断言证明仅单列state FK时可以把claim挂到其它AppID的mini state。最后一项修复为 `(mini_state_hash,provider,app_id)` 复合FK并重新PG通过。迁移30同时用复合FK将mini state固定到原付款intent。迁移27及其它已冻结SQL未改。

覆盖：

- 订单/租户/通道/provider/AppID/method/用途/path错误绑定拒绝；并发重复state仅一位消费成功；网页与mini双向重复code拒绝，共享code并发争抢仅一位成功。
- code claim不可更新改挂state、缺失state/其它AppID/同时web+mini双FK拒绝；数据库只有code/state HMAC，不保存原值。
- 真实PG锁等待跨过500ms短state到期后拒绝；禁用商户state被消费，恢复后不能重启；已有attempt不能签新state，先前另一个state也不能重新换身份。
- 微信真实TLS `jscode2session`，固定appid/secret/js_code/authorization_code，请求不走网页access_token API；严格JSON重复字段/尾随数据/缺session_key/异appid/平台错误拒绝，固定错误无secret。
- 支付宝mini复用准确的token节点与证书SN验签；未知SN、签名后篡改身份拒绝；临时key，未连接真实网关。
- 微信从V1受保护收银台HTML启动JSON读出原页面和completion state，再POST SDK code，真实可信OpenID只送JSAPI底层创建；支付宝同样真实TLS取得可信buyer并创建mini支付。
- completion拒绝重复JSON键、未知openid字段、不同/空body state、尾随JSON和超过8KiB body；响应不带code/token/openid/state。
- 停写/禁用商户/渠道白名单变化、兑换中途停写均不创建付款，恢复不复活state；原保存IP、真实连接IP、可信buyer黑名单分别阻止提交。
- 外部兑换响应失败后仍消费原state/code；新state重用相同code不会再外呼。
- 两家身份与付款创建后余额仍0；随后验签查单确认真实成功、重复查询与晚到重复通知后只入账125一次、每订单只有一条verified receipt。
- 仅mini配置在加密导入+全局OAuth opt-in下可离线构造；默认关闭、false清两map、无通道opt-in拒绝、simulation不读取给定的配置路径。付款写开关仍独立控制。

protocol伙伴的同增量材料测试覆盖：V1/V2入口、8个活跃state上限、辅助route不签state、无效配置创建订单前拒绝、启动0attempt/0receipt/0ledger/0上游IO，以及原PHP启动编码实际互操作。这里并未将不同code/不同state并行的身份HTTP说成整单仅一次；24保证最终付款attempt/payer只能有一个。

最终实际命令：

```powershell
$env:GOTMPDIR='E:\GOEPAY\.tools\go-tmp'
$env:GOCACHE='E:\GOEPAY\.tools\go-cache'
$env:GOEPAY_TEST_DATABASE_URL='<本机 PostgreSQL 测试 DSN>'
& D:\GoLang\sdk\go1.21.3\bin\go.exe test ./internal/payment ./internal/adapters/payment ./internal/server ./internal/channelconfig ./tests/contract ./cmd/api -run 'Test.*Mini|Test.*OAuth|TestConfigured|TestConcurrentProviderIntent' -count=1
& D:\GoLang\sdk\go1.21.3\bin\go.exe vet ./internal/payment ./internal/adapters/payment ./internal/server ./internal/channelconfig ./tests/contract ./cmd/api
```

均退出0：payment11.456s、adapter1.806s、server0.823s、channelconfig1.412s、contract11.380s、api0.829s；vet无输出。

迁移30 SHA-256：`396CF5C68D64A7DCE1B86F02956FF2B09263BF70E1524CFD7B1A6DD72ECCC06F`。迁移27仍为 `EDEBB84813854EB1271CE88E2E6F8E39F4C1EE95E72CD852DEBCD5C3F3E0FD02`。

客户端必须适配新的POST SDK-code契约；尚无真实设备、域名权限、真实资金或生产权限验证。详见[小程序状态与客户端契约](../design/mini-oauth.md)。
