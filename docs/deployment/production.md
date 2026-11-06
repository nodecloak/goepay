# 生产部署：从本地演示到真实收款

`compose.yaml` 只用于本地体验：它固定使用 `simulation` 模拟通道、开启沙箱，不会向支付机构发出任何请求。接入真实支付需要把 API 和 Worker 切换到 `configured` 模式，并准备下面几项部署层配置。缺少其中任何一项，API 会在启动时退出并记录 `invalid local provider configuration`。

| 项目 | 本地演示（compose） | 真实收款（configured） |
| --- | --- | --- |
| `GOEPAY_PROVIDER_MODE` | `simulation` | `configured` |
| `GOEPAY_SANDBOX` | `true` | 不设置或 `false`（为 `true` 时拒绝启动） |
| `GOEPAY_PUBLIC_ORIGIN` | `http://127.0.0.1:8180` | 必须是 `https://域名`，不带路径和查询参数；支付机构回调地址由它生成 |
| 源通道 | 内置模拟通道 | `GOEPAY_CHANNEL_CONFIG_FILE` 指向的加密通道文件，至少登记一个通道 |
| 商户可用通道 | 种子商户 1000 | `GOEPAY_MERCHANT_CHANNELS`（必填，可为 `{}`）及可选的 `GOEPAY_DEFAULT_PAYMENT_CHANNELS` |
| 真实下单/退款 | 不发送 | `GOEPAY_ENABLE_PROVIDER_WRITES=true` 后才发送 |
| V2 接口签名 | 无平台密钥，V2 接口不可用 | `GOEPAY_PLATFORM_PRIVATE_KEY_FILE` |

## 1. 主密钥与数据库

1. 生成主密钥并离线备份：`openssl rand -hex 32`，设置为 `GOEPAY_MASTER_KEY`。加密通道文件、身份验证器、迁移归档等都用它保护，**丢失后无法解密，不能随意更换**。
2. 准备 PostgreSQL 与定期备份，设置 `DATABASE_URL`。
3. 执行迁移并创建管理员（不会创建示例商户）：

   ```sh
   cli migrate
   GOEPAY_SEED_PASSWORD='<至少 12 位>' cli seed-admin
   ```

   `seed-admin` 创建的首个管理员是超级管理员，并默认关闭“商户密钥登录”；生产环境不要运行仅用于沙箱的 `seed-local`。

## 2. 平台签名密钥

V2 接口的所有响应和 V2 商户异步通知都用平台私钥签名，商户用对应公钥验签。API 与 Worker 都需要读取同一个私钥文件。

```sh
GOEPAY_PLATFORM_PRIVATE_KEY_FILE=/etc/goepay/platform.pem cli gen-platform-key > platform-public.pem
```

- 私钥文件以 `0600` 权限创建；已有文件时命令拒绝执行，不会覆盖。
- 公钥会显示在商户后台“API 信息”中。更换平台密钥意味着所有商户都要更新验签公钥，请按计划轮换。

## 3. 源通道文件

源通道是支付机构账户的根配置（插件、产品、凭据）。后台“支付通道”里新增的通道必须选择一个已登记的源通道（同一支付插件），再填写自己的账户资料；因此文件里至少要有一个通道。

先写明文 JSON（只在受控的临时环境中保存），例如：

```json
{
  "version": 1,
  "channels": [
    {
      "id": 1,
      "plugin": "alipay",
      "app_types": ["1", "2"],
      "config": {
        "appid": "<支付宝应用 AppID>",
        "appsecret": "<应用私钥 PEM 或 Base64>",
        "appkey": "<支付宝公钥 PEM 或 Base64>"
      },
      "certificates": {}
    },
    {
      "id": 2,
      "plugin": "wxpay",
      "app_types": ["1", "2"],
      "config": {
        "appid": "<公众号或应用 AppID>",
        "appmchid": "<微信支付商户号>",
        "appkey": "<32 位 APIv2 密钥>"
      },
      "certificates": {
        "sslcert_path": "<apiclient_cert.pem 内容，退款需要>",
        "sslkey_path": "<apiclient_key.pem 内容，退款需要>"
      }
    }
  ]
}
```

- `app_types` 沿用原易支付插件的产品编号：支付宝 `1` 电脑网站、`2` 手机网站；微信 `1` 扫码（Native）、`2` 公众号（JSAPI）、`3` H5、`5` App。
- `config`/`certificates` 的键名与原插件配置字段一致；插件不同，所需字段不同。

加密、校验并删除明文：

```sh
GOEPAY_MASTER_KEY=... cli seal-channels < channels.json > /etc/goepay/channels.enc.json
GOEPAY_MASTER_KEY=... GOEPAY_CHANNEL_IMPORT_FILE=/etc/goepay/channels.enc.json cli check-channels
shred -u channels.json
```

`seal-channels` 会拒绝拼错的字段名、重复的通道 ID 以及插件无法构建的配置，并指出出错的通道 ID；输出中只列出通道 ID 与插件，不回显任何凭据。`check-channels` 只做离线检查，不向支付机构发请求，**也不能证明产品权限已开通**。

## 4. 商户可用通道

`GOEPAY_MERCHANT_CHANNELS` 在 configured 模式下必填，格式为 `{"商户ID":[源通道ID,...]}`，只能引用文件中登记的通道，例如 `{"1000":[1,2]}`。暂不授权任何商户时填 `{}`。

`GOEPAY_DEFAULT_PAYMENT_CHANNELS`（例如 `[1,2]`）只对**未出现在** `GOEPAY_MERCHANT_CHANNELS` 中的商户生效，仅用于收款，不授予转账或自动出款。用户组还可以在后台进一步收窄或关闭这些来源。详见 [default-payment-channels.md](default-payment-channels.md)。

## 5. 进程与环境变量

API（`/app/api`）：

| 变量 | 说明 |
| --- | --- |
| `DATABASE_URL`、`GOEPAY_MASTER_KEY` | 必填 |
| `GOEPAY_PROVIDER_MODE=configured` | 必填 |
| `GOEPAY_CHANNEL_CONFIG_FILE` | 加密通道文件路径 |
| `GOEPAY_PUBLIC_ORIGIN` | 商户 API 与回调使用的 HTTPS 地址 |
| `GOEPAY_CONSOLE_ORIGIN` | 后台地址；与 API 同域时可不设 |
| `GOEPAY_PLATFORM_PRIVATE_KEY_FILE` | 平台签名私钥 |
| `GOEPAY_MERCHANT_CHANNELS`、`GOEPAY_DEFAULT_PAYMENT_CHANNELS` | 见第 4 节 |
| `GOEPAY_ENABLE_PROVIDER_WRITES` | `true` 才会向支付机构下单、关单、退款；先保持 `false` 完成只读核对 |
| `GOEPAY_TRUSTED_PROXY_CIDRS` | 反向代理地址，见 [可信代理](../design/trusted-proxy.md) |
| `HTTP_ADDR` | 监听地址，默认 `127.0.0.1:8080` |
| `COOKIE_SECURE` | 默认开启 Secure Cookie；仅本地 HTTP 调试时设为 `false` |
| `GOEPAY_ADMIN_IDLE_MINUTES`、`GOEPAY_MERCHANT_IDLE_MINUTES` | 后台会话空闲多少分钟后失效（`0` 为只保留 12 小时绝对期限）；管理员默认 30，商户默认 0 |

Worker（`/app/worker`）负责商户异步通知与结果核对：

| 变量 | 说明 |
| --- | --- |
| `DATABASE_URL`、`GOEPAY_MASTER_KEY` | 必填 |
| `GOEPAY_PLATFORM_PRIVATE_KEY_FILE` | V2 订单的商户通知签名需要 |
| `GOEPAY_PROVIDER_MODE=configured`、`GOEPAY_CHANNEL_CONFIG_FILE`、`GOEPAY_ENABLE_PROVIDER_QUERIES=true` | 需要自动查询未知结果（未知支付、退款、转账）时设置；不设置时只发送通知 |

其它调度进程（结算、推广、商户提醒、计划任务）各自默认关闭，按 `.env.example` 中的说明单独开启。

## 6. 上线检查

1. `GET /readyz` 返回 200（数据库可用且迁移完整）。
2. 商户后台“API 信息”显示平台公钥，与 `platform-public.pem` 一致。
3. `GOEPAY_ENABLE_PROVIDER_WRITES=false` 时，确认后台能看到源通道、商户授权与用户组配置正确。
4. 开启写入后，用测试商户发起最小金额订单：支付机构回调到达 `/callbacks/providers/<通道ID>/<订单号>`，订单变为已支付，商户收到异步通知。
5. 对该订单退款，并用 `/api/pay/refundquery` 查到终态 `status=1`。
6. 运行 `cli reconcile`，所有一致性计数为 0。
7. 确认数据库与主密钥的备份、恢复流程可用。

## 常见启动失败（`invalid local provider configuration`）

- `GOEPAY_SANDBOX=true` 与 configured 模式同时设置。
- `GOEPAY_PUBLIC_ORIGIN` 不是 `https://` 开头，或带有路径、查询参数。
- `GOEPAY_CHANNEL_CONFIG_FILE` 未设置、无法用当前主密钥解密，或文件中没有任何通道。
- `GOEPAY_MERCHANT_CHANNELS` 未设置、不是 JSON 对象，或引用了文件中不存在的通道 ID。
- `GOEPAY_ENABLE_PROVIDER_WRITES` 等开关取值不是 `true`/`false`。
- 开启 `GOEPAY_ENABLE_SCAN_RECOVERY` 时没有同时开启写入和查询。
