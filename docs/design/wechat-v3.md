# 48 直连微信APIv3

使用固定官方HTTPS域名，有界HTTP客户端且禁止重定向。商户RSA请求签名覆盖method/requestURI/timestamp/nonce/原始body；原始响应及通知先按本地明确配置的微信支付公钥ID/平台serial验签与5分钟窗口检查。未知serial不自动下载信任。递归拒绝重复JSON键、深度超过32、正文超过1MiB。未验证签名的业务错误、5xx、网络错误、非预期状态及不一致业务证据均为写结果unknown；无自动写重试。

支持直连native/jsapi/h5/app创建、原号查询、关闭、退款与查退款；JSAPI和APP为RSA二次签名。异步支付通知须验签、AES-256-GCM解密、核对event/resource/app/mch/订单/金额/CNY。V3回调成功204空body，错误400 JSON；现有V2保持XML。查询/退款状态依原始业务号和金额核对，不把PROCESSING或ABNORMAL当成功。

显式配置：原plugin仍wxpay，`config.wechat_api_version=v3`，`appid`、`appmchid`、`v3_merchant_serial`、32字节`v3_api_key`；`certificates.v3_merchant_key`为商户私钥PEM；每个可信验签公钥以`certificates.v3_verifier_<serial>`提供PEM（最多8个，支持PUB_KEY_ID及已核实的平台serial）。不从输入URL或磁盘路径加载证书。H5另需h5_site_url/h5_site_name，AppTypes沿用1/2/3/5。

仅配置工厂离线构建不外呼，写入仍有原allowLive开关。未配置版本保持V2，未知版本拒绝；V3不能静默开启V2付款码、银行卡出款或小程序启动。不宣称服务商、电商、子商户或分账流程已落地。分账声明须以后续原订单持久政策实施，不能由当前通道开关追认历史单。

48核对发现guarded包装层只透传AppID而未透传TransferAccountIdentity；真实配置工厂红测重现。已增加转发，真实商户号替换改变scope而仅密钥轮换不变。原弱scope历史真实意图因此保守拒绝，不自动重写旧HMAC证明。第47快照全库通过不包含该缺口的修复和证据。

官方依据：[签名验签](https://pay.wechatpay.cn/doc/v3/merchant/4012365342)、[平台验签](https://pay.wechatpay.cn/doc/v3/merchant/4013053420)、[官方Go SDK Native模型](https://github.com/wechatpay-apiv3/wechatpay-go/blob/main/services/payments/native/models.go)、[官方退款模型](https://github.com/wechatpay-apiv3/wechatpay-go/blob/main/services/refunddomestic/models.go)。2026-10-23核对；不将临时TLS视为真实通道权限或真实资金联调。
