# 微信/支付宝渠道分账证据与实施顺序

此能力属于既有两支付通道，不归入其它插件暂缓。站内账本分账不能代替远端分账。原管理员/用户receiver、order四个菜单仍pending。

本地证据：Pro vendor/cccyun/wechatpay-sdk/src/V3/ProfitsharingService.php：添加27、删除54、提交72、查询90、完结110、可分余额128、回退140/154；alipay-sdk/src/AlipaySettleService.php：绑定29、解绑55、查询80、提交102、完结129、查询148、比例161、回退179。Pro ProfitSharingService.php367/380/393/406有动作检查；核心Swoole逻辑加密，不能根据Release推断全部Pro语义。

Release Wxpay查询只看第一接收方、退回对任意成功判断不够，BaseProfitSharing默认接收方成功亦不可复制。实现必须核对每个接收方的类型、账户、金额、独立状态及原单绑定；FINISHED只表示执行结束，不表示全部接收方成功。未知、部分成功、完结和回退均有不可变原号，不生成新号重发。

微信：POST /v3/profitsharing/receivers/add|delete；POST orders；GET orders/{out_order_no}?transaction_id；POST orders/unfreeze异步完结；POST return-orders、GET return-orders/{out_return_no}?out_order_no。服务商/ecommerce独立产品，不直接替换路径。支付下单须有settle_info.profit_sharing=true声明；当前V2支付与V3分账组合未获证据，应先实现直连V3支付与不可变分账声明。

支付宝：alipay.trade.royalty.relation.bind|unbind；order.settle包含trade_no/out_request_no/royalty_parameters/royalty_finish；settle.query可使用原请求号和原交易号核对遗失回执；refund退款0元并传refund_royalty_parameters用于明确产品的分账回退，必须核对后续refund.query明细。不同支付产品普通退款有provider_proportional与explicit模式，不能统一先手工退分账再普通退款，避免重复资金返还。

每个支付订单必须保存原收款账户摘要、产品/声明、规则版本、退款分账模式与完整接收方快照。远端接收方变更、提交、完结、回退先持久化稳定业务号与审计再I/O；未知只查询原号。订单锁协调普通退款，禁止两个不同流程同时处理同一分账资金。

实施顺序：48直连V3支付传输/验签/解密及支付退款闭环；49两通道分账适配器与临时TLS；50不可变规则与声明快照/持久操作及锁；51双后台与逐动作授权、部分失败/回退/并发/故障测试；不得用资料CRUD代替远端闭环。

官方依据（2026-10-23核对）：[V3签名总述](https://pay.wechatpay.cn/doc/v3/merchant/4012365342)、[平台验签](https://pay.wechatpay.cn/doc/v3/merchant/4013053420)、[分账声明](https://pay.wechatpay.cn/doc/v3/merchant/4014547102)、[接收方结果](https://pay.wechatpay.cn/doc/v3/merchant/4012524936)、[异步完结](https://pay.wechatpay.cn/doc/v3/merchant/4012526374)。支付宝参考官方alipay-sdk-php-all/v3的ExtendParams、AlipayTradeRefundModel及OrderSettleApi，需实施时重新核对原始文档。

追加核对：支付宝关系请求号最多32字符且仅字母数字下划线，绑定/解绑没有seller_id，不能把配置SellerID当成经过该响应验真的主体；查询out_request_no属于本次查询，不是原绑定号检索。官方[BindModel](https://raw.githubusercontent.com/alipay/alipay-sdk-php-all/master/v3/src/Model/AlipayTradeRoyaltyRelationBindModel.php)、[BatchqueryModel](https://raw.githubusercontent.com/alipay/alipay-sdk-php-all/master/v3/src/Model/AlipayTradeRoyaltyRelationBatchqueryModel.php)、[响应模型](https://raw.githubusercontent.com/alipay/alipay-sdk-php-all/master/v3/src/Model/AlipayTradeRoyaltyRelationBatchqueryResponseModel.php)。同PID可有多AppID，但未证实各App关系的隔离边界，保持保守限制。

支付宝剩余可分金额可按官方[onsettle请求](https://raw.githubusercontent.com/alipay/alipay-sdk-java-all/master/v2/src/main/java/com/alipay/api/domain/AlipayTradeOrderOnsettleQueryModel.java)/[响应](https://raw.githubusercontent.com/alipay/alipay-sdk-java-all/master/v2/src/main/java/com/alipay/api/response/AlipayTradeOrderOnsettleQueryResponse.java)实现：trade_no、可选royalty_source，返回unsettled_amount。该值0不等于已释放冻结，更不证明某个独立PF终态；不得自行映射。[SettleExtendParams](https://raw.githubusercontent.com/alipay/alipay-sdk-java-all/master/v2/src/main/java/com/alipay/api/domain/SettleExtendParams.java)支持原PS显式royalty_finish=true的处理后解冻约定，但独立PF的空分账明细仍不能作为终态证据。

追加协议修正（2026-10-23）：官方 [AlipayTradeOrderSettleQueryResponse](https://raw.githubusercontent.com/alipay/alipay-sdk-java-all/master/v2/src/main/java/com/alipay/api/response/AlipayTradeOrderSettleQueryResponse.java)只列 operation_dt、out_request_no、royalty_detail_list，没有 settle_no / trade_no；[SettleResponse](https://raw.githubusercontent.com/alipay/alipay-sdk-java-all/master/v2/src/main/java/com/alipay/api/response/AlipayTradeOrderSettleResponse.java)则包含受理号和交易号。应用不能要求查询返回不存在的字段。查询使用原不可变 trade_no / out_request_no 发起已签名请求，验签后核对原请求号、原接收方类型/账号/金额、所有逐项结果。若已有验签受理号则保留；若受理回执遗失，不杜撰该号码。微信仍必须核对其明确返回的原渠道分账单号。实际 TLS+PG 的正常受理及遗失受理两分支专项于6.642秒通过；完整集成证明尚待后续固定候选复跑。
