# 独立标识获取下一阶段：本地证据与实施计划

研究日期：2026-10-23。位置：`E:\GOEPAY`。此文仅依据已存在的本地明文源码、SDK、路由、已构建静态页面与Go实现；没有执行PHP、解释Swoole加密控制器、读取`.secrets`或真实通道配置，也没有访问生产、发起外部授权、调用厂商接口或交易。本文中的协议名称表示本地SDK所实现的契约，不是当前厂商权限、签约资格或线上兼容性声明。

## 1. 结论与优先级

可以立即规划的最小完整增量是：在既有安全独立网页标识查询基础上，增加**真正独立的小程序标识查询**，并提供与支付产品分开的、明确获准的OAuth应用配置。它可复用微信`jscode2session`和支付宝`alipay.system.oauth.token`适配器，但不能调用当前小程序支付回调，因为该回调会继续`ProviderFlow.Submit`。

AppAuthToken有Release和共享SDK的直接明文证据，可以作为第二个独立用途实施；它是商家授予第三方应用的长期凭证，不能当用户OpenID、不能沿用普通标识结果类型，也不能自动写回支付通道。服务商/子商户和电商有共享SDK证据，但当前两个主插件的配置和Go适配器没有完整绑定这些账户模式。必须区分“SDK具备字段”“主插件已接入”“Go能安全启用”三层，不能只加下拉框宣称完成。

建议顺序：A独立mini+OAuth应用档案；B独立支付宝应用授权（普通/指定应用）；C在明确账户模式及配置来源后支持服务商应用与子应用的标识命名空间。电商不新增一种虚构OAuth协议；当前证据仅证明其分账产品分支，不能据此发明电商独立标识接口。用户已要求其它插件暂不做，本文不把`alipaysl/wxpayn/wxpaynp`、抖音等插件列入本次实现。

## 2. 原系统证据

| 来源 | 可直接证明的事实 | 不能据此推出 |
|---|---|---|
| `reference/server-pro/plugins/payment/wxpay/info.json`；`WxpayPlugin.php:20` | v1.2主插件：appid、appmchid、APIv2密钥和退款证书；产品1 Native、2 JSAPI、3 H5、5 APP；可绑定appwxmp/appwxa。getConfig只构造直接商户appid/mchid。 | 主插件已有服务商、电商或任意子商户配置。 |
| `reference/server-pro/plugins/payment/wxpay/WxpayPlugin.php:160,304,431` | 网页JSAPI按绑定公众号取得openid；小程序可用绑定appwxa；支付订单`sub_appid`可影响应用选择。 | 允许新的独立工具信任调用者提供的sub_openid/sub_appid；该旧支付参数不等于已验证用户身份。 |
| `reference/server-pro/plugins/payment/alipay/info.json`；`AlipayPlugin.php:20` | v1.4主插件：RSA2公钥/三证书、appid、应用私钥、可选卖家UID；产品1 page、2 wap、3扫码、4当面付JS、6 APP、7 JSAPI小程序、5/9/10预授权、8订单码。getConfig没有app_auth_token字段。 | token来自普通支付appid，或仅配置SellerID就等于ISV授权。 |
| `epay_pro/app/common.php:800,820,839,886` | 微信网页与小程序使用`wxinfo.appid/appsecret`；支付宝网页可切换全局`alipay_web_login`应用，小程序可切换`alipay_mini_login`应用。支付宝优先使用返回的user_id，否则open_id。 | 所有OAuth应用必须与收款AppID相同；原控制器返回JSON已被恢复。 |
| `epay_pro/app/lib/wechat/WechatOauth.php:41,70`；`vendor/cccyun/wechatpay-sdk/src/JsApiTool.php` | 网页：`/sns/oauth2/access_token`，scope snsapi_base；mini：`/sns/jscode2session`，js_code，返回openid/session_key。 | 小程序可通过公众号网页二维码直接得到openid；可复制原SDK中关闭TLS验证的行为。 |
| `epay_pro/app/lib/wechat/WechatAPI.php:19,54,72` | stable_token以client_credential/appid/secret换应用access_token；generatescheme返回openlink，generate_urllink以path/query/expire_time返回url_link；默认600秒。 | 加密generate_wxa_link控制器必然选其中哪一种、原路径/参数/期限已被恢复，或可以把access_token放明文配置。 |
| `epay_release/user/openid.php:25..` | 独立wechatid，或channel绑定公众号；支付宝auth_code调用getToken；app_auth_code调用getAppToken，展示AppAuthToken并记录auth_app_id/user_id；act app_auth和app_auth_assign有明确分支。 | 可照搬URL sid恢复会话、把token放PHP session/结果页或使回调自行登录。 |
| `epay_pro/vendor/cccyun/alipay-sdk/src/AlipayOauthService.php:18,30,56,93,118,145,165` | 普通用户授权/换token；普通应用授权appToAppAuth.htm；指定应用授权PC b.alipay.com与固定钱包scheme；`alipay.open.auth.token.app`换app_auth_token，`...app.query`查授权商家。OAuth构造器会移除传入app_auth_token。 | 普通`alipay.system.oauth.token`请求应携带AppAuthToken，或能用用户access_token代替商家授权凭证。 |
| `epay_pro/vendor/cccyun/alipay-sdk/src/AlipayService.php:54,104` | 一般业务服务可把appAuthToken写到AOP公共参数；公钥/证书是独立的签名配置维度。 | 两个主插件已把长期token接到了所有支付/转账接口。 |
| `epay_pro/vendor/cccyun/wechatpay-sdk/src/BaseService.php:57`、`PaymentService.php:23,93` | V2 SDK有sub_mchid/sub_appid字段，JSAPI桥应用优先subAppId。 | 同一个openid可跨sp_appid与sub_appid使用；配置字段就是授权应用身份。 |
| `epay_pro/vendor/cccyun/wechatpay-sdk/src/V3/PartnerPaymentService.php:26,44,64` | partner支付路径有sp_appid/sp_mchid/sub_appid/sub_mchid，桥使用subAppId或appId。 | 当前Go直连V3 adapter已经具备这些payer、响应验签和账户HMAC维度。 |
| `epay_pro/vendor/cccyun/wechatpay-sdk/src/V3/ProfitsharingService.php:29..`、`V3/BaseService.php:102` | ecommerce决定电商分账路径，包含receivers/orders/finish/return。 | 存在新的“电商OAuth code换openid”接口，或可通过本工具启用电商付款/分账。 |

`epay_pro/plugins/payment/`当前为空。上表Pro主插件来自既有已取回明文`reference/server-pro/...`，未重新连接服务器。`epay_pro/app/controller/user/Openid.php`是Swoole加密内容，只能确定不可直接读到控制器实现；本阶段不尝试解密。

### 原独立获取页面可见契约

`epay_pro/public/static/assets/index-BySTvbcv.js:1`（GetToken组件）明确存在“微信公众号Openid”“微信小程序Openid”“支付宝用户ID”“支付宝应用授权Token”页签（后者有“基础应用授权”与“指定应用授权”），并读取siteurl、wxmp_list、wxa_list、alipay_channels、alipaysl_channels、default_wx/default_wxa/default_alipay等选项。链接包含：

- `user/openid/wechat?wechatid=...`；另外原其它页面有`?channel=...`。
- `user/openid/alipay?channel=...`。
- `user/openid/alipay?act=app_auth&channel=...`与`act=app_auth_assign`。
- mini页按钮调用生成接口后只消费返回`url`；它没有证明url具体生成协议。

同目录`settings-mNzlkkmh.js:1`给出GET `./settings/getTokenOptions`和POST `./settings/generate_wxa_link`，body/params中是channel。`epay_pro/route/admin.php:51`确认这两个方法；`route/user.php:253`确认openid/wechat、alipay、qrcode、getopenid等GET路由（SessionInit）。只能记录这些路径与页面实际读取的字段，**不能编造加密控制器的完整JSON、二维码格式或mini后台实现**。

重要范围区别：该页面AppAuthToken选项读取的是`alipaysl_channels`。这不是“主插件alipay默认支持商家应用授权”的证据。共享Alipay SDK仍可用于新Go专用授权工具，但必须显式标为新实现，并单独配置ISV应用；不加载alipaysl插件。

## 3. 当前Go的准确能力

| 分支 | 代码事实 | 当前独立页状态 |
|---|---|---|
| 微信普通商户网页 | BuildOAuth显式enable_web_oauth=true、AppTypes含2、appid+oauth_appsecret；WechatOAuth固定官方网页URL与snsapi_base。付款可为V2或V3直连，readiness要求真实相同provider/AppID与账户scope。 | 已有，不应重复开发。 |
| 支付宝普通网页，RSA2公钥/证书 | BuildOAuth要求产品4；复用Alipay证书构造与精确响应节点验签；ExchangeOAuth签名top-level grant_type/code，不传biz_content，优先user_id再open_id。 | 已有；UI统一称“支付宝用户标识”，未持久化原返回字段类型。 |
| 微信mini | NewWechatMiniOAuth已有jscode2session验证，丢弃session_key；BuildMiniOAuth受enable_mini_launch、合法mini_path、产品2及mini AppID=付款AppID约束。 | 适配器有，独立查询未接。 |
| 支付宝mini | ExchangeMiniCode直接使用ExchangeOAuth；BuildMiniOAuth需产品7与相同AppID，借私有clone构造签名客户端。 | 适配器有，独立查询未接。 |
| 小程序支付完成 | `internal/server/oauth_mini_complete.go:68..`消费原mini-payment state，然后换code、校验风控，最后ProviderFlow.Submit。MiniLaunch还强制trade_no、正金额及固定支付complete URL。 | **不能供独立查询调用，也不能用0.01元假订单绕过。** |
| 分离公众号/mini OAuth应用 | 原Pro允许绑定/全局OAuth应用与付款AppID不同；Go oauthChannelReady/miniLaunchChannelReady强制相同，独立capabilities只有channel/provider/app_id。 | 未实现独立应用选择器与对应权限档案。 |
| 服务商/子商户 | WechatV2Config/WechatV3Config无sp/sub角色，账户scope仅provider/app/merchantID；AlipayConfig为AppID/SellerID，无AppAuthToken。 | 未实现；不能把未识别字段当成功启用。 |
| AppAuthToken | Alipay没有AppOAuthAuthorizationURL/getAppToken/appQuery专用适配器；当前OAuthIdentity仅Provider/AppID/SubjectID。 | 未实现，不能把token塞入SubjectID。 |
| ecommerce | 现有WechatV3为`/v3/pay/transactions/...`直连，未建电商支付/分账账户模式。 | 未实现，缺少独立OAuth协议证据。 |

主要路径：`internal/server/identity_lookup.go:109,127,160,276`、`internal/channelconfig/oauth.go:12`、`mini_oauth.go:14`、`internal/adapters/payment/oauth_web.go`、`alipay_oauth.go:20`、`oauth_mini.go:31`、`transfer_account.go:24`、`internal/payment/identity_lookup*.go`、`web/src/components/IdentityLookupPage.vue`。

保留现有安全成果：持久原始会话与auth_version校验、拒代登录、owner隔离、状态/浏览器/authorization token分离HMAC、五分钟TTL、session有效期上限、最多五个活动请求、交换前提交state/code消费、AES-GCM结果、单次reveal、无日志/URL/storage明文；付款接口完全独立。当前identityUnavailable还要求ProviderFlow.AllowLive，这意味着独立标识工具被付款写开关一并限制；这是配置耦合，后续应显式设计独立OAuth启用语义，不能默默改变旧安全默认。

额外风险：channelconfig.Config是map，factory未识别的service/sub/ecommerce/app_auth_token配置键可被忽略。下一阶段应对已知未支持账户模式显式拒绝或在能力中明确unavailable，不能把这些配置降级成直连商户后报告成功。只增加工具能力不自动扩大付款adapter能力。

## 4. 最小完整设计（待根任务批准后实施）

### A. 独立mini标识：优先落地

新增明确的`flow_kind=mini_identity`，与现有`web_identity`及`mini-payment`永久分离。旧网页API请求省略flow_kind时仍固定web，旧表/指纹/AAD继续校验原版本，不原地修改已冻结迁移。

运行时增加受信任OAuth应用档案，而非让前端提交app_secret/sub_appid：profile ID/版本、provider、app_kind（official_account/mini_program）、application_id、credential revision指纹、允许商户/原channel关系和启动页面。完整secret仅显式加密配置导入或已有加密凭据服务可见；列表只返回ID、类型、AppID与可用原因。首增量允许从当前已验证同AppID通道生成兼容档案；另外的公众号/mini应用必须有显式授权关系，不能因为全局存在就开放给全部商户。

独立mini交换客户端从启动资料分开构造：仅查询openid不需要支付产品权限，不要求假订单/正金额，也不要求mini支付path。可配置独立`identity_mini_path`，固定完成地址`/identity/mini/complete/{provider}/{profile_id}/{request_id}/`；SDK只POST code/state，不能提交subject、merchant、redirect。应用ID与path完全来自原档案封存。本地WechatAPI已明确给出stable_token、generatescheme和generate_urllink方法，因此A的完整范围可包含真正的微信小程序URL Link生成：使用独立identity path和opaque request/state构造受信任query，先以本应用secret取应用token，再调用generate_urllink；绝不能拿payment MiniLaunch伪造链接。Go显式选择该SDK协议，不声称还原加密generate_wxa_link控制器的原path/query。只接受实际成功响应的url_link，响应URL必须命中启用时明确审核的厂商链接域白名单，不能盲跟任意跳转或抓取远程页面。期限与原请求有效期一致；应用token只在内存/AEAD缓存并使用有界刷新租约，不能照搬PHP明文token缓存。生成链接本身需独立一次操作状态（creating/ready/failed/unknown），不确定响应不能自动重复生成，GET状态不能再次外呼。若应用没有URL Link权限，只提供不可用原因与SDK对接资料，不能显示“已生成”。该厂商现行权限、链接域及最短期限须在实施启用前用官方文档明确核对；本研究未外呼验证。

小程序与网页浏览器的信任源不同：web用一次浏览器cookie+state；mini用原请求opaque state和官方SDK code绑定应用，不冒用网页cookie。两者最终都只能由原始后台会话查询与单次reveal，回调不返回openid、不恢复后台登录，不暴露支付按钮。

启用语义须显式：保留GOEPAY_ENABLE_OAUTH总开关，增加独立identity能力开关（建议默认false）或单独的应用级enabled；能够在付款写关闭时做已授权身份交换，但无任何路径调用Submit/Transfer/Refund。此调整须由根任务确认并新增“付款写关闭+标识开启仍零资金调用”测试；不能靠移除AllowLive条件的单行改动完成。

### B. AppAuthToken：独立商家应用授权工具

新`flow_kind=alipay_app_authorization`和单独结果DTO/存储，不复用SubjectID。提供两个静态launch_kind：app_auth、app_auth_assign；assigned允许的app_types来自服务端白名单，原Release明文集合为MOBILEAPP/WEBAPP/PUBLICAPP/TINYAPP/BASEAPP。目标链接固定官方origin/固定scheme，state始终独立随机且HMAC，回调仅接受app_auth_code；绝不把auth_code和app_auth_code互相回退。

新增签名适配器：`alipay.open.auth.token.app`以biz_content={grant_type:authorization_code,code}请求；校验精确签名节点、成功码、auth_app_id、授权商户user_id、非空token及可验证到期字段。普通getToken仍不能带app_auth_token。app.query是显式只读核对操作，不在每次GET状态时自动外呼；refresh也不作为未知请求重试，不在首增量默认启用。响应如缺少可靠绝对有效期，只能把本地展示TTL与厂商凭证有效期分开，不能宣称永久有效。

结果中auth_app_id和授权商户UID是商家关系资料；app_auth_token和app_refresh_token是敏感凭证。存储AEAD关联request/purpose/issuer app/auth app/merchant/version；metadata仅masked与token_present。首期不保留refresh_token或如后续明确需要则单独AEAD，不从普通DTO回显。reveal仅一次、no-store、仅内存password输入展示，未知响应禁止自动再次reveal。**不自动配置到渠道、子商户、进件、转账或分账。**

权限建议比普通标识严格：首期仅本人super发起/查看/单次reveal（拒operator和代登录），并显式选定获准商户和ISV应用档案。若后续需要商户自助，仅可操作其被授权的应用关系，须另补owner测试。生成/接收长效授权属于应用权限操作，不是付款；是否复用管理资金密码作为再确认应由根任务明确定义，不能自行新增泛授权绕过或把金融grant当作厂商授权。

### C. 服务商/子商户、独立公众号与电商边界

本工具的主体命名空间是OAuth AppID，商户号不是openid的命名空间。档案必须明确`account_mode=direct|partner`、issuer_app_id/issuer_merchant_id、可选sub_app_id/sub_merchant_id、oauth_subject_app_id与被批准的应用侧；profile生成的绑定HMAC包括所有公开账户维度与credential revision。回调前后、poll/reveal都复验，任一角色/子商户/应用/权限变化终止旧请求；不能沿用目前只有直接merchantID的scope让更换子商户继续读旧结果。

相同协议的网页/mini交换可复用受审adapter，但服务商和子应用的secret不是互通的：根据可信profile选择具体应用，禁止调用者指示use_sub_app或任意AppID。若应用secret没有被显式配置，能力只返回unavailable；不能使用服务商secret猜子应用code，不能返回sp_openid冒充sub_openid。工具对服务商应用的标识查询支持，不等于partner支付/退款/分账已支持。

电商配置在现有明文中影响支付业务/分账的路径；其用户授权仍需一个明确公众号/mini应用。没有独立OAuth端点证据时不增加`ecommerce`按钮或接口，只可按已获准应用档案查询其OpenID，并清楚显示“仅该应用用户标识，未启用电商资金产品”。其它插件暂缓。

## 5. 建议API与DTO（新Go契约，不冒充Pro加密JSON）

- 原GET `/identity-lookups/capabilities`向后兼容原channels，新增`profiles:[{profile_id,revision,provider,app_kind,app_id,flows,available,reason}]`；无秘钥、原始account_scope或授权token。
- 原POST `/identity-lookups`保留channel_id默认web；新明确profile_id/revision/flow_kind，严格互斥，不接受前端自带AppID或身份。响应包含原request metadata、flow_kind以及固定类型的launch资料；GET只返回原状态，不外呼。
- 独立mini POST `/identity/mini/complete/{provider}/{profile_id}/{request_id}/`，strict JSON code/state，callback无明文标识。不是`/oauth/mini/complete/.../{trade_no}`。
- 普通identity reveal返回provider/app_id/subject_kind/subject_id；subject_kind由可信响应字段决定（wechat_openid/alipay_user_id/alipay_open_id），不按前缀猜标签。旧未存类型的历史结果继续安全泛称支付宝用户标识，不能追溯猜测后改写。
- 应用授权建议使用独立 `/app-authorizations`资源（create/status/reveal，可选显式query），独立public authorize/callback路由；返回app_auth_token只能在单次reveal，不放入subject字段或authorization_url。launch_kind固定，POST body绑定原profile revision。
- 旧`/user/openid/...`如需别名，仅返回安全跳转/明确新契约，仍须已有原会话创建opaque请求；不接受sid或原URL直接恢复登录，也不声称复刻原JSON。admin getTokenOptions/generate_wxa_link是否别名由精确输出证据决定，不能编造字段和成功响应。

新迁移编号由根任务分配，使用新增表或版本化列/不可变guard；不编辑47和其它冻结迁移。若扩展现有表，必须保留旧记录AAD/HMAC算法并按版本分支读取，不能用新字段默认值重新解释旧证据。

## 6. code去重必须按真实交换协议设计

现有web付款与独立web查询都会写`oauth_authorization_code_claims(domain=web)`。现有mini付款写旧`oauth_code_claims`，没有在ConsumeMiniState中追加global claim；47仅对当时历史数据做了domain backfill。独立mini接入时若只写global mini，会与新增mini-payment存在并发空隙，因此必须同时为既有ConsumeMiniState接入同一个原子global claim，并兼容历史legacy claim。state和code消费提交在所有外部I/O之前，claim冲突应烧掉新state并拒绝，不能恢复原码。

还需明确支付宝web与mini都调用同一`alipay.system.oauth.token`：不能仅因页面用途不同就认为授权码可重复兑换。建议定义稳定exchange realm（微信web/微信mini/支付宝system oauth/支付宝app auth），同实际交换方法的mini/web共享一次性code唯一键，purpose只隔离state；支付宝app-auth是不同方法，单独realm。既有47的web/mini枚举和历史claim必须兼容迁移/回填，保留原HMAC算法，不能删除旧claim或靠字符串前缀识别码型。最终具体迁移方案需在实现前由core owner确认，并用真实本地TLS+并发PG证明0重复外呼。

## 7. 红→绿测试和分工

本研究没有修改源码或运行新业务测试。下一阶段必须先用本地合成密钥、httptest TLS及专属新PostgreSQL端口/随机schema写RED，再实施；不把真实外部授权作为验收前提。

1. 配置/适配器owner：table-driven覆盖wx direct网页/mini、Alipay公钥/证书网页/mini、独立应用；错role/子App、secret缺失、服务商与ecommerce未支持配置显式拒绝。构造与capabilities零HTTP。微信URL Link必须经真实本地TLS的stable_token→generate_urllink两步，检查原App、固定path/query/期限、token不落日志、坏响应/未知不重发，链接生成不能调用付款API。普通token严格top-level grant_type/code，app-token严格biz_content+精确签名节点，证书unknown SN/过期/重复JSON/身份错配/超大响应拒绝；不得泄漏access/session/refresh token。
2. store/core owner：持久原session、owner、Issuer/admin class、五分钟TTL、配置版本和完整角色scope HMAC、AEAD/AAD、并发state一次消费与单次reveal。支付web/mini/独立工具共享实际exchange realm的并发重复码仅一次外呼，历史47claim仍有效。失败、超时、exchange期间撤销session/merchant/profile、恢复配置后旧state仍不可复活。AppAuth和用户identity交叉purpose均拒绝。
3. server owner：新fixed路由/strictJSON/重复query/cookie、CSRF、bodylimit、限速、callback allowlist、POST mini code；无任意host/redirect/path/sid。真实TLS+PG分别走完整wechat mini、alipay mini、用户web、app_auth、app_auth_assign；不允许把code放GET或写访问日志，GET状态不调用provider。
4. console/UI owner：原标识页以应用/用途清楚分组，独立应用与原付款AppID同时展示，能力未知/缺secret/未支持角色不可点；明确展示单次结果、过期、未知、已消费和配置变更。离页、换merchant/profile清空内存；无storage/URL/log主体或token。AppAuthToken独立password字段与单次确认，不自动绑定channel；operator/代登录拒绝。测试375px、真实代理路径和CSP，并覆盖SDK mock而非伪支付二维码。
5. 不变性验收：每类独立查询全程orders、provider_attempts、payment_receipts、ledger_entries、资金任务及余额均无变化；任何支付adapter Create/Submit/Transfer/Refund调用计数0。仅mock Exchange OAuth接口允许一次；capabilities/poll/reveal不外呼。
6. 根任务最终集成：文档、OpenAPI、菜单能力和默认关闭开关；影响范围的全部web/mini付款HMAC/原App绑定测试回归，再独立审查/固定SHA/Windows与Linux完整门禁。AppAuth或partner未做的分支保留明确不可用，不能因原菜单可打开就声称全量兼容。

下一步建议根任务先确认A的profile/flow DTO与code realm迁移，再拆为core、adapters/config、HTTP/UI三个互不冲突的owner；B按独立token资源并行但不可先开放到默认环境；C仅在可信profile模型完成且本地证据足够时接入。当前保持只读研究状态，等待根任务执行通知。



## 8. 关键快照校验值

以下只读取本地源码计算SHA256，便于实施前识别证据是否变化：

| 文件 | SHA256 |
|---|---|
| `reference/server-pro/plugins/payment/wxpay/info.json` | `0e8163eeef1c773ca94342443406b36e4679abb36131e0fe9cc4bbeb2e9d5155` |
| `reference/server-pro/plugins/payment/wxpay/WxpayPlugin.php` | `e6b343dc7f836fec7ee1e0cb90222b54d7c2d91af6aee68cc393c00dced7749e` |
| `reference/server-pro/plugins/payment/alipay/info.json` | `eda52b15949faca5af63d034b1c28d47a6eb4769e78739548229d96a403f9c03` |
| `reference/server-pro/plugins/payment/alipay/AlipayPlugin.php` | `468df478a2f858081db76dcd8a85ec6081755b552937993d34350d838698ef0e` |
| `epay_pro/public/static/assets/index-BySTvbcv.js` | `fe5c46574891b353c038f5b55ba58c37ecbd3b8b4c4169316bfb2905468651da` |
| `epay_pro/vendor/cccyun/alipay-sdk/src/AlipayOauthService.php` | `91c16f8c5bf9969200ccf9fe441c2f1958cc033a0fa3117a2aa1d4922e277659` |
| `epay_pro/app/lib/wechat/WechatAPI.php` | `aa994772dedd8868fcd84524f36f0fb31c25b9e2eab4f0acceae7e713dc0456d` |
| `epay_release/user/openid.php` | `7336acce4632ea8c7420fa7a4173128591d69980ec24b20f8f2b3234270bfe4c` |
| `internal/server/identity_lookup.go` | `b76e1d4ac70ad7f5548952f0acea62c99b775f463d87884c655d39efb32cc49a` |
| `internal/server/oauth_mini_complete.go` | `40631209abd8a5a039fb88c018d9ce1879737d55d336965c6fb81c53683c4019` |
| `internal/channelconfig/oauth.go` | `28854ad304325bd00ea041d377f22789dca9656aaba4b4ac2aa72a4df6475e14` |
| `internal/channelconfig/mini_oauth.go` | `d8f523f5fbdd36f1b2c2f2226c76573e0177e69c439a62f813397d332eafd9dc` |
| `internal/payment/oauth_mini_state.go` | `a9cf1247b2d8448ea05af173c8241b56b7cb2d738c4d424d126601fbb6dff4e7` |
