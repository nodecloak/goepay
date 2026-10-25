# 支付宝预授权增量

本增量只依据本地明文 `reference/server-pro/plugins/payment/alipay/AlipayPlugin.php` 的 `preauth/jsapipay/preauthnotify`、`epay_pro/app/lib/Payment.php` 的预授权查单/解冻分支，以及 `epay_pro/vendor/cccyun/alipay-sdk/src/AlipayTradeService.php`。没有推测加密 PHP 代码，没有读取 `.secrets`、真实密钥或访问生产。

显式 `apptype=5/10` 映射为 SDK `preauth`；9 使用授权二维码，见 [APP与券二维码](preauth-voucher.md)，默认 page/wap 不会自动启用。`alipay.fund.auth.order.app.freeze` 生成 RSA2 SDK 请求串，`out_order_no/out_request_no` 均为平台订单号；此时没有向上游发送冻结请求，买家仍需在支付宝确认。V2 返回 `pay_type=jsapi`、字符串 `pay_info`；收银台用 `tradePay` 的 `orderStr` 调起。原 `/pay/preauth/{trade}/` 只是带原有到期 HMAC 的收银台别名，知道订单号不能读取材料。

冻结通知走 `/pay/preauthnotify/{trade}/`，从本地不可变 attempt 获取通道。要求 RSA2 验签，精确绑定应用、配置的收款方、平台订单号/请求号、金额、`operation_type=FREEZE`、`status=SUCCESS`、授权号与买家标识；重复字段和未被原 AOP 规范签入的 `@...` 身份均拒绝。`payment_authorizations` 单独持久化授权号、买家、通道与金额，同一通道授权号不能绑定两个订单，身份不可修改。授权成功不生成账本分录、不增加平台商户余额，也不占用商户可用余额。

按明文 `preauthnotify` 的行为自动全额扣款，使用 `alipay.trade.pay`、`product_code=PREAUTH_PAY`、`auth_confirm_mode=COMPLETE`。外部调用前先提交唯一 capture 意图与延迟只读核对任务。并发回调只能领取一次发送权；失败、断连、响应验签失败、金额/买家/订单不符均保留 capture 结果，不可借重试回调再次发送。记录失败或未知后 ACK `success` 保留原插件的状态 4 语义；持久化失败不能 ACK。停写时仍允许接收可信授权证据，但不创建扣款发送意图。

只有确认成功的支付回执才进入统一 `PaidReceipt`/`markPaid`，将上游支付流水、账本与 capture 成功状态在同一事务保存。授权号不是支付流水。只读交易查询或正常支付异步通知也能确认原未知扣款，不产生新的扣款请求。冻结通知丢失时可用原始请求号查询授权并持久化证据；该恢复过程不能自动扣款。

已验证授权但尚无支付交易时，仅 RSA2 认证的精确 `40004:ACQ.TRADE_NOT_EXIST` 可以返回未支付的 `AUTHORIZED` 查询结果。该白名单不包含网络错误、验签失败或其他业务拒绝。授权查询失败也不能挡住另一条独立认证且金额/订单匹配的实际支付成功证据。

商户查单/列表：授权存在但订单未 paid 时返回 `status=4` 和授权号；真正 paid 后返回 `status=1` 和不可变可信支付流水，退款语义沿用普通支付。普通 close 对 preauth 明确不支持，不能用 `alipay.trade.close` 冒充买家资金解冻。已支付订单可以沿用普通退款。

迁移 25 已在严格适配器之上增加全额未扣款授权的持久解冻意图、capture 互斥、未知结果核对及受保护后台入口，见[授权解除](alipay-authorization-release.md)。撤销仍只有适配器，尚未开放独立持久业务入口；不能直接把适配器方法当成完整资金流程。也未实现部分扣款、多次扣款、授权额度调整、跨应用买家身份转换、真实设备/产品权限联调（网页OAuth27已独立实现，不能自动扩展成预授权特有身份策略）。查询与通知仅支持本地测试明确覆盖的字段画像；生产兼容性尚未验收。
