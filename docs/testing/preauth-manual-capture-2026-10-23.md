# 人工预授权扣取：本地验证（2026-10-23）

所有数据使用本机55432随机PG schema。HTTP使用真实httptest TLS服务、临时RSA密钥；测试transport先验证官方HTTPS host，再连接本地受信TLS证书。未访问生产、真实通道、真实code/secret或真实资金。

实际红测先记录缺少CapturePreauthorization接口；HTTP已实现前原console返回501，接新hook后通过。TLS测试最初错误期待buyer_id请求字段，经核对原明文SDK/已实现adapter，改为断言auth_no、原稳定tradeNo、原金额及COMPLETE；没有改协议凑测试，buyer依原授权与支付响应核实。

覆盖：

- 12并发明确操作只有一次上游扣取、唯一receipt及125余额；已成功、unknown、failed均不可重新领取。
- 完整rest_amount/已知剩余额、正确FREEZE操作、原authNo/买家/金额与原请求号；其它notfound、网络错、验签错、已有WAIT交易不放行。
- release在前置查询后抢占、实际capture/release并发时最多一次资金写；领取后权限失效为failed且零发送，之后不重启。
- 控制台归属、CSRF、停写、当前渠道白名单；前置授权查询期间撤销白名单阻断；GET capability纯本地零provider调用，unknown不再展示可扣取。
- actor intent审计写失败时零查询/零扣取；真实TLS capture发送时检查审计已存在；结果审计故障返回503，已经成功的付款与receipt不回退，也不能重复扣取。
- TLS响应丢失后只发送一次，普通只读Query恢复同一真实支付；重复Query不重复入账。
- 自动通知和人工capture共用发送后校验/settlement；原自动失败ACK、captureunknown恢复、释放互斥及voucher/app预授权相关测试一同回归。

最终实际命令：

```powershell
$env:GOTMPDIR='E:\GOEPAY\.tools\go-tmp'
$env:GOCACHE='E:\GOEPAY\.tools\go-cache'
$env:GOEPAY_TEST_DATABASE_URL='<本机 PostgreSQL 测试 DSN>'
& D:\GoLang\sdk\go1.21.3\bin\go.exe test ./internal/payment ./internal/server ./internal/adapters/payment ./tests/contract -run 'Test.*(Preauth|Authorization|Capture)' -count=1
& D:\GoLang\sdk\go1.21.3\bin\go.exe vet ./internal/payment ./internal/server ./internal/adapters/payment ./tests/contract
```

均退出0：payment33.656s、server0.641s、adapter1.351s、contract11.920s；vet无输出。独立只读复审未发现P1/P2，并独立重跑同四包全PASS（payment33.603s、server0.570s、adapter1.597s、contract11.789s）。

未新增迁移；23/25/27/30 SHA分别仍为 `FE225386737C62E1B0CCFB7C9A6580CA4ADD217D76D77013B290DCF2A42D168B`、`569F47D6E955F71A9EF4D6CB5B8B9F9AC2F3F9FC99F28AA08E67E1F094DD5DEA`、`EDEBB84813854EB1271CE88E2E6F8E39F4C1EE95E72CD852DEBCD5C3F3E0FD02`、`396CF5C68D64A7DCE1B86F02956FF2B09263BF70E1524CFD7B1A6DD72ECCC06F`。客户端行为见[人工扣取契约](../design/alipay-manual-capture.md)。
