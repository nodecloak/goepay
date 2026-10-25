# 支付宝 APP 授权与授权二维码

明文 Pro 插件 `AlipayPlugin.php` 的 `preauth()` 对应 apptype5 的 APP 冻结材料，`jsapipay()` 的授权分支对应10；二者复用 `alipay.fund.auth.order.app.freeze`，不是新的资金入账路径。apptype9 位于 `qrcode()`，使用 `alipay.fund.auth.order.voucher.create` 发授权二维码，不是协议签约。工厂显式识别5/9/10；同时配置5或10时优先 SDK 材料，与明文产品分支顺序一致。

二维码创建前持久化原 `preauth` 尝试、通道/AppID/付款身份摘要与只读核对任务。发码成功只保存 `qrcode` 材料，既不创建已授权记录，也不入账。必须验签精确响应节点并匹配 `out_order_no`、`out_request_no` 和 `code_type=qrCode`；只接受 HTTPS 的 `qr.alipay.com` 材料，拒绝伪域名、用户信息、非默认端口及 fragment。不读取返回的图片地址，不访问二维码 URL；PNG 在本地生成。

外呼中断、未认证结果或持久化结果失败不能触发重新发码。相同商户订单号重复调用返回原尝试；结果未知必须查原订单/授权。买家确认后的可信冻结通知、单次 capture、未知只查与全额未消费授权解除复用迁移23/25，不把授权号或二维码当作付款流水。后台与收银台明确显示授权和扣款的区别。

当前官方请求模型使用 `PRE_AUTH`、`pay_timeout`，Go 的9产品默认按该画像发码，默认2h；直接适配器自定义到期时间限制1分钟至15天，金额限制至100000000元。原明文 Pro 使用 `PREAUTH_PAY`、`time_expire`，两者有协议差异，不能静默假设等价。需保留旧产品画像时，可在显式加密通道配置中设置 `preauth_voucher_product_code=PREAUTH_PAY`；只接受这两个产品值，不允许请求方指定任意产品。该设置仅用于券二维码，不应用到 APP SDK 冻结。当前没有真实产品权限和设备联调，是否签约对应产品须独立确认。

字段画像依据 [官方请求模型](https://github.com/alipay/alipay-sdk-java-all/blob/master/v2/src/main/java/com/alipay/api/domain/AlipayFundAuthOrderVoucherCreateModel.java)、[官方响应模型](https://github.com/alipay/alipay-sdk-java-all/blob/master/v2/src/main/java/com/alipay/api/response/AlipayFundAuthOrderVoucherCreateResponse.java) 及[发码文档](https://developer.alibaba.com/docs/api.htm?apiId=1526&docType=4)，结合本地明文 Pro 处理分支实现；没有从境外授权产品的参数推断人民币画像。

本地验证已运行：显式5/9/10工厂映射、真实临时 TLS 请求/响应签名与订单匹配、恶意二维码拒绝、显式旧产品选择及错误配置、实际PG HTTP幂等发码/零余额/零授权/零凭证/未知不重发、保护收银台和本地PNG；对应测试为 `alipay_voucher_test.go`、`preauth_products_test.go`、`preauth_voucher_http_test.go`。最新四包定向与既有预授权回归均通过（adapter1.654s、channelconfig0.591s、payment7.419s、contract7.306s）。这是离线能力验收，不是生产全兼容声明。
