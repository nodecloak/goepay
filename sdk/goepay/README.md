# GoEPay Go SDK 1.0

这是本项目的 V2 商户支付 Go SDK，使用 Go 标准库，最低 Go 1.22。

本 SDK 实现以下路径：

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| `SubmitURL` | `/api/pay/submit` | 只在本地构造网页支付 URL，不发请求、不创建订单 |
| `Create` | `/api/pay/create` | 发起一次 V2 下单请求 |
| `Query` | `/api/pay/query` | 查询原平台订单号或商户订单号 |
| `Refund` | `/api/pay/refund` | 提交一次退款申请 |
| `RefundQuery` | `/api/pay/refundquery` | 按原退款标识查询 |
| `Close` | `/api/pay/close` | 申请关闭原订单 |
| `VerifyNotification` | 商户自己的通知地址 | 只校验 V2 RSA、时间戳、参数唯一性及商户 ID |

实际参数、权限、通道能力及状态以当前站点 `/doc/` 文档与商户后台配置为准。SDK 不接管收银台、不渲染返回的 HTML、不打开支付链接、不做后台管理或转账操作。它不支持 V1 MD5 通知；V1 地址即使采用 RSA 请求，其订单通知仍按 V1 规则处理。

## 下载后使用

下载包是独立 Go module，模块名 `example.com/goepay-sdk` 是保留示例名，**没有宣称它已发布到公开模块仓库**。将解压得到的 `GoEPay_Go_SDK` 目录放到自己的工程 `third_party/goepay-sdk` 后，在自己的项目根目录执行：

```sh
go mod edit -replace example.com/goepay-sdk=./third_party/goepay-sdk
go mod edit -require example.com/goepay-sdk@v0.0.0
go test ./...
```

在商户应用中使用 `import goepay "example.com/goepay-sdk"`。此仓库本身的包路径为 `goepay/sdk/goepay`；公开下载包的模块名由打包工具生成。SDK 没有第三方依赖，解压目录内可直接运行 `go test ./...` 和 `go vet ./...`。测试只使用内存生成的一次性 RSA 密钥、本机 `httptest` 和保留示例域名；不会访问真实通道。

## 初始化与离线构造 URL

从商户后台取得并妥善保管商户私钥与平台公钥。私钥为单个未加密 PKCS#8 `PRIVATE KEY` 或 PKCS#1 `RSA PRIVATE KEY` PEM，公钥为 PKIX `PUBLIC KEY` 或 PKCS#1 `RSA PUBLIC KEY` PEM，RSA 至少 2048 位。这里的“未加密 PEM”指 SDK 读取格式；实际密钥文件和密钥管理系统必须受到访问控制，SDK 不负责磁盘加密或密钥解密。

以下函数仅构造 URL，不发起网络请求。示例使用保留域名，不应直接用于真实付款：

```go
package integration

import (
    "net/url"
    "os"
    goepay "example.com/goepay-sdk"
)

func buildPaymentURL() (string, error) {
    privatePEM, err := os.ReadFile("/run/secrets/merchant-private.pem")
    if err != nil { return "", err }
    publicPEM, err := os.ReadFile("/run/secrets/platform-public.pem")
    if err != nil { return "", err }
    client, err := goepay.New(goepay.Config{
        BaseURL: "https://pay.example.com", // 换成实际 Go 服务 HTTPS 根地址
        MerchantID: 1001,                 // 换成自己的商户 ID
        MerchantPrivateKeyPEM: privatePEM,
        PlatformPublicKeyPEM: publicPEM,
    })
    if err != nil { return "", err }
    defer client.CloseIdleConnections()
    return client.SubmitURL(url.Values{
        "out_trade_no": {"your-stable-order-id"},
        "name": {"示例商品"},
        "money": {"1.00"}, // 元的十进制字符串；不要经过 float64
        "type": {"alipay"},
        "notify_url": {"https://merchant.example.com/payments/notify"},
        "return_url": {"https://merchant.example.com/payments/return"},
    })
}
```

不要在 `url.Values` 中自行填写 `pid`、`timestamp`、`sign_type`、`sign`，SDK 会填充并拒绝覆盖。参数是已解码的原始 UTF-8 字符串；SDK 先按原值签名，再进行 form 编码，不能先 `QueryEscape` 或 `PathEscape`。重复参数以及 Go 服务拒绝的歧义键名也会被拒绝。示例 merchant ID 和订单号是占位值，不包含任何安装凭据。

## 显式调用与未知结果

调用形式为 `response, err := client.Query(ctx, url.Values{"out_trade_no": {originalID}})`。其余四个 POST 方法具有相同签名 `func(context.Context, url.Values) (*Response, error)`。调用前需要明确该操作的授权与业务规则；下单、退款和关闭都可能改变远端状态。

API 请求固定为原 HTTPS 站点的 POST form，不带 URL 查询参数。默认总超时 10 秒，可通过 `Timeout` 改为正值；不从环境读取代理、不跟随重定向。每次显式调用只有一个 SDK 应用请求，不自动重试，不发送可重放 body。成功与业务错误都存在平台签名时，都会先验证 RSA 与当前时间 ±300 秒的时间戳；协议之外的成功、缺失/错误签名、重复 JSON 键、非 JSON、过大响应均拒绝。

SDK 使用自己构造的 HTTP transport，不依赖或复制应用的 `http.DefaultTransport`。商户应用替换全局 RoundTripper、设置 typed nil 或自定义全局 DialContext，不会使 SDK 构造 panic，也不会让全局拨号逻辑接管 SDK 的签名请求。

`Response.Fields` 保留标量原值；金额是字符串，大整数不经过浮点数。`Response.Code == 0` 只说明该次接口返回成功，**不能单凭它判断实际支付或退款已完成**。`Refund` 应由业务保存稳定的 `out_refund_no` 后再调用；申请接受或出现未知结果后，使用原单号查询。付款结果应核对原订单状态，退款完成应核对退款查询状态。查询本身可能触发服务端核对通道，因此它也需要正确权限和原始标识。

网络超时、连接中断、HTTP 失败或无效签名都可能留下未知结果，不能据此自动换单重付、重复退款或宣称已失败。先保留原商户订单号/退款号，再查询原请求。只有业务确认结果并完成相应核对后，才改变自身订单状态。

## 错误的认证边界

`errors.As(err, &apiError)` 可取得 `*goepay.APIError`。`Authenticated == true` 表示该错误中的标量字段经过平台签名及时间校验；`false` 的未签名错误信息仅用于排查，不能视为可信的业务结论。非零 code 的签名错误同样会返回 `Response`，保留 `out_refund_no` 等原始标识。签名不正确时拒绝整个响应，不向调用者提供伪造的已认证结果。

本 SDK 当前只支持上表五个返回 JSON 的支付路径以及离线页面提交。Go 服务的原协议不把顶层 JSON 数组或对象放入签名，故 SDK **拒绝任何含顶层数组/对象字段的回复**，不把未被签名的数据伪装成已认证字段；商户列表、转账等其它接口不在此版本支持范围内。即使拥有 RSA 验签，仍必须使用正常证书校验的 HTTPS。`MaxResponseBytes` 为 1 MiB，超过限制的 HTML 等支付材料需使用站点收银台方式或先评估业务需求。

## V2 通知处理

平台通知采用商户配置地址上的 GET 查询参数；`notify_url` 原有 query 字段也在签名内。服务端应使用 `values, err := url.ParseQuery(r.URL.RawQuery)` 并拒绝解析错误，再把**完整的** `values` 传给 `VerifyNotification`，不要只挑选已知字段、重新编码值或悄悄丢掉重复字段。不要使用会忽略 `ParseQuery` 错误的快捷方法代替严格解析。

验签返回的 map 只证明 RSA、时间戳及商户 ID 校验通过。接着必须按自己的持久化记录核对原订单号、金额、币种/实际业务约定和 `trade_status == "TRADE_SUCCESS"`；不把异步通知中的 name 当作订单身份，站点可能按配置将其替换为 `product`。采用数据库事务或等价机制实现业务幂等，提交成功后才以 HTTP 200 和**精确的**正文 `success` 确认；多余空格、换行或其它正文不等于确认。失败时不要误回 `success`。

时间窗不是业务防重放或一次性处理保证。平台重试通知时会重新生成时间戳和签名，所以应按订单业务身份去重；重复通知已被正确处理时也应返回相同确认。浏览器 return 页面只用于用户展示，不能单独触发发货或入账。

## 本地验证与打包

仓库内还包含 `server_compat_test.go`，它直接与 Go 服务的请求验签和通知构造实现交叉验证；这个文件依赖服务内部包，因此不会进入独立 SDK ZIP。公共 ZIP 保留 portable 单测与可运行的 `ExampleClient_SubmitURL`，示例只输出路径，不打印完整签名链接，不会产生支付。

维护者在仓库根目录运行 `python sdk/goepay/package.py` 可生成确定性 ZIP，并在 `.tools/sdk-go/package.json` 写入清单及 SHA。该步骤不包含密钥、编译产物、环境文件、第三方依赖或现有安装配置。`AllowLoopbackHTTP` 仅供明确的本地测试，允许字面量 loopback IP，不能用于外网 HTTP 或 localhost 名称。共享 Client 可并发调用；短期实例结束时用 `CloseIdleConnections` 清理空闲连接。
