# GOEPAY

Go + PostgreSQL 支付平台，包含 API、Worker、迁移 CLI 与 Vue 3 管理员/商户后台。工作目录为 `E:\GOEPAY`。当前实施与限制见 [实施状态](docs/implementation-status.md)，实际验证见 [测试报告](docs/test-results.md)。

最新独立模拟预览为**149/schema89（8187/API8186）**，旧8185固定98保留。恢复双角色原结算列、原收款快照和详情、核实到账/完成时间，修复翻页待加载时旧勾选仍能提交的竞态，统计页签相邻对齐。453单元/324浏览器通过，9项显式环境用例未执行；后台字节及8程序摘要与148一致，按148/147/145已验证范围承接，148全仓测试仍运行中。实机28视图/86只读请求、82页与33零账务核对通过。见[149验证边界](docs/verification/local-preview-ux149-2026-10-26.md)。全部原条件功能与UX仍继续整改。

本轮按用户要求仅接入微信、支付宝，其他业务插件暂缓。已实现支付、可信回调、查询、关单、退款及钱包转账的主要流程，并使用本地假上游验证。**尚未达到原版全部功能和所有支付产品完全兼容的验收条件**；同一插件中未实现的产品与规则也明确列在 [方法映射](docs/design/two-plugin-methods.md) 和 [转账边界](docs/design/provider-transfers.md)。

## 本地运行

当前复核预览为 [管理员后台](http://127.0.0.1:8187/admin/dashboard) 和 [商户后台](http://127.0.0.1:8187/merchant/dashboard)，API为 `http://127.0.0.1:8186`。版本、源码及实际镜像见 `.tools/preview-current-path.txt` 指向目录的 `CURRENT.json`；本地账号admin/merchant，密码只从该目录合成配置 `GOEPAY_SEED_PASSWORD` 读取。旧8185/API8184保留为固定98比较基线，当前界面请使用8187。

以下是另外新建默认Compose环境的通用方式，与本轮独立验收项目的端口分别记录：

需要 Docker Compose。复制 `.env.example` 为 `.env`，替换为随机本地数据库密码、32 字节主密钥（64 位十六进制）、种子密码和 API key。现有 `.env` 是本机测试配置，不提交、不用于生产。

```powershell
Set-Location -LiteralPath 'E:\GOEPAY'
docker compose up -d --build
docker compose run --rm --entrypoint /app/cli api seed-local
```

Compose 的 migrate 服务先执行数据库迁移，API/Worker 等迁移成功后启动。已执行迁移的 SHA-256 变化会被拒绝；修复使用新迁移。API `/readyz` 检查数据库与迁移状态。

- 前端：`http://127.0.0.1:8181/admin/dashboard`、`/merchant/dashboard`。
- API：`http://127.0.0.1:8180`；PostgreSQL：`127.0.0.1:55433/goepay_final`。
- 本地账号：`admin`、`merchant`，密码读取 `.env` 的 `GOEPAY_SEED_PASSWORD`；商户 PID 为 1000。
- seed 只创建缺失账号，不覆盖现有密码或密钥。保留主密钥和数据库备份；丢失主密钥会使旧凭证无法解密。
- 默认 `simulation` 模式只使用本地模拟通道，不加载 `.secrets`，不连接微信、支付宝或生产系统。模拟通道的上游状态在进程内，重启后不保留；账本与资金记录仍在 PostgreSQL。

## 已有能力

整数分资金模型、不可变复式账本、费用快照、退款/转账预占与未知结果核对、持久任务和通知；V1 MD5/V2 RSA 兼容适配；显式支持的微信、支付宝支付产品；受保护收银台、回调验签和订单身份/金额校验；退款与转账的全局上游业务号避免跨商户碰撞。

后台使用持久会话、CSRF、Argon2id、TOTP、受限管理员与商户归属检查。支持资源管理、本地审核、账户安全、会员、充值、保证金、账务申请、站内转账/分账、报表和运行管理。82 个原版菜单中 80 项具有部分能力、2 项因其他插件暂缓；逐项状态见 [capabilities.json](web/capabilities.json)。本地审核不等于上游进件或投诉处理，结算核准不等于银行出款。

真实配置模式必须显式提供加密配置文件、商户通道白名单和外部写入开关；默认关闭。Worker 仅在显式启用查询时核对已授权通道，不重新发送资金请求。V2 需要单独配置平台 RSA 私钥与商户公钥；默认 Compose 未挂载真实密钥。

独立应用标识查询使用加密配置中的显式OAuth应用档案。配置模式下须同时设置 `GOEPAY_ENABLE_OAUTH=true`、`GOEPAY_ENABLE_IDENTITY_LOOKUP=true`，且档案本身启用并授权指定商户；独立应用可以与支付AppID不同。付款写开关保持关闭时也可进行单独身份交换，但不会创建支付订单或发起资金操作。默认simulation不加载这些档案；微信mini档案另需显式 `enable_url_link=true` 才允许尝试一次真实URL Link生成，否则提供原SDK资料。链接结果未知不重发，二维码仅本地编码已验证的返回链接。另有默认关闭的独立支付宝应用授权，仅原super会话使用专用ISV档案并单次展示凭证，不自动绑定支付通道。开发、固定候选与原本地部署的区别见下方记录。

独立验收项目使用显式字段映射，时间展示采用北京时间；金额和ID保留精确字符串。固定80已整合原订单条款日结、退款回收和默认关闭的TLS邮件验证码发送，并通过252前端单元及130项mock浏览器；其完整后端验证和后续订单码支付增量仍按[最新验收记录](docs/verification/integrated-80-2026-10-24.md)逐项记录。旧版本的通过结论不替代新候选的验证，不据局部通过声明全部功能完成。

## 验证

```powershell
$env:GOEPAY_TEST_DATABASE_URL='<本地 PostgreSQL 测试 DSN>'
./scripts/test.ps1
$env:GOEPAY_PHP_INTEROP='1'
go test ./internal/compat ./internal/adapters/payment -count=1
node scripts/smoke.mjs
node scripts/provider-smoke.mjs
./scripts/restore-test.ps1
```

数据库测试使用独立随机 schema；没有测试 DSN 时集成测试会跳过，不能据此声称数据库验收通过。浏览器、Linux race、依赖检查与执行证据见 [测试报告](docs/test-results.md)。脱敏历史迁移工具说明见 [迁移演练](docs/migration/RUNBOOK.md)。没有执行生产操作或真实资金交易，也没有旧 PHP 性能对照结论。
