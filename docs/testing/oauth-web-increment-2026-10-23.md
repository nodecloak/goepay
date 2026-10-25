# 网页 OAuth 本地验证（2026-10-23）

所有兑换与支付请求使用临时密钥和真实 `httptest.NewTLSServer`。测试 transport 保留生产请求的固定官方 HTTPS 目标检查，只在测试中将连接导向本地、验证受信任的临时 TLS 证书。没有生产 code/token/secret、真实通道或真实资金。PG 使用本机 55432 与随机独立 schema。

实际红测包括：Store/API 未实现；原 JSAPI 路由缺少显式 OAuth 分支；QR 子路径错误签发新 state；事务开始时未到期但等待订单锁后到期仍被 `now()` 放行。后两项均先复现运行时失败，再修复并通过受控回归。

覆盖：

- 订单、商户、通道、provider、AppID、method、用途、浏览器绑定与五分钟过期；跨租户/跨 state 的 code 重放拒绝；16 并发消费只有一个成功。
- 受控真实 PG 订单锁等待跨过 state 到期后拒绝；禁用商户后回调消费失败不能在重新启用后复活；已有 attempt 不签新 state、不更换 payer。
- 微信 snsapi_base 真 TLS code 兑换→可信 OpenID→JSAPI 材料，全程商户余额零。
- 支付宝 auth_base 真 TLS、顶层 code/grant_type 签名、成功节点可无 code=10000、精确签名、证书 SN/链/有效期复用；OAuth 不使用可配置支付 gateway 来改变固定 token endpoint。
- Secure/HttpOnly/Lax/__Host cookie，多标签复用；QR/return 不签新 state；错误 cookie/通道、重复 code、其它 tab 的旧 state 均不重新兑换。
- stop/白名单/AppID/商户状态改变及兑换过程中停写；恢复配置不能重用已消费授权；原 IP、实际连接 IP、真实买家黑名单各自阻断提交。
- 严格 oauth 参数、禁止网页授权套入 applet，原签名买家路径继续可用；OAuth 表不含明文 code/state/cookie/token。
- 显式加密文件+全局/通道双 opt-in，false 清空、非法布尔值拒绝、缺文件不能自动发现、simulation 不读取配置的 OAuth 文件。

最终定向与静态检查：

```powershell
$env:GOTMPDIR='E:\GOEPAY\.tools\go-tmp'
$env:GOCACHE='E:\GOEPAY\.tools\go-cache'
$env:GOEPAY_TEST_DATABASE_URL='<本机 PostgreSQL 测试 DSN>'
& D:\GoLang\sdk\go1.21.3\bin\go.exe test ./internal/payment ./internal/adapters/payment ./internal/server ./internal/channelconfig ./tests/contract ./cmd/api -run 'Test.*OAuth|TestConfigured|TestCommonProviderProducts|TestConcurrentProviderIntent' -count=1
& D:\GoLang\sdk\go1.21.3\bin\go.exe vet ./internal/payment ./internal/adapters/payment ./internal/server ./internal/channelconfig ./tests/contract ./cmd/api
```

最终实际均退出 0：payment 5.715s、adapter 1.118s、channelconfig 0.692s、contract 4.981s、api 0.752s；server 包 0.509s 但过滤后无直接 case，HTTP 行为由 contract 真 TLS/PG 测试覆盖。vet 无输出。独立审阅在修复锁等待过期后复跑六包 Scan/OAuth 通过，无其余 P1/P2。迁移 27 SHA-256：`EDEBB84813854EB1271CE88E2E6F8E39F4C1EE95E72CD852DEBCD5C3F3E0FD02`。

边界见[网页 OAuth](../design/web-oauth.md)。此报告不证明真实设备/授权域名/产品权限，未覆盖小程序 code、跨 AppID、profile/手机号授权；这些不能算网页增量已完成。
