# 支付宝银行卡转账协议与资金状态

## 依据

本机 `epay_release/plugins/alipay/alipay_plugin.php:861` 与 `epay_pro/vendor/cccyun/alipay-sdk/src/AlipayTransferService.php:85` 使用 `alipay.fund.trans.uni.transfer`，产品 `TRANS_BANKCARD_NO_PWD`、场景 `DIRECT_TRANSFER`、收款身份 `BANKCARD_ACCOUNT`，`bankcard_ext_info.account_type=2`（对私）。不传微信银行编码。

官方 [Participant 模型](https://github.com/alipay/alipay-sdk-java-all/blob/master/v2/src/main/java/com/alipay/api/domain/Participant.java) 和 [BankcardExtInfo 模型](https://github.com/alipay/alipay-sdk-java-all/blob/master/v2/src/main/java/com/alipay/api/domain/BankcardExtInfo.java) 支持上述收款结构。官方 [转账查询 API](https://github.com/alipay/alipay-sdk-php-all/blob/master/v3/docs/Api/AlipayFundTransCommonApi.md) 包含银行卡产品，按平台业务号查询时必须带原产品和场景。

官方 [统一转账响应模型](https://github.com/alipay/alipay-sdk-java-all/blob/master/v2/src/main/java/com/alipay/api/response/AlipayFundTransUniTransferResponse.java) 列出 SUCCESS、FAIL、DEALING、REFUND；同步金额 amount 通常不返回。[查询响应模型](https://github.com/alipay/alipay-sdk-java-all/blob/master/v2/src/main/java/com/alipay/api/response/AlipayFundTransCommonQueryResponse.java) 的 order_fee 是预计收费，不是已结算费用；trans_amount 只承诺成功状态返回。查询没有银行卡号、姓名或产品回显。查询晚退票必须保留，不能套用微信 BANK_FAIL 或 cmms_amt。

截至本次核对，公开模式的此银行卡产品授权条件没有找到足够的官方说明。实现只开放已验证证书模式和显式 enable_bank_transfer=true 的通道；这是一条实现能力限制，不声称官方禁止公钥模式。

## 设计

- 默认关闭银行卡能力；启用仍要求全局/商户代付授权、部署写开关、完整有效证书和场景配置。银行卡号12–32位数字、真实姓名、对私账号、无需银行编码；本地拒绝发生在预占前。
- 原收款快照加密、请求HMAC和产品在预占事务中固定。平台全局 TF{id} 不变。31给银行生命周期增加不可变 provider_kind，并新增支付宝请求绑定及不可变观察表，保留微信已有专用处理。
- 同步响应通过现有证书验签、精确 TF 和上游 order_id 后，只记录原提交与收款/产品绑定；不凭缺省金额宣告到账。丢失此绑定时，后续查询不足以重建银行卡身份，保持未知和冻结，不重发。
- SUCCESS 的结转必须有已验证原提交绑定，并且查询的 TF、上游 order_id、实际金额精确匹配；仍持续每6小时只读复查。本金转入清算，平台服务费单独保留。预计 order_fee 仅展示，不记实际费用账。
- FAIL / REFUND 只有同样完整金额及身份/产品绑定证据才能解冻或做幂等反向凭证。缺金额、身份矛盾、产品矛盾、成功后普通FAIL均进入 reversal_pending，不自动补钱，不冒充成功。REFUND 的观察不可被后来的 SUCCESS 覆盖。
- 认证失败、不确定响应、NOT_FOUND 均保留预占。现有 T+1、服务费、操作者审计和持久只读对账任务沿用。真实通道、真实银行卡、生产资金均不调用。

官方通常不在 FAIL/REFUND 返回金额，因此实际这些响应通常需要人工核对。没有额外、可验证的上游账单证据时，不能声称自动退票闭环已完成。

## 配置与接口

通道仍使用 `plugin=alipay`、证书 `sign_type=1`。额外配置为 `enable_bank_transfer="true"`、`bank_transfer_scene` 及 `bank_transfer_scene_reports`（JSON数组，元素是 `info_type` / `info_content`）。没有这些配置时不广告银行卡写入能力。关闭写开关后，保留原证书可继续查询已存在的银行卡意图；公钥模式无法查询本实现的原银行卡意图。

后台代付能力中的每个通道额外返回 `bank_product`（`alipay_bankcard` 或 `wxpay_v2`）及 `bank_code_required`。支付宝银行卡不填银行编码，并固定为对私账户。原创建、详情及查询接口无需新增路径，仍使用 `type=bank`、原本金/平台服务费和租户权限。

公开返回 `bank_estimated_fee_amount` 为元字符串，详情对象同名字段为整数分字符串；`bank_estimated_fee_known` 指出是否有估算。它与实际 `bank_fee_amount` / `bank_fee_known` 分开，预计值从不冒充实际已扣或已退费用。`bank_product` 同时返回在记录详情及公开转账结果中。

已成功记录遇到验签、业务号或金额认证失败时，会持久记录未验证观察并转为 `reversal_pending`，不再把旧成功当成刚查询确认的成功；无余额变化。公开查询返回 `status=0`、`query_required=true`，并保留 `ledger_status=succeeded` 历史状态。数据库观察写入失败时，退票反向分录一并回滚。

## 本地验证

- `TestAlipayBankCertificateCapabilityAndAuthenticatedEvidence`：临时CA/应用/平台证书和TLS假上游；原请求签名及对私银行卡产品，缺额同步受理、查询/退票、错误TF/金额、公钥限制、关闭写能力后继续查询。
- `TestAlipayBankFactoryExplicitCertificateProductAndWriteGuard`：离线配置能力、证书模式、部署写开关。
- 三个 `internal/payment` 的 `TestAlipayBank...`：真实PG、原提交丢失、未知资金保留、并发退票幂等、费用分离、观察失败原子回滚、产品串账门禁。
- `TestAlipayBankCertificateHTTPActualAdapterLateRefund`：实际适配器+临时证书TLS+PG+RSA公开API及后台能力，确认成功后错误金额/缺额退票均不能继续报告成功。

这些测试不表示生产商户产品授权已开通，也不表示通常缺少金额/身份回显的退票可以自动处理。

## 后续可验证回传增强（31未启用）

官方统一转账请求和查询响应模型均列有 `passback_params`。后续可在首次意图内生成不可猜回传令牌或绑定摘要，加密保存后随原请求发送，查询严格验证回显，从而在原ACK丢失时增强原收款/产品请求证据。31仍维持上述保守行为，没有发送或依赖此参数；不能给历史无令牌意图补造证据，缺绑定仍待人工核对。
