# 授权解除增量验证（2026-10-23）

使用本机 PostgreSQL `127.0.0.1:55432`，各 fixture 随机 schema；上游仅 httptest，临时生成 RSA 密钥。没有生产访问、真实凭证或真实资金。

红测证据：尚未实现 Store/Flow 时缺少方法与状态字段；未接 console hook 时返回 501；原先 reversal_pending/sent=false 的显式重放只能查询，导致永远没有首次发送机会，真实 PG case 明确失败。实现后上述回归通过。

验证覆盖：

- 原授权零商户账务，解除成功只关闭未付订单，晚到可信支付仍统一入账。
- 解除意图阻止 capture，20 个并发 MarkSent 只产生一个发送机会，直接 SQL 也受触发器保护。
- 同一全局 UA 身份不可重建；已发送 unknown 不重发，只有原 UA 的明确 UNFREEZE、金额/授权号匹配且剩余额明确零才能完成。
- 缺少剩余额、部分剩余额、操作类型不符、未知 capture 只有 notfound、实际支付成功均不盲目解冻；capture 认证 CLOSED 后才允许后续显式动作竞争原 UA 的首次发送。
- 真实 httptest/PG console 链：CSRF 与归属拒绝；intent 审计失败后所有上游调用为零；停写拒绝；预校验 HTTP 503 保持 sent=false；显式重试同 UA 首次 unfreeze HTTP 503→unknown；重放不重发；停写下 Worker 只读 UA 核对；确认后关闭且商户余额仍零；操作者 intent/result 审计存在。
- Root 接入的查询/列表在 confirmed release 后不再报告 status 4；只读 Worker 使用新的专用 job kind。

最终定向命令：

```powershell
$env:GOTMPDIR='E:\GOEPAY\.tools\go-tmp'
$env:GOCACHE='E:\GOEPAY\.tools\go-cache'
$env:GOEPAY_TEST_DATABASE_URL='<本地 PostgreSQL 测试 DSN>'
& D:\GoLang\sdk\go1.21.3\bin\go.exe test ./internal/payment ./internal/adapters/payment ./internal/server ./tests/contract ./internal/reconciliation ./cmd/worker -run 'TestAuthorizationRelease|TestConsoleAuthorizationRelease|TestPreauth|TestAlipayPreauth|TestReconciliationWorker' -count=1
& D:\GoLang\sdk\go1.21.3\bin\go.exe vet ./internal/payment ./internal/adapters/payment ./internal/server ./tests/contract ./internal/reconciliation ./cmd/worker
```

实际退出均为 0。最终定向测试耗时：payment 15.690s、adapter 1.255s、server 0.600s、contract 6.021s、reconciliation 1.871s、worker 0.630s；vet 无输出。本记录只代表上述范围，未执行真实设备/产品权限联调，也未实现部分解冻或撤销业务入口。迁移 23 的冻结 SHA 未变化。迁移 25 SHA-256 为 `569F47D6E955F71A9EF4D6CB5B8B9F9AC2F3F9FC99F28AA08E67E1F094DD5DEA`。设计见[授权解除](../design/alipay-authorization-release.md)。
