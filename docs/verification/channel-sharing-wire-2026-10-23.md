# 49 渠道分账协议层进行中

本机真实适配器经临时TLS测试，不调用生产，不加载真实密钥，不发真实资金交易。阶段50持久化/资金/双后台尚未实现；四个原渠道分账菜单保持pending，不能以wire测试算菜单完成。

微信：显式enable_channel_sharing；接收方姓名OAEP-SHA1（协议要求）加密并选精确serial；绑定/解绑；多接收方提交/查询；每项原账户/类型/金额/结果检查；FINISHED中部分CLOSED映射partial。独立回退按原order_id/out_order_no/out_return_no/receiver/amount核对。PF首次受理仅pending，独立查询原PF并核对本商户与预计解冻金额才确认。剩余金额接口字段缺失、null、负数、浮点、错原交易均拒绝；0是明确证据，不创建PF（官方NOT_ENOUGH表示无金额可解冻）。未知不重发、不随机生成业务号。

支付宝：显式enable_channel_sharing；稳定关系绑定/解绑；强制async提交，10000仅pending；原out_request_no+trade_no查询并核对全部royalty_detail_list，金额定点、类型/账号明确且不可只看第一人。手动PR另需enable_channel_sharing_return及sharing_return_evidence=refund_royaltys；初次0元退款只pending，查询须原交易/原回退号、0元买家退款、精确退分账明细和成功结果。明细存在产品范围限制，缺失时保留待核对，不假定普通产品都有。尚不宣称支付宝独立完结已具备可信终态，FinishSupported=false。

共用：配置工厂和guarded透传能力，但写入继续受allowLive控制。V2不默默启用V3分账。多个验签key时启用微信分账需明确v3_encryption_serial；单key可自动固定为该唯一key。启用接收方/分账能力不等于原支付单已声明分账；50必须先完成不可变原订单条款，不能给历史单补声明。

额外红测重现：支付宝有效签名JSON包含重复result_code时曾被后值覆盖为SUCCESS。已抽取两通道共享递归去重/深度限制解析器，Alipay.call在解释前拒绝；写入结果保留unknown。金额0原先被正数parseAmount拒绝，已单独允许规范0用于0元退分账。剩余金额null被Go解为int64零的问题经红测修复为必有非null整数。外部交易号不能通过../改变固定查询路径。

已执行：适配器全套5.418s、配置全套1.713s；本轮新增分账/重复JSON/剩余金额专项1.388s通过。之前本阶段支付账户/旧OAuth/临时TLS HTTP相关回归payment43.474s、contract19.549s及定向vet通过。固定49-wire-candidate-d0af950e-c847-490d-a0b8-da5b3f3f7244含591文件；断网Linux容器goepay-race-local-49-wire，协议race适配器6.773s、配置2.322s及全库vet exit0。50新增资料模型、接收方操作及下一48号SQL不属于此快照。

官方依据：微信[请求分账](https://pay.wechatpay.cn/doc/v3/merchant/4012524936)、[分账查询](https://pay.wechatpay.cn/doc/v3/merchant/4012525210)、[回退](https://pay.wechatpay.cn/doc/v3/merchant/4012525287)、[完结](https://pay.wechatpay.cn/doc/v3/merchant/4012526374)及官方Go SDK。支付宝官方SDK模型AlipayTradeRoyaltyRelationBindResponseModel（result_code）、AlipayTradeOrderSettleQueryResponseModel（out_request_no/royalty_detail_list）、RoyaltyDetail、RefundRoyaltyResult；[官方退款查询文档](https://aipay.alipay.com/docs/vibe-pay/ai-web-app-payment-qianyi/api-list/alipay-trade-fastpay-refund-query.html)明确10000不等于退款成功。
