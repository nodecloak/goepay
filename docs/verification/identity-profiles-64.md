# 独立OAuth应用与用户标识：64验收记录

范围为设计A：可信独立OAuth档案、微信/支付宝直连网页与mini标识查询、原会话一次领取、SDK控制台与OpenAPI。没有实现AppAuthToken、服务商/子商户、电商、URL Link/Scheme生成；没有访问生产、读取实际凭据或发起实际授权/资金交易。所有外呼测试均通过合成凭据和本地httptest TLS回送传输完成。

## 固定源码与范围

2026-10-24 根任务复核补充：下面25文件清单是作者当时的检查点，已被后述商户配置版本修复取代，不能用它证明当前完整候选。当前完整固定候选为 `.tools/checkpoints/64-candidate-bdee7a9f-4604-40ab-8be8-dbe88bddd76f`，853源码文件；全库 Windows/Linux race 正在执行，尚未宣称通过或部署。

25个本代理源文件及设计文档的SHA256清单：`.tools/verification/identity-profile-64/source-sha256.json`，清单SHA256为`0e0272721596b1cafe74b8564e620d051db88a97cb49aa25618ed3c5ae08ca03`。清单包含迁移64、typed config/factory/import、独立核心与历史code realm、旧47迁移测试的当前schema升级、HTTP/helper、UI与测试；迁移1..63没有被本代理修改。

独审补充测试`internal/payment/identity_profile_review_test.go`由独立审查代理提供，其SHA256为`d483c692076155d219ed9db1534c0618044b00fb8e9b354c2db346191d351af6`。该测试也纳入本轮最终Go命令。独审捕获的原始源码和crypto RED记录在`.tools/64-identity-review-path.txt`指向的检查点；独审代理后续没有完成最终签字，根任务接续代码审阅与固定完整候选门禁，不把本记录声称为独立审查全部通过。

根任务拥有共享接线，验收时引用值如下，不列入本代理25文件清单：

| 文件 | SHA256 |
|---|---|
| `web/src/App.vue` | `8a7970046d8eb0f994b81f482643fc294304fc4db8958ee101fc6d9a72a52874` |
| `web/nginx.conf` | `92beae17ec12042fbcfac3f399d937a3b8ea0be076d16d62ccbb60bbae0aadb9` |
| `internal/server/server.go` | `3d954ab2b632f61b70441e98bc5635dc7afd23920b8facff4f2146a873a10172` |
| `cmd/api/configured.go` | `9e1578e76b685ee5de1ae8de90ad1455a498bd89023fa91e5224090e5fef1168` |
| `docs/tools/build-openapi.mjs` | `b85d5699d969b2e3da7c4e87e8ed2b815d353d366f58eb90572ba44ab872c39c` |

Nginx现有`/identity/`日志路径脱敏映射覆盖新路由；只记录无query的脱敏路径。独立mini的state和code只在JSON body，不进入URL；所有独立响应no-store/no-referrer。

## 真实RED与修复

| RED证据（同目录原始log） | 观察与修复 |
|---|---|
| `realm-red.log` | 支付mini曾可兑换独立网页已使用的支付宝code。统一支付宝实际exchange realm并兼容历史47claim；微信实际两个realm保持分开。 |
| `account-mode-red.log` | 已识别的子商户字段曾被静默解释成直连。付款工厂及独立profile显式拒绝尚未实现角色。 |
| `deadline-red.log` | 审计等待后状态已过期却可授予兑换机会。claim与审计后再验DB时钟及原durable会话。 |
| `session-cap-red.log` | 旧47创建曾可绕过64的五活动请求上限。相同session advisory lock下计数两类请求。 |
| `cipher-red.log` | 同session且相同短expiry的两个mini请求可复制cipher领取。来源HMAC加入精确请求ID，AAD随之隔离；PG绕过触发器复制密文仍拒绝，原q仍可领取。 |
| `null-json-red.log` | 可选merchant_id的JSON null被string Decode当空值接受。先读取RawMessage明确拒绝null。 |
| `nested-config-red.log` | 嵌套凭据重复字段可后项覆盖。typed导入拒绝嵌套重复/null/非string值。 |
| `channel-binding-red.log` | 仅关联通道付款凭据轮换未改变原profile绑定。完整可信Channel和完整Profile共同形成内部指纹，不回显凭据。 |
| `oauth-regression.log` | 历史47迁移测试只升到47后使用当前API，缺64共享cap表。测试先保留47 backfill断言，再升48..64，补历史mini→支付宝web真实realm断言；没有让核心退回忽略64表。 |
| `frontend-browser.log` | 全浏览器首轮95通过/9跳过/1失败：63退款测试status定位器同时匹配列表loading。相关owner仅限定原提示locator，保留原退款/金额断言；原失败log保留。 |

初始未实现类型/方法的编译RED分别保留在`config-red.log/profile-red.log/http-red.log`；不把这些编译失败当成功业务验收。

## 已运行结果

专用随机schema PostgreSQL端口55436；使用标准本机Go1.21.3与`-p1`，不与根任务PG实例共享。HTTP fixture通过正常`db.Migrate`加载全量迁移并走真实Handler。最终37个顶层匹配测试，无skip/fail：

```powershell
go test -p 1 ./internal/channelconfig ./internal/payment ./internal/server ./tests/contract `
  -run 'TestIdentity|TestMiniOAuth|TestOAuth|TestMiniLaunch|TestReviewIdentity' -count=1 -v
go vet -p 1 ./internal/channelconfig ./internal/payment ./internal/server ./tests/contract
```

| 验证 | 实际结果 | 原始记录 |
|---|---|---|
| 最终OAuth/身份配置匹配测试 | channelconfig 1.078s；payment 71.232s；server 1.306s；contract 38.107s，全绿 | `oauth-regression-final.log` |
| 完整channelconfig包 | 1.724s绿；其后完整channel绑定再定向0.915s绿 | `config-final.log`、`channel-binding-migration-green.log` |
| 完整server包 | 1.273s绿 | `server-regression.log` |
| 精确请求AAD与原64核心 | 16.824s绿，含独审crypto及跨q密文复制拒绝 | `cipher-green.log` |
| 当前profile/session、web browser、伪HMAC、实际realm历史与并发 | 11.242s绿 | `boundary-green.log` |
| 真实Alipay同App原物理Seller/有效凭据变化使旧q失效 | 4.394s绿；旧配置恢复不能再领取 | `physical-account-green.log` |
| 四条本地TLS web/mini完整查询及交换期间会话撤销 | 18.470s绿 | `http-strengthened.log` |
| 静态检查 | exit0 | `go-vet.log` |

每条完整HTTP合同均验证订单、provider付款尝试、账本与任务零新增；capabilities/create/status/reveal无兑换外呼，兑换只有一次，重放拒绝。交换期间撤销原会话后为failed、无cipher、无法领取、不能再兑换。与原支付web/mini的回归同时验证原App身份、浏览器和付款HMAC状态，未更改真实付款开关或资金授权。

前端最终固定源验证运行`npm test`、`npm run build`、`npx playwright test`、`npm audit --json`；119单元通过、typecheck及生产构建通过、96浏览器通过（25.9s）、9 live测试按环境要求跳过，audit总漏洞0。前后25文件SHA全部一致。原始记录以`frontend-unit-final.log`、`frontend-build-final.log`、`frontend-audit-final.json`、`frontend-browser-fixed-source.log`、`frontend-final-consistency.log`为准。本代理源码于此冻结；live测试留给主任务本地模拟部署门禁，不允许实际资金调用。

本记录不替代根任务固定63/64完整候选的Windows/Linux全库门禁或部署授权；本轮未部署。

## 根任务复核：商户停用再启用不能恢复旧授权

真实持久超级管理员会话保持原auth_version，目标商户通过正常 `ReplaceMerchantAtomic` 停用再启用，使配置版本增加2。新增测试实际失败4.376s：旧pending可兑换、verified可领取、processing可完成、未启动网页链接可启动、原请求仍可读。保留 `merchant-revision-red.log`，没有用直接破坏数据库来代替正常业务复现。

修复在尚未部署的迁移64增加不可变 `merchant_revision`，创建时在原会话持有商户共享锁后读取，纳入来源HMAC，并在每个生命周期操作及审计等待后与当前商户版本比较。没有修改已部署的1..63迁移或旧47会话模型。旧请求即使未观察到停用过程，也不能在重新启用后恢复；新请求可正常兑换。

原复现修复后通过4.097s；扩展测试同时验证SQL禁止改版本，以及损坏恢复绕过SQL guard把旧请求改成当前版本后仍被来源HMAC拒绝。随后四包身份/OAuth专项实际通过：channelconfig 0.708s、payment 70.318s、server 0.993s、contract 41.873s，包含本地TLS四种web/mini流程。证据为同目录 `merchant-revision-green.log`、`merchant-revision-regression.log` 与 `root-merchant-revision-source.json`。这批结果不替代当前853文件的完整候选回归。
