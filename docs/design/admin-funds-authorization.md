# 管理员资金二次授权（迁移41）

状态：41身份、HTTP与原有金融路由事务钩子已经实现，专项本地PG/TLS测试见下文；42批次执行授权由独立持久execution intent约束。43钱包结算execute/cancel授权绑定也已有本地PG回归，但不能由本页推导全部钱包结算领域验收状态。没有进行真实通道或真实资金联调。

管理员各自持有独立 Argon2id 凭据，复用当前密码实现（64 MiB、2轮、并行2、随机盐），不创建默认密码、不导入旧版明文。设置与修改必须校验当前登录密码、已有资金密码以及当前已启用的 TOTP。新凭据12至1024字节，禁止与登录密码相同。身份只接受未委托的原始管理员持久session。登录密码、权限、账号启停引起 auth_version 变化；资金凭据变化另递增 credential_epoch。

显式全局设置 `admin_funds_authorization_enabled` 缺省为false以保留既有接口。启用后首次敏感资金意图必须有授权；未设密码、授权服务/数据库异常均拒绝。该设置不自行开放任何厂商写操作，现有通道写闸仍生效。

HTTP先预览具体动作，再提交资金密码确认。challenge最多2分钟，grant最多5分钟且不超过session期限；两者都绑定原session摘要、账号、auth_version、credential_epoch和服务端规范化动作SHA256。grant是32字节随机令牌，数据库仅保存其SHA256。只经响应及 `X-Payment-Authorization` 请求头传输，禁止URL/日志/本地持久存储。前后端都不接收调用方自称的可信action hash。

动作包含kind、资源标识以及规范字符串参数。服务端必须从真正业务元组重建：金额为整数分，ID为规范十进制，目标商户、通道、收款资料、原业务编号及批次项目范围全部纳入。不同动作或同业务编号换内容不能共用授权。preview/confirm与执行端必须使用同一构造器。

`ConsumeTx`由业务首次意图事务调用：在同一PostgreSQL事务中重新验证原账号/session、两个版本、截止时间、动作和grant，消费grant并追加不可变审计，与资金预留或唯一发送claim共同提交。外呼在commit后。不能仅靠HTTP中间件先消费授权。已有相同不可变意图重复请求应由业务先返回/只读查询原记录；unknown绝不因新授权重发。已commit的外部请求无法靠改密撤销；厂商回调、查询worker继续核实。

认证失败限流跨进程保存在PostgreSQL；账户和账户/IP各有5次失败/15分钟预算，以及300/150次总核验/15分钟上限，不采用共享nginxIP全局封锁。账号桶不以session、凭据版本分桶，不因成功或改密清零。20次连续正常确认不会触发失败预算。每进程最多4个密码核验；容量不足返回503，不无限排队。失败审计/限流写入失败则拒绝。数据库时钟在获得锁后重新读取，避免等待锁后使用过期的transaction timestamp。

凭据设置、challenge签发、密码验证成功/失败、grant消费均要求持久审计成功；审计不保存密码、令牌或原始收款资料。外部操作已发送后的结果审计故障不能伪称未发送，应留可恢复的持久意图与核实任务。

实施顺序：严格规范动作/密码边界测试；真实PG凭据与session绑定；challenge/grant并发消费、回滚与撤销；严格JSON HTTP与CSRF/no-store；主任务接资金业务事务；端到端出款未知与原商户SDK回归。迁移1至40不修改。

HTTP合同（所有路径以前缀`/console/v1/admin/security`开始）：

| 方法/路径 | 输入 | 输出data |
|---|---|---|
| GET `/payment-password` | 无 | configured、enforced、credential_epoch、totp_required |
| PUT `/payment-password` | current_password、current_payment_password、new_payment_password、code | 无密码回显；旧授权被版本失效 |
| POST `/funds-authorizations/challenges` | target完整管理员POST路径、body原业务对象 | challenge_id、expires_at、安全summary对象 |
| POST `/funds-authorizations/confirm` | challenge_id、payment_password | authorization、expires_at |

执行原业务POST时携带`X-Payment-Authorization`，原body及external_no保持不变。所有响应no-store，所有写请求验证CSRF。严格JSON拒绝根/嵌套重复键、未知外层字段、尾随内容、无效UTF8及过深结构；上限128KiB，覆盖100条最大长度常规批次。资金密码验证错误返回403及`FUNDS_AUTHENTICATION_FAILED`，不注销仍有效的登录session；共享登录session失效仍由既有中间件返回401。授权缺失/过期/内容不匹配为428 `PAYMENT_AUTH_REQUIRED`，不得伪装成已发送202。

当前保护矩阵（仅管理员POST；下列路径在`/console/v1/admin`之后）：

| 路径 | 事务guard kind |
|---|---|
| `/transfers` | transfer |
| `/transfer-batches` | batch-prepare |
| `/transfer-batches/:id/execute` | batch-execute |
| `/transfer-batches/:id/cancel-unsent` | batch-cancel |
| `/orders/:trade/refund`、`close` | refund、order-close |
| `/orders/:trade/capture-authorization`、`release-authorization` | authorization-capture、authorization-release |
| `/orders/:trade/freeze-income`、`unfreeze-income` | income-freeze、income-unfreeze |
| `/workflows/{settlements,internal-transfers,profitsharing}` | funds-request |
| `/workflows/{settlements,internal-transfers,profitsharing}/:id/{approve,reject}` | funds-review |
| `/workflows/memberships` | membership |
| `/workflows/deposits`、`/workflows/deposits/:id/{approve,reject}` | deposit、deposit-review |
| `/workflows/profitsharing-rules/:id/execute` | funds-request |
| `/settlement-payouts/:requestID/execute`、`cancel-unsent` | settlement-payout、settlement-payout-cancel |

预览只接受规范数字ID，但既有业务解析器接受的`01`/`+1`路径别名仍必须安装执行guard，开启政策后拒绝非规范路径，不能通过绕开精确allowlist省略资金授权。普通商户签名API、厂商callback、充值、proof、通知重发、sandbox-paid和纯查询不要求管理员资金密码。自动预授权通知的既有领域流程不被冒充为管理员手动动作。

权威`Server.ResolveFundsAction`在预览及真实业务事务内复用。规范body与相关DB快照分别通过`Store.FundsActionHMAC`域隔离签名，最终action只存摘要，避免对收款账号做离线字典猜测。快照包含原订单/资金申请/批次不可变元组、目标商户profile/version及白名单、实际通道应用标识与产品能力；全局只选明确金融settings键，用户组/规则选当前目标，risk与旧收款资料只选本次商户，不聚合全站无关资料。目标历史超过10000条明确拒绝，不能静默截断。动态余额和当天配额不进入指纹，但仍由业务事务重新判断。修改相关政策必须重新确认；修改无关组或其他商户资料不应使本次授权失效。

批次execute的确认绑定当前unsent前limit个明确ID、原收款密文及应用身份。42在首claim同事务消费grant并固定execution子集，之后每条只可在该子集claim一次，不能以内存“已授权”标记扩展发送范围。单笔和退款等已有意图先走完整幂等比较，再决定是否需首次guard。

43钱包结算确认绑定原request/TF、独立原gross资金来源、39gross/fee/net不可变快照、40收款密文及配置ID、原通道应用身份。确认摘要明确available balance再扣款为0，不能把net TF当作一次新的available支出。execute授权不能用于cancel；cancel授权只释放该请求原gross一次。prepare和query不要求资金密码；原claimed意图重放不会再次发送，也不强制再消耗grant。账户掩码通过同一事务reader读取及验证真实收款密文。

可复现专项（设置GOEPAY_TEST_DATABASE_URL为本地隔离PG；Windows缓存/TEMP均使用E盘.tools）：

```powershell
go test -count=1 ./internal/identity -run TestFunds
go test -count=1 ./internal/server ./tests/contract -run 'TestFunds|TestAdminFunds'
go test -count=1 ./internal/payment -run 'Test.*FundsAction|Test.*Batch.*Author'
```

已实跑证据包括：12并发grant只有1次消费且rollback可恢复；登录/凭据版本、logout及换session拒绝；原session等锁后过期不能改密；TOTP缺key/重放拒绝；20次合法确认后失败预算仍保留；无grant不产生资金预留；同业务编号重放不再消费；金额/商户政策改变不消费原grant；1002条无关组不会遮住后建费率；严格JSON/CSRF/no-store及100条批次容量；真实临时RSA+本地TLS请求验签，12并发仅一次外部发送、503未知保留预留、后续验签Query核实。其他增量正在写入时出现的编译红窗口须等稳定快照再执行整库，不将跳过或未执行的测试记为通过。

最终稳定快照定向命令 `go test -count=1 ./internal/identity ./internal/server ./internal/backoffice ./internal/payment ./tests/contract ./cmd/api -run 'TestFunds|TestAdminFunds|Test.*FundsAction|Test.*Batch.*Author|TestSettlementPayout'` 六包退出码0（identity28.674s、server1.128s、payment32.565s、contract17.958s；backoffice/cmd/api此正则无测试但成功编译）。同六包`go vet`退出码0。这是专项证据，不代替主任务整库回归、独立审阅和本地Docker部署结果。

源行为证据：Release `install/install.sql:11`默认admin_paypwd=123456；`admin/ajax_settle.php:160..186`全局比较和session原文授权；`admin/ajax_transfer.php:197..201`、`admin/ajax_order.php:334..340`直接比较；`admin/set.php:194..205`保存新原文配置。Pro公开assets的password/settle/order模块及管理员用户表单存在对应入口；其管理控制器有Swoole加密头，因此不能宣称已证实Pro内部哈希/TTL机制。Go版为明确安全收紧。
