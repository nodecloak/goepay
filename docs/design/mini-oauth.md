# 小程序启动与一次性身份兑换（迁移30）

本增量支持同一支付应用中的微信 JSAPI、支付宝 mini 付款。启动材料保留原 common.php 的 `path?money&url` 组织方式；`url` 改为平台生成的专用 POST completion。启动、身份授权和付款成功是三个不同阶段。只有随后验签且核对订单、金额、人民币和流水的支付查单/通知才进入统一 PaidReceipt 账本。

## 配置与客户端契约

仅显式解密导入的渠道可配置 `enable_mini_launch=true`、`mini_path`，并要求全局 `GOEPAY_ENABLE_OAUTH=true`。微信还需独立 `mini_secret`；支付宝复用已配置私钥和平台公钥/证书信任。`mini_appid` 省略时采用支付 AppID，有值必须与它相同。微信可选 `mini_program_id`。渠道必须有微信 apptype2 或支付宝 apptype7；预授权不进入这条链路。付款停写、simulation、sandbox、非 HTTPS origin、商户禁用或失去渠道白名单均不可兑换并付款。

已经验签的创建参数选择 `method=applet`（或兼容 `is_applet=1`），不能带客户端 `sub_openid`，也不能混入网页 `oauth=1`。普通签名买家接口继续存在。V1返回受保护收银台，V2返回 `wxapp` JSON启动材料或 `urlscheme`。打开启动材料本身没有上游身份/付款请求。

小程序客户端必须适配以下步骤：

1. 读取可信启动路径中的 `money`、`url`；金额仅作展示，付款金额以平台原订单为准。
2. 微信调用 `wx.login` 获取临时 code；支付宝调用 `my.getAuthCode({scopes:['auth_base']})` 获取 authCode。
3. 将 code 通过 HTTPS POST 到完整、不改写的 `url`，`Content-Type: application/json`，body 为 `{"code":"<SDK code>"}`。如 body 同时携带 `state`，必须与 URL query 的唯一 state 完全一致。不得提交 openid、手机号或自选 AppID。
4. 成功结果为 `code:0,data:{trade_no,checkout_url,attempt}`。`attempt` 仅包含原付款创建结果；微信 JSAPI材料、支付宝支付流水由 SDK继续付款。`processing/unknown` 或创建结果未确定时使用受保护的 `checkout_url`/原订单查单，不能重新换买家。失败且还没有付款 attempt 时可重新打开收银台取得新 state 和新的 SDK code，原 code 不可重用。

completion 固定为 `/oauth/mini/complete/{provider}/{channel}/{trade}/?state=<opaque-state>`，只允许 POST。它没有网页 cookie 语义，也没有通过 GET code 回显身份的兼容模式。因此原来向旧 GET code 页面提交的客户端需要改造；这里不宣称原小程序无需改动。

## 持久状态与风控

`mini_payment_intents` 固定订单、商户、渠道、provider、支付应用、底层 method、用途 `mini-payment` 和可信页面。`mini_oauth_states` 保存随机256位 state 的 HMAC，五分钟过期，每订单最多8个未消费有效 state。不存在明文 code、state、session_key、access_token 持久化。mini state 与网页 state 使用不同 HMAC域，不可跨用途消费。

兑换前先锁订单并原子消费 state，然后写入网页/小程序共享 `oauth_code_claims`。共享主键为 provider+AppID+code HMAC，所以跨 state、商户、web/mini 并发仍只有一个兑换资格。迁移30只扩展27的 claims 表：web FK与 mini FK必须二选一，mini复合FK强制 provider/AppID/state匹配；原不可更新/删除 guard保持，不能将旧claim换到新state。mini state自身也通过复合FK固定到原付款intent。迁移27内容未修改。

订单锁或code唯一键锁等待之后，使用 `clock_timestamp()` 当前时间检查到期，不能用事务开始时的 now()。禁用商户或已有付款attempt的有效旧state会被消费并拒绝；恢复配置不能复活它。外部兑换失败时，state/code认领保持消费，没有重新发送同code的路径。在首个付款attempt创建前，不同有效state和不同code仍各有一次身份兑换资格；并不声称整张订单只允许一次身份HTTP请求。24的订单锁和不可变payer认领最终保证只能产生一个付款attempt和一位payer，竞态输家不可再提交付款。

微信仅向固定 `https://api.weixin.qq.com/sns/jscode2session` 兑换，可信响应同时要求 openid/session_key，但只返回前者；丢弃 session_key。支付宝复用迁移27的 `alipay.system.oauth.token` 顶层code签名和精确token节点验签，证书模式校验已pin的平台SN/信任/有效期，不自动下载证书。HTTP有超时、不跟随重定向，响应大小受限；错误为固定文本，不输出code/token/secret/请求URL。

兑换前后重复检查实时开关、支付与身份应用一致、商户和白名单。得到可信身份后，原订单保存的clientIP、真实连接来源IP、可信buyer分别通过现有CheckPayment。最终由24的不可变payer绑定创建原支付attempt；授权不直接改paid、不增加余额。付款响应丢失后已有attempt只能恢复原请求，不能重新通过OAuth换人。

## 依据与边界

本地明文依据：`epay_pro/app/common.php` 的 wxminipay_jump_path、wechat_applet_oauth、alipaymini_jump_scheme；`epay_pro/app/lib/wechat/WechatOauth.php::AppGetOpenid`；`epay_pro/vendor/cccyun/wechatpay-sdk/src/JsApiTool.php::AppGetOpenid`；支付宝原getToken及27已核对的签名节点契约。没有解释加密插件内容。

官方资料：[支付宝 my.getAuthCode](https://idocs.alipay.com/miniprogram/miniprogram/mpdev/API_OpenAPI_getAuthCode) 描述auth_base只取身份；它的国际小程序文档不作为国内全部产品权限的证明。微信[code2Session官方入口](https://developers.weixin.qq.com/miniprogram/dev/OpenApiDoc/user-login/code2Session.html)本次抓取失败，不能声称已核验其当前页面；具体请求字段由本地明文SDK与固定HTTPS本地TLS测试交叉验证。

81增量提供[原生客户端工程](../../clients/mini/README.md)与[客户端约束](mini-client-81.md)，保留money/url启动字段并适配上述POST契约。当前客户端测试结果独立记录，未包含在94固定候选中。

尚未验证真实设备SDK、实际小程序发布/业务域名、平台产品权限和生产授权成功率。无跨AppID授权、手机号/profile权限、session登录或生产资金验证。离线边界见本增量验证记录。
