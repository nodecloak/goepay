# 微信V2原分账产品79

当前已固定92/schema78，所有1..78迁移与固定目录不得修改。本阶段只改主区，不将92的测试结果用于79。

本地原依据：`reference/server-pro/plugins/payment/wxpay/WxpayPlugin.php:138,203,272,324,349,385`在下单时按原profits声明profit_sharing=Y；`epay_release/includes/vendor/cccyun/wechatpay-sdk/src/ProfitsharingService.php`提供V2的接收方、单次分账、原号查询、完结、余款与回退。已有Go仅支持wxpay_v3_direct，不能把原V2分支归入其它业务插件暂缓。

官方复核：[单次分账](https://pay.wechatpay.cn/doc/v2/merchant/4011941670)、[查询](https://pay.wechatpay.cn/doc/v2/merchant/4011983179)、[接收方](https://pay.wechatpay.cn/doc/v2/merchant/4011983255)、[回退](https://pay.wechatpay.cn/doc/v2/merchant/4011984166)、[完结](https://pay.wechatpay.cn/doc/v2/merchant/4011984076)。V2分账使用HMAC-SHA256；发送分账、回退和完结须双向证书。单次分账会同时释放余款，FINISHED只是处理完成，必须逐接收方核实SUCCESS/CLOSED。回退仅限商户接收方。以上只读取公开文档，未调用支付接口。

本轮新增独立产品wxpay_v2_single_direct，默认不开启，仅直连账户。沿用已确认的单次产品及既有auto_finish=true政策，不把多次分账或子商户产品混入。新XML/HMAC交换器独立于原MD5支付/扫码/银行卡逻辑，真实TLS、签名、nonce、原商户/应用、订单及回显均校验；错误或超时不重试。查询不要求仍有可用客户端证书，但金融写前需纯校验证书和原输入。未知写仍仅查询原号。

复用原接收关系加密/所有权、规则、原付款声明、专属分账预留、单次claim、逐接收方观察、回退预算、完结及审计。新产品只在首次付款前绑定；历史V0/V1声明不补造，已有V3/支付宝MAC格式不变。扫码也按原政策声明分账；新ScanRequest字段为false时保持78请求摘要算法，true才追加分账维度，原付款码仍不落库。退款继续使用原merchant_funded来源，不发明加密Pro的手续费优先级。

新增79只扩展明确的产品约束及相应完结保护，不修改1..78，不重签或变更旧记录。资金释放仍须原订单、原账户、逐接收方终态与明确余款为零等来源证据；一份结果不能只凭FINISHED释放余额。未认证业务错误不能释放任何预留。独立支付写权限与管理员资金确认沿用现有规范。

验收按接收方/传输、单次结果/余款、回退/完结、持久产品与扫码声明四个可独立验证部分推进。每部分先实际失败再实现；临时密钥/本地TLS与mTLS、只读查询、坏签名/错身份/重复字段/金额及接收方错配、未知不重发、旧78迁移数据保持和财务损坏计数必须覆盖。最终仍需新固定候选双平台全量/race、PHP原签名互操作、双后台、独立本地无重置部署及全表恢复。主区79四部分已实现并通过阶段专项，扩大回归继续；尚未固定和发布，不能据此开启真实渠道。
