# 微信、支付宝两个 Pro 插件的方法映射

2026-10-25状态补充：当前本地固定98/schema83已完成完整双平台、两通道模拟、实际浏览器/额外业务交互及200表恢复验收；以[98集成记录](../verification/integrated-candidate-98-2026-10-25.md)为准。本文保留早期方法盘点，后续已加入78独立付款码恢复、79微信V2直连单次分账、75预授权原剩余解除、66支付宝应用授权、67微信URL Link及81原生客户端控制器；这些增量不能由下文旧“待做”字样误判为完全未实现，也不能扩大为服务商/电商或全部加密语义已兼容。所有外部执行默认关闭，真实产品权限未验证。

来源为 `reference/server-pro/plugins/payment/wxpay/WxpayPlugin.php`（v1.2）、`alipay/AlipayPlugin.php`（v1.4）以及本地 Pro `vendor/cccyun` SDK。表内“实现”表示有本地模拟协议测试，不能解释为生产联调或实际通道权限已验证。提供的原配置产品仅微信 Native/JSAPI、支付宝 page/wap；新增产品需要显式配置，不会自动改变原通道。测试部署只用 simulation，不读取真实密钥；同一插件的其他方法仍单独列出，不能以“其他插件延后”掩盖缺口。

| 插件来源方法 | Go 对应/状态 | 边界 |
|---|---|---|
| 微信 submit/mapi/qrcode | Provider.Create(native) 与 HTTP 收银台 | 显式商户通道白名单，HMAC 到期 token，二维码 PNG |
| 微信 jspay | Provider.Create(jsapi) | 完整浏览器二次签名参数；OpenID 来自商户签名请求或显式开启的同 App 网页 OAuth，不能接受未绑定身份 |
| 微信 wap | 可返回受保护收银台 | 原微信内网页/OAuth 跳转细节尚未完整复刻 |
| 微信 h5 | Provider.Create(h5)，显式 apptype=3 | 必须配置可信 `h5_site_url`（HTTPS）、`h5_site_name`；MWEB、scene_info、已签名平台回跳 URL；不能用商户任意场景 JSON 替代配置 |
| 微信 apppay | Provider.Create(app)，显式 apptype=5 | 完整 appid/partnerid/prepayid/package/noncestr/timestamp/sign 二次 MD5 签名 JSON，原 PHP SDK 双向互操作测试；APP 权限和应用注册仍需实际开通 |
| 微信 wxminipay | 同 AppID JSAPI 参数与独立小程序 code 完成流程 | 原签名 `method=jsapi,is_applet=1,sub_openid=...` 仍可用；新启动上下文通过专用 code→jscode2session 取得可信 OpenID，不能用网页 OAuth state 或请求参数冒充身份，不允许替换 AppID |
| 微信 applet | 显式 enable_mini_launch 的 wxapp 启动材料 | appId/miniProgramId/path 来自可信通道；money 与一次性完成地址进入原 money/url 页面字段；需要同 App 支付与 mini_secret、可信 mini_path。尚不提供独立 wxplugin 产品或跨 App 授权 |
| 微信 scanpay | 显式 enable_scan_pay=true 的 micropay、只读核对及78独立有界恢复 | mapi method=scan；付款码只保留HMAC，稳定平台订单号仅发送一次；首次支付路径不直接reverse，恢复须独立持久来源/查询链/单次撤销claim，默认关闭 |
| 微信 return/notify | HTTP 受保护同步返回/可信异步回调 | 同步返回先查证，已支付后签名跳转保存的商户 URL；回调提交账务后 ACK |
| 微信 query/refund/close | Provider.Query/Refund/QueryRefund/Close | 退款采用平台 RF 编号，未知不重复发送，晚到成功允许核对入账 |
| 微信 transfer/transfer_query（零钱） | TransferProvider.Transfer/QueryTransfer | 稳定 TF 编号，mTLS；查询绑定 AppID/商户号/OpenID/金额 |
| 微信 transfer/transfer_query（银行卡） | 已实现显式 RSA 公钥+mTLS 的银行分支及独立退票账务 | `enable_bank_transfer=true`、可信 `bank_public_key` PEM（或 Certificates.publickey_path）和 mTLS 同时具备才暴露 bank；显式银行编号；卡号/户名 OAEP-SHA1 加密，查询匹配金额、卡号/姓名摘要；受理只进入处理中 |
| 支付宝 submit/mapi/pagePay/submitpc/submitwap | Provider.Create(page/wap) 与 HTTP 收银台 | RSA2 转义 POST 表单；扩展业务身份参数未全量复刻 |
| 支付宝 qrcodepc/qrcode | 显式 apptype=3 映射 Provider.Create(native) | 已有 precreate 验签、notify_url 顶层字段、HTTP 收银台/二维码测试；当前已导入配置仍只启用 page/wap；插件内部其他二维码策略尚未全部映射 |
| 支付宝 apppay | Provider.Create(app)，显式 apptype=6 | 生成 RSA2 签名 SDK 查询字符串，不在材料生成时访问上游；原 PHP sdkExecute 双向签名互操作测试 |
| 支付宝 jspay | Provider.Create(jsapi)，显式 apptype=4 | trade.create，签名传入可信 buyer_id/buyer_open_id，生成的支付宝 trade_no 是待支付材料，不直接入账 |
| 支付宝 jsapipay/alipaymini（JSAPI_PAY） | Provider.Create(mini)，显式 apptype=7 | 公共 method=jsapi,is_applet=1 或独立小程序 code 完成流程；op_app_id 绑定可信通道 AppID，payer 来自签名参数或固定 TLS、精确 RSA/证书验签的 token 交换；V2 pay_type=jsapi、pay_info 为交易号纯字符串，trade.create 不入账 |
| 支付宝 minipay | 显式 enable_mini_launch 的 urlscheme 启动材料 | 新 method=applet 准备入口生成 alipays startapp，绑定可信同 AppID/path 与一次性 POST 完成地址；原 PHP 的 money/url/page 编码已离线互操作。原 GET code 页面客户端需要适配新 POST 契约，不假称旧程序无需修改 |
| 支付宝 scanpay | 显式 enable_scan_pay=true 的bar_code trade.pay、只读核对及78独立有界恢复 | 仅条码付款码；公钥/证书模式共用精确节点验签；10003/20000等未确定结果保留原订单查证，独立撤销沿用原号、不可变来源和入账屏障，默认关闭 |
| 支付宝 preauth/preauthnotify（SDK/券二维码授权） | apptype=5/10 的 app.freeze SDK，apptype=9 的 voucher.create 二维码专用增量 | 源码 5/9 是同插件预授权产品，不是协议签约 addon；与普通支付分流，材料和冻结成功不直接入账；9 的单次创建绑定原授权意图，未知不重新创建；授权回调与后续 capture 分开，未知 capture 只查询不自动重发 |
| 支付宝预授权释放/核对 | 专用 UA 编号的全额未消费授权释放流程 | 原授权与扣款状态先分别查证；释放意图阻止新的自动 capture；未知 unfreeze 不重发，authorization.release.reconcile 仅查询原 UA 操作，缺少显式零余量不当成功；买家解冻不会贷记商户余额 |
| 支付宝 signnotify（安全发签约） | 依赖 alipaysatf addon 的分支暂缓 | 源码仅在 FUND_SAFT_SIGN_WITHHOLDING_P/NORMAL 且 addon 启用时转发 signNotify；不能当普通支付成功通知 |
| 支付宝 appgw（投诉变更事件） | 独立投诉业务的外部刷新尚未接入 | 原码 alipay.merchant.tradecomplain.changed 与 alipay.security.risk.complaints.merchants.notify 转交投诉模型；本地管理记录不等于已完成厂商投诉查询 |
| 支付宝 appgw（安全发/批量/代进件事件） | 明确依赖其它 addon，按用户范围暂缓 | AlipayPlugin.php:1143 起的资金单据、批次、授权、KYB 分别转交 alipaysatf/alipayfund；不把这些 addon 缺口计作普通收款已完成，也不对未处理资金事件 ACK 假成功 |
| 支付宝 notify/return/query/refund/close | Provider 与 HTTP 同名流程 | 验证精确响应节点 RSA2；TRADE_SUCCESS/FINISHED 统一后仍以平台订单幂等 |
| 支付宝 transfer（公钥模式支付宝账号） | TransferProvider.Transfer | `alipay.fund.trans.toaccount.transfer`；账号与 UID 类型按本地插件规则 |
| 支付宝 transfer_query（公钥模式） | TransferProvider.QueryTransfer | `alipay.fund.trans.common.query`，稳定 TF 编号与金额校验 |
| 支付宝证书模式（收款/回调/查询/退款） | 显式 sign_type=1 三证书模式 | 应用私钥与证书匹配，离线信任链/有效期校验；app_cert_sn/alipay_root_cert_sn 参与签名；平台响应 alipay_cert_sn 必须命中显式配置且有效的证书，未知证书不下载、不回退公钥 |
| 支付宝 transfer（证书模式支付宝账号） | TransferProvider.Transfer/QueryTransfer | alipay.fund.trans.uni.transfer；UID/账号/OpenID 明确区分，签名响应保持处理中/未知语义，后续查询仍绑定 TF 编号和金额；权限与合规场景申报仍需真实商户明确配置 |
| 支付宝 transfer（银行卡） | 独立证书银行产品与退票增量，31 最终验收中 | 仅完整证书、显式 enable_bank_transfer、可信产品/场景报告才暴露 bank；不使用微信 bank_code/BANK_FAIL/cmms_amt。order_fee 仅预计展示；丢失原始提交绑定或缺少完整实际金额的 REFUND 进入 reversal_pending 且不补钱，完整匹配证据才反向一次 |
| 支付宝 transfer_proof | TransferProofProvider.ApplyTransferProof/QueryTransferProof | 先持久化申请意图/文件编号再查询；申请受外部写开关限制 |
| 支付宝 balance_query | ChannelBalanceProvider.ChannelBalance | 账户查询与额度查询；仅通道余额，不能替代商户账本余额 |
| 转账 API bookid（支付宝安全发） | 明确未支持 | 显式拒绝，不使用普通 uni.transfer 伪装安全发账本产品 |

## 运行与验证

V2 `method=app/jsapi` 返回真实 `pay_type=app/jsapi` 和字符串类型 `pay_info`；web/native 保留真实 html/qrcode 材料。显式 `method=jump` 始终返回保护收银台URL，即使通道仅支持APP、JSAPI或小程序。微信 JSAPI 为签名 JSON 字符串，支付宝 JSAPI 为原插件要求的 trade_no 纯字符串，不能混为同一个 JSON 结构。传统 mapi 的响应仍只提供 payurl，受保护收银台区分微信/支付宝 Bridge 与 APP 拉起链接；H5 返回 jump。已付页面只展示结果，不再提供原支付材料，已付二维码请求返回410。签名请求的 clientip 仅接受 IP 字面量；缺失时使用实际连接来源，不信任转发头。

适配器 `ValidateCreate` 与通道工厂在请求外发前检查本地前提。ProviderFlow 在创建 durable attempt 前调用验证器，缺 OpenID/非法 IP 不会产生 unknown 尝试或核对任务。H5 缺少可信站点配置时工厂启动校验直接失败；新增产品不会在默认 methods 或原有已导入 apptype=1/2 中自动启用。

支付 API、聚合收款和余额充值已接入 [34 通道策略路由](provider-policy-routing.md)：仅在运行时已加载、商户有权使用且满足原请求产品/身份前提的同类型通道中选择。四策略、金额/UTC+8时段/数量额度与订单持久绑定在 PostgreSQL 中执行；unknown 不改选另一上游。聚合收款/充值入口使用调用端固定的浏览器产品过滤，不能因为轮询选到预授权或小程序而绕过其独立生命周期。配额按首次申请保守累计，失败不返还，不能当作成功收款统计。

小程序启动另须显式 `enable_mini_launch=true`、可信 `mini_path`，可选 `mini_appid` 只能等于付款通道 AppID；微信还可配置 `mini_program_id`，身份交换使用专用 `mini_secret`。网页和小程序授权客户端都受应用级 OAuth 开关控制，启动还要求支付写开关及当前商户通道许可；默认 simulation 没有这些客户端。材料生成前先检查所有本地前提，拒绝 caller sub_openid、跨 AppID、与网页 oauth 混用；不会先建立未知支付尝试再报本地配置错误。

新 `method=applet` 的 V2 返回 `wxapp` JSON 或支付宝 `urlscheme` 字符串，V1 只返回受 HMAC token 保护的 payurl；页面不伪造微信 scheme 或小程序插件权限。原 money/url/path 的 PHP 编码保持一致，url 改为本平台固定 HTTPS `/oauth/mini/complete/{provider}/{channel}/{trade}/?state=...`。小程序客户端必须调用钱包 SDK 取得一次性 code，再向该地址 POST `application/json` 的 `{"code":"..."}`；可附 state 但必须与 query 完全一致。重复键、额外身份字段、尾随 JSON 或其他上下文 state 均拒绝，不能继续使用原 GET code 页面假定已兼容。

每个小程序 state 有效期五分钟，同订单最多八个未消费有效 state；辅助 return/QR 路径不会签发 state。state/code 在交换前提交消费，停用商户、撤销通道或关闭写开关后也不能通过恢复配置复活原 state；网页与 mini 共用 code 去重，不共用浏览器 cookie。多个不同合法 state 的身份交换与真正支付尝试分别控制：原始 code/token/session_key 不持久化，实际支付仍经不可变买家身份 HMAC 和单次 provider attempt，未知只查询。启动、code 交换和 prepay/trade.create 都不代表付款成功，最终入账仍须已验签 Query/通知证明，且和晚到重复通知共用一次账务。

所有自动化测试使用临时密钥、httptest 和本地 PostgreSQL，不使用真实配置。配置模式必须指定加密文件，工厂的写开关默认关闭；worker 永远 `Build(false)`。独立 worker 仅在 `GOEPAY_PROVIDER_MODE=configured` 且 `GOEPAY_ENABLE_PROVIDER_QUERIES=true` 时领取已配置通道的核对任务。默认或 simulation 模式只处理商户通知任务，避免跨进程重建本地模拟通道内存状态。

支付核对任务在通道尝试和订单 submitting 状态的同一事务中写入，延迟 30 秒运行，可覆盖请求进程中断。只读核对可以确认支付、退款、转账结果，不会重新发起任何资金写请求。达到重试上限后保留未知状态和预占，进入人工复核；管理员重试也仍是查询，不是重新出款。

微信官方查询文档明确指出 NOT_FOUND 可能仍在处理中，不应据此直接释放转账预占。[官方查询说明](https://pay.wechatpay.cn/doc/v2/merchant/4011989767)

银行转账的线协议编号将平台 `TF{id}` 映射为 `TF` 加 20 位补零数字，满足官方 8–32 位要求，外层编号、加密收款资料、请求 hash 和日额度收款人策略保持原值。没有自动获取公钥或根据卡号访问外部 BIN 服务；银行编号采用提供的插件映射并在调用前校验。银行 `result_code=SUCCESS` 只代表受理，必须查询到 `status=SUCCESS` 才结转本金。[银行提交接口](https://pay.wechatpay.cn/doc/v2/merchant/4011939840)

官方银行接口允许付款 SUCCESS 后变为 BANK_FAIL。平台单独记录银行生命周期、实际银行手续费和商户服务费；服务费暂存，匹配原付款流水、金额和手续费的可信退票只返还一次，缺少或矛盾证明进入 reversal_pending，不能直接把已结转成功的转账改成普通失败。`transfer.bank_reconcile` 每六小时持续只读核对，下一任务先持久化再 ACK 当前任务；排程失败不提前查询/ACK，最终返还或失败后停止继续排程。该核对没有凭空假设一个银行最终性期限。[银行查询及退票状态](https://pay.wechatpay.cn/doc/v2/merchant/4011941066)

银行查询已验证商户号、平台编号、金额和收款人摘要后，即使 BANK_FAIL 缺少或带有非法手续费，也必须保留可信退票观察，进入 reversal_pending 且不补钱；之后的旧 SUCCESS 不能抹掉该观察。身份失配仍直接拒绝，不将伪造退票交给账务。真实 WechatV2→本地 HTTPS→PostgreSQL→商户 HTTP 回归覆盖这一区别。

支付宝证书模式的 `Certificates.app_cert_path/alipay_cert_path/root_cert_path` 存放 PEM 内容，不是待读取的任意路径或 URL。仅显式 sign_type=1 启用；应用证书必须匹配私钥，平台证书可显式配置轮换 bundle。证书 SN 使用逆序 issuer 与任意精度十进制 serial，根 SN 保持原 SDK 的 RSA SHA1/SHA256 筛选与文件顺序。原 PHP 8.2 helper 的 80-bit serial 与请求签名互操作已有离线测试。证书过期会在创建外部意图前拒绝，不接受未配置的响应序列号；旧异步通知没有 SN 时仍须由已 pin 的有效平台证书验签。应用/平台证书链、到期和错误私钥均有拒绝回归；未实现联网证书下载或吊销查询。[官方 PHP 证书 SDK](https://github.com/alipay/alipay-sdk-php-all/blob/master/v2/aop/AopCertClient.php)

预授权释放核对任务同样按不可变 provider_attempts.channel_id 过滤，仅配置模式的查询开关启用时领取。超时、原 UA 操作不匹配或余量证明缺失都会保留任务重试；不会调用 Freeze/Capture/Unfreeze/Cancel，也不会把授权额度作为平台可用余额。

付款码支付仅通过显式 `enable_scan_pay=true` 开启，不新增或猜测原插件 apptype 编号。`method=scan` 的原参数先验签，再复制参数并按 auth_code 前缀推导缺省 type；显式 type 冲突会在创建订单前拒绝。当前仅接受微信 18 位 10–15 前缀和支付宝 16–24 位 25–30 前缀的数字条码，不支持刷脸或声波 token。原付款码只在请求内用于一次提交，数据库仅保存带通道、方法和可信 AppID 的 HMAC 指纹；不同付款码不能复用旧订单成功结果。[微信 V2 付款码协议](https://pay.wechatpay.cn/doc/v2/merchant/4011937125)、[支付宝条码交易参数](https://jaq-doc.alibaba.com/docs/doc.htm?articleId=850&docType=4&treeId=180)

V2 仅在已验签且账务提交后返回 pay_type=scan，pay_info 为 type/trade_no/api_trade_no/buyer/money 的 JSON 字符串；未知结果返回原 trade_no 供查询，不输出假的扫码成功。V1 保持 release `Payment::echoJson` 的 legacy fallback：code=1/trade_no/payurl，付款后的受保护结果页不再生成支付材料。查询从已验签结果保存真实 buyer，后到回调与查询共用一次账务；明确非 CNY 的结果不能按 CNY 入账。原 PHP SDK 的扫码请求签名互操作、实际两家 httptest→PostgreSQL→HTTP 的正常/未知/重复/晚回调均有回归。

以下为早期付款码撤销设计约束；后续78已实施独立持久来源、六次有界查询链、永久撤销claim与晚成功入账屏障，并随93及后续候选完整验收。原PHP轮询超时后直接reverse/cancel没有照搬：撤销可能退回已经成功扣取的资金，不能作为普通Close。超时、查单NOT_FOUND或网络错误均不足以直接退款或释放已结转款；恢复执行默认关闭，所有未知结果保留原业务号与原claim，不因重试或更换配置再次发送。当前测试证据见固定98集成记录及其完整后端证据。

## 后续同插件分支的实施条件

这些分支仍属于当前两个插件，不能归入“其他插件暂缓”。建议分别建立回归测试和独立增量，避免把不同资金语义并入普通支付状态。

| 分支 | 实现复杂度 | 可离线验证内容 | 必须明确的前提 |
|---|---|---|---|
| 微信银行卡付款 | 协议与独立退票账务增量已实现 | RSA-OAEP 加密、银行编号、mTLS、请求签名、查询金额/卡号姓名摘要、TF 编号、持续查询及冻结款/退票事务有离线回归 | 显式可信银行加密公钥、银行编号及存量产品权限；不自动获取/信任未知公钥；银行受理不等于最终到账 |
| 支付宝证书模式 | 已完成收款及支付宝账号 uni-transfer 增量；银行卡另立增量 | 三证书解析、根链/有效期、SN 与原 PHP SDK 互操作、签名请求/精确节点验签、真实 PG 查询入账、金额/流水幂等有回归 | 显式完整 PEM 证书、匹配私钥和签约产品；未知序列号失败关闭、不联网下载、不回退公钥；特殊重复/未知 issuer 属性保守拒绝 |
| 微信/支付宝付款码 | 已完成一次扣款/查询增量；撤销另立资金增量 | 条码参数、签名、HMAC 身份绑定、单次发送、未知只读核对、并发与晚回调账务幂等有实际 PG/SDK 回归 | 短期付款码不持久化原文；不自动 reverse/cancel；撤销必须有独立意图、预占/冲正和晚付一致性模型 |
| 支付宝 JSAPI/小程序 | 已完成同 AppID 的协议增量；跨应用授权另行实现 | trade.create 与客户端交易号材料、buyer 标识绑定、可信回调/查单已离线测试 | AppID 与 buyer 标识必须来自可信配置和已授权身份；不能从 user_id/code 无条件伪造授权 |
| 微信/支付宝 OAuth | 已完成显式同 App 的网页与小程序独立增量 | 固定 TLS code 交换、网页浏览器绑定、mini 独立5分钟 state、跨两种 state 的 code 全局去重、不可变订单/App/路径/方法绑定与停用后消费有 PG 回归；token/session_key 不返回、不持久化 | 网页和小程序 state 不通用，可信付款身份由签约应用响应证明；实际用户授权、应用权限和设备启动未做生产联调，不能读取真实配置来代替 |
| 支付宝预授权 | 5/10 SDK 授权与 9 券二维码使用独立授权/一次 capture/释放与只读核对流程 | 授权、支付、释放分别持久化，未知不重复发资金请求，释放原 UA 查询与零余量证明有 PG 回归 | 授权成功不是支付入账；5/9 产品映射依据原插件 preauth/qrcode，不能误归类为协议签约或安全发 addon |
| 小程序插件跳转、APP 网关 | 中 | 签名启动参数、路径/场景材料和模拟回调 | 需绑定的小程序原始 ID、插件供应商及授权配置；跳转成功不得当作已支付 |

以上离线测试可以推进实现和资金不变量验证；不能证明真实商户已开通产品、设备 SDK 实际调起成功或生产联调已完成。
