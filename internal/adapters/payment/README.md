# 官方支付适配器

此目录独立处理支付通道协议，不直接修改订单或余额。`internal/payment/ProviderFlow` 持久化通道尝试，保存未知结果，并在验签后幂等入账；HTTP 下单、受保护收银台、回调提交后 ACK 和只读结果核对任务已接入。所有已运行的部署演练均使用本地 simulation 或 httptest，未发起真实支付、退款或转账。配置模式默认关闭外部写操作，独立 worker 还须显式开启查询开关。

## 已定位来源

- 本地 Release：`plugins/wxpay` 为微信 APIv2；`plugins/wxpayn` 为微信 APIv3；`plugins/alipay` 为支付宝官方 RSA2。
- 用户远端实际 Pro 源码：`reference/server-pro/plugins/payment/wxpay/WxpayPlugin.php`（1.2）和 `alipay/AlipayPlugin.php`（1.4）。实际启用微信 Native/JSAPI（apptype 1/2），支付宝电脑网站/手机网站（apptype 1/2），支付宝为公钥模式。
- Pro 本地供应商 SDK：`epay_pro/vendor/cccyun/wechatpay-sdk/src/BaseService.php`、`alipay-sdk/src/Aop/AopClient.php`。

远端配置和证书不属于此目录，不写入测试样本和报告。这里只使用临时测试密钥。

## 接口与边界

`Provider` 提供 Create、Query、Close、Refund、QueryRefund；金额为整数分。Create 使用稳定平台 TradeNo，Refund 使用 `RF` 加平台退款 ID，不直接使用可在不同商户间重复的客户端退款号。查询返回上游状态，由应用层建立显式状态映射后再影响账务。

`NewWechatV2(WechatV2Config)` 支持实际启用的 Native/JSAPI，Native 返回二维码，JSAPI 返回完整 JSON（appId/timeStamp/nonceStr/package/signType/paySign）。微信退款仅在显式配置 RefundClient 时开启；调用方必须给该客户端装载正确商户 mTLS 证书。配置中注入客户端/网关便于本地 httptest；线上网关和传输策略由可信启动配置锁定。退款查询不需要 mTLS。退款提交成功先返回 PROCESSING，必须以退款查询/可信通知确认最终状态。

`NewAlipay(AlipayConfig)` 默认仅启用 page/wap，生成 RSA2 签名并做 HTML 属性转义的 POST 表单，用户提交后才访问支付宝；不在生成材料时请求上游。native 只在显式配置该产品时使用。Query/Close/Refund/QueryRefund 使用签名表单 HTTP 请求并验证原始响应 JSON 节点的 RSA2 签名。

适配器克隆注入的 http.Client，强制禁止重定向，且设置不超过 15 秒的总超时，避免重定向重放签名材料。调用方仍须约束目的域名、TLS 验证和证书。

`TransferProvider` 是同一微信/支付宝插件内的转账扩展，`TransferTypes()` 显式报告支持的产品。当前实现微信 APIv2 零钱转账/查询（两者均须 mTLS）和支付宝公钥模式账号转账/查询；上游编号由业务层使用 `TF` 加平台转账 ID。查询验证平台编号、金额；微信另验证 AppID、商户号与收款 openid。微信旧版转账返回 XML 不带签名，依赖校验服务器证书的 TLS、客户端 mTLS、固定上游以及响应业务身份；如果响应带签名，仍必须验证。支付订单接口继续强制验证响应签名。

支付宝另提供 `TransferProofProvider`（回单申请/查询）和 `ChannelBalanceProvider`（通道账户余额与额度）。回单申请仅返回 FileID，不在适配器内轮询或重复申请；业务层持久化后按该 FileID 查询。申请也受外部写开关限制。回单成功下载地址仅接受 HTTPS，适配器不下载文件。通道账户余额不能作为商户账本可用余额。

当前银行卡转账、支付宝证书模式及未启用的支付产品均明确拒绝，不会构造成功结果。完整来源方法矩阵见 `docs/design/two-plugin-methods.md`。

任何外部写请求在网络失败、非正常 HTTP、畸形响应、验签失败或缺少成功材料时返回 `*UnknownOutcome`。应用必须保存待查证状态，按同一业务号查询，禁止直接释放预占或创建新业务号重试。明确且可信的业务拒绝使用 `ProviderError`。不得将错误签名响应映射成成功。

`NotificationVerifier` 要求给出 ExpectedPayment。微信校验 APIv2 签名、AppID、商户号、交易号和整数金额；支付宝校验 RSA2、AppID、配置/预期卖家 ID、交易号、金额和交易状态。验签成功不等于可重复记账；事件需经过持久化去重、订单状态校验及账务事务。支付宝 TRADE_FINISHED 仍须按稳定交易号去重，不能再次入账。支付宝无配置 SellerID 时，应由可信进件/订单资料提供预期卖家 ID，不能信任回调自行声明的身份。

## 签名差异

验签成功的支付通知在适配器内归一为 `State=SUCCESS`；支付宝原始 TRADE_SUCCESS / TRADE_FINISHED 均必须先通过签名、身份与金额检查。应用层仍按稳定交易号去重，不会因为交易结束通知再次入账；查询结果则保留上游状态以供显式映射。

- 微信 APIv2：排除 sign 和严格空字符串，保留 sign_type，按键排序，拼接 `&key=...`，MD5/HMAC-SHA256 输出大写。与易支付外层 MD5 完全不同。
- 支付宝请求：包含 sign_type；通知验签排除 sign/sign_type。本地 PHP AOP SDK 还排除 trim 后空值和以 `@` 开头的文件标记值。因此通知中被 SDK 排除的额外字段不能视为可信，业务身份和金额都必须独立匹配 ExpectedPayment。
- 微信 APIv3 辅助函数保留请求目标与原始 body 精确字节、每行末尾换行。响应/通知按配置可信序列号选择公钥，并校验时间窗口。AES-256-GCM 资源解密须在外层原始通知验签后调用。本轮实际启用通道为 APIv2，未实现/接入 APIv3 通道客户端，也不自动下载或信任未知证书。

## 测试

```powershell
$env:GOEPAY_PHP_INTEROP='1'
& D:/GoLang/sdk/go1.21.3/bin/go.exe test -count=1 ./internal/adapters/payment
& D:/GoLang/sdk/go1.21.3/bin/go.exe vet ./internal/adapters/payment
```

覆盖本地模拟上游的微信下单/查单/关闭/退款/退款查询、完整 JSAPI 二次签名、支付宝网页材料/查单/退款/退款查询、回调错误金额/卖家拒绝、写请求未验签响应/缺材料为 UnknownOutcome、重定向拒绝；XML 重复字段/嵌套元素/DTD/多根拒绝；APIv3 精确签名字节、时间/序列号/重复签名头、AES-GCM 已知固定向量。

`TestOriginalProviderPHPInterop` 直接运行原 Pro 供应商 PHP SDK，微信 MD5/HMAC 和支付宝 RSA2 签名结果与 Go 相同；PHP SDK 可验证 Go 通知签名。Docker 禁网、临时测试 RSA 密钥；没有读取生产凭据，没有真实支付、退款或关闭动作。

## 官方规则参考

- [微信 APIv3 请求签名](https://pay.wechatpay.cn/doc/v3/merchant/4012365336)
- [微信 APIv3 签名/验签工具](https://pay.wechatpay.cn/doc/v3/partner/4012365880)
- [微信支付通知与资源解密](https://pay.wechatpay.cn/doc/v3/merchant/4012791861)
- [微信 APIv2 零钱付款查询：mTLS 与结果未知处理](https://pay.wechatpay.cn/doc/v2/merchant/4011989767)
- [微信 APIv2 零钱付款](https://pay.wechatpay.cn/doc/v2/merchant/4011989673)

供应商具体产品权限、证书模式、服务商/子商户、预授权、分账、付款码和投诉仍须按方法矩阵补齐；此目录测试通过不表示这些能力已交付。
