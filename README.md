<p align="center">
  <a href="https://goepay.cc/">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="web/public/static/goepay-wordmark-ondark.svg">
      <img src="web/public/static/goepay-wordmark-onlight.svg" width="240" alt="GOEPAY">
    </picture>
  </a>
</p>

<h1 align="center">GOEPAY</h1>

<p align="center">兼容易支付 API，为高并发支付而构建。</p>
<p align="center">Go + PostgreSQL 支付引擎 · 管理员与商户后台 · 自主部署</p>

<p align="center">
  <a href="https://goepay.cc/">官网</a> ·
  <a href="manual/index.md">使用手册</a> ·
  <a href="https://goepay.cc/doc/">开发文档</a> ·
  <a href="https://goepay.cc/doc/pay_create.html">在线沙箱</a> ·
  <a href="https://goepay.cc/doc/sdk.html">Go SDK</a> ·
  <a href="https://goepay.cc/pricing/">商业授权</a>
</p>

## 为每一笔支付，构建更强的底座

GOEPAY 是使用 Go 构建的支付网关，采用 PostgreSQL 存储订单与账务数据，并提供 Vue 3 管理员后台和商户后台。沿用易支付接口路径、商户参数、签名及回调格式，帮助已有业务平滑接入，让运营方自主掌握部署、配置与数据。

### 兼容易支付 API

支持支付下单、订单查询、退款、商户查询和签名通知等流程；提供 V2 RSA / SHA-256 签名与时间戳校验，以及 V1 MD5 协议适配。可对接支付宝、微信支付，具体产品、参数和通道配置以接口文档及实际部署能力为准。

新系统订单号采用北京时间的 `年月日时分秒＋13 位唯一流水号`，例如 `202611040912340000000000001`，固定为 27 位。流水号不按天重置，并发和重复提交由数据库保证唯一与幂等；API 中仍按字符串传递，历史订单号保持原值。

### 更安全，更可靠

通过商户独立密钥、严格验签、权限隔离与幂等处理保护支付流程。后台提供持久会话、CSRF 防护、Argon2id 密码哈希与 TOTP；回调校验订单身份和金额，敏感操作保留审计记录。

账务采用整数分金额模型和不可变复式账本，结合数据库事务、费用快照、退款与转账预占、未知结果核对，帮助保持订单状态与资金记录一致。

### Go 原生高并发

轻量 goroutine、数据库连接池与异步任务协同处理订单、回调和通知。API、Worker 与调度任务分开运行，可按部署环境配置处理能力；实际吞吐量取决于硬件、数据库、通道响应及业务负载。

### 运营管理清晰有序

在统一工作台管理商户、支付通道、订单、退款、结算与报表，为管理员和商户提供各自的权限与操作范围。完整功能状态见 [功能清单](web/capabilities.json)。

易支付迁移可使用后台的 [迁移向导](docs/migration/WIZARD.md)：上传 MySQL 备份、预检查并原子导入到全新实例。核心历史与期初余额原生导入，其它原始数据加密归档；通道、旧凭据和插件不会自动启用。迁移安装请使用 `cli seed-admin` 初始化管理员，跳过会创建示例商户的 `seed-local`。

## 快速开始

本地体验需要 Git、Docker 和 Docker Compose。镜像构建包含 Go 服务与前端资源，无需先在宿主机安装 Go 或 Node.js。

```sh
git clone https://github.com/nodecloak/goepay.git
cd goepay
cp .env.example .env
```

编辑 `.env`，将以下占位配置替换为独立的随机值：

| 配置 | 用途 |
| --- | --- |
| `POSTGRES_PASSWORD` | PostgreSQL 数据库密码 |
| `GOEPAY_MASTER_KEY` | 凭据加密主密钥，32 字节、64 位十六进制字符串 |
| `GOEPAY_SEED_PASSWORD` | 初始后台密码，至少 12 个字符 |
| `GOEPAY_SEED_API_KEY` | 初始商户 API 密钥，至少 24 个字符 |

然后启动服务并初始化本地账号：

```sh
docker compose up -d --build
docker compose exec api /app/cli seed-local
```

Compose 会先执行数据库迁移，再启动 API 与后台任务。访问入口如下：

| 服务 | 默认地址 |
| --- | --- |
| 管理员后台 | <http://127.0.0.1:8181/admin/dashboard> |
| 商户后台 | <http://127.0.0.1:8181/merchant/dashboard> |
| API | <http://127.0.0.1:8180> |
| 就绪检查 | <http://127.0.0.1:8180/readyz> |

本地初始账号为 `admin`、`merchant`，密码使用 `.env` 中的 `GOEPAY_SEED_PASSWORD`，初始商户 PID 为 `1000`。初始化命令只创建缺失账号，保留已有账号的密码与密钥。

默认 Compose 使用 `simulation` 模拟通道，供本地体验和流程验证使用。接入真实支付前，需要另行配置通道凭据、商户授权范围、平台与商户 RSA 密钥，以及相应的外部执行开关。请妥善备份数据库与主密钥。

## 接口与接入

V2 接口入口如下，详细请求方法、签名规则、返回字段及错误处理见 [官方接口参考](https://goepay.cc/doc/index.html)。

| 接口 | 用途 |
| --- | --- |
| `/api/pay/submit` | 页面跳转支付 |
| `/api/pay/create` | API 下单，返回支付链接或二维码 |
| `/api/pay/query` | 查询订单 |
| `/api/pay/refund` | 申请退款 |
| `/api/pay/refundquery` | 查询退款 |
| `/api/pay/close` | 关闭订单 |
| `/api/merchant/info` | 查询商户信息与余额 |
| `/api/merchant/orders` | 查询商户订单列表 |

商户服务端应生成请求签名，并验证响应与支付通知。可从 [Go SDK 接入指南](https://goepay.cc/doc/sdk.html) 开始，结合 [签名规则](https://goepay.cc/doc/sign_note.html) 和 [支付通知说明](https://goepay.cc/doc/pay_notify.html) 完成对接。

官网提供独立的 [在线接口沙箱](https://goepay.cc/doc/pay_create.html)，用于体验参数校验、RSA 签名和模拟响应。文档中的 `pay.example.com` 为示例地址，接入时替换为自行部署的网关域名。

## 部署与迁移

1. **准备部署环境**：配置 Go 服务、PostgreSQL、域名、HTTPS 和备份策略。
2. **配置商户与通道**：设置商户凭据，并接入已开通的支付宝或微信支付产品。
3. **验证后再切换**：在测试环境核对下单、查询、回调、退款及账务，再切换业务流量。

项目提供 API、Worker、迁移 CLI 与多种后台调度进程。生产环境应按实际使用的支付产品配置服务，接口兼容范围和功能实施情况可查阅 [实施状态](docs/implementation-status.md)；历史数据迁移步骤见 [迁移说明](docs/migration/RUNBOOK.md)。

## 开发与验证

源码开发使用 Go 1.27.3、Node.js 24 和 PostgreSQL；前端基于 Vue 3、TypeScript 与 Vite。版本要求以 `go.mod`、构建文件和前端依赖为准。

后端测试需要显式指定独立的本地 PostgreSQL 测试库。PowerShell 下可运行：

```powershell
$env:GOEPAY_TEST_DATABASE_URL = '<本地 PostgreSQL 测试 DSN>'
./scripts/test.ps1
```

也可在前端目录单独运行检查：

```sh
cd web
npm ci
npm run typecheck
npm test
npm run build
```

数据库测试缺少 DSN 时可能跳过，模拟通道测试也不能替代实际支付产品的联调验收。验证范围与执行记录见 [测试报告](docs/test-results.md)。

## 项目结构

```text
cmd/          API、CLI、Worker 与调度进程
internal/     支付、账务、通道适配及后台业务
db/           PostgreSQL 数据库迁移
web/          Vue 3 管理员与商户后台
sdk/goepay/   Go 商户接入 SDK
api/          接口规范
tests/        契约与集成测试
scripts/      开发、测试与运维脚本
docs/         设计、迁移与验证资料
```

## 使用与授权

GOEPAY 自研代码采用 [GNU AGPL v3.0](LICENSE)（`AGPL-3.0-only`），同时提供可选商业授权。个人和商业使用者均可在遵守 AGPL-3.0 的前提下使用、修改和分发，无需购买商业授权。

个人和商业使用者均可自愿购买商业授权；购买的授权终身有效、按域名绑定，具体许可范围以正式商业授权文件为准。商业授权的域名约定不限制 AGPL-3.0 授予的权利。详见 [开源与商业授权说明](docs/LICENSING.md) 及 [官网授权入口](https://goepay.cc/pricing/)。支付通道费用以服务商规则为准。

---

[访问官网](https://goepay.cc/) · [阅读文档](https://goepay.cc/doc/) · [反馈问题](https://github.com/nodecloak/goepay/issues)
