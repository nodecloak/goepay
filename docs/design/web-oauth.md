# 微信、支付宝网页 OAuth（迁移 27）

本增量仅覆盖同一可信支付通道 AppID 的微信 `snsapi_base` 与支付宝 `auth_base`。OAuth 成功只得到支付所需买家身份，仍必须走既有 ProviderFlow、迁移 24 的不可变 payer 绑定和真实支付回执；授权与 JSAPI 材料均不等于已支付。

## 来源与当前边界

本地明文来源：`epay_pro/app/common.php` 的 `wechat_oauth/alipay_oauth`，`app/lib/wechat/WechatOauth.php`，`vendor/cccyun/wechatpay-sdk/src/JsApiTool.php`，`vendor/cccyun/alipay-sdk/src/AlipayOauthService.php` 与 `AlipayService.php`。支付宝 token 成功节点可没有 `code=10000`，不能直接套普通 trade API 的响应条件。

已查阅 [支付宝 token 官方文档](https://iopenhome.alipay.com/docs/ac/solution_api/oauth_token) 与[官方授权链接说明](https://docs.antom.com/ac/common/obtain_uid)。[微信官方网页授权文档](https://developers.weixin.qq.com/doc/offiaccount/OA_Web_Apps/Wechat_webpage_authorization.html) 及官方旧入口本轮抓取受限，未将无法读取的正文当作已核对内容；微信字段画像依据上述本地明文并用真实 mock TLS 验证。这里不宣称真实产品权限、授权域名注册或设备联调已完成。

不实现 `auth_user/snsapi_userinfo`、手机号或昵称资料收集、跨 AppID 的登录通道替换、OAuth 登录后台账户、小程序 code 兑换。小程序将使用独立用途与状态；不能复用网页浏览器 cookie/state。

## 显式启用

只有配置模式读取显式指定的加密通道文件，且 `GOEPAY_ENABLE_OAUTH=true` 与通道 `enable_web_oauth=true` 同时开启。微信需要 JSAPI 产品及加密导入中的 `oauth_appsecret`；支付宝需要 JSAPI 产品及现有应用私钥/平台公钥或显式可信证书链。OAuth AppID 就是支付通道 AppID，不能用请求参数替换。证书模式复用应用/平台/根证书 SN、链和有效期校验，无下载、无公钥降级。

`BuildOAuth` 只离线构建客户端，不读取额外文件或发出请求。全局/通道默认关闭，非法布尔值拒绝，simulation 不加载 OAuth。外部写入开关、商户启用状态和当前通道白名单同时约束开始与完成流程；关闭写入也不能借 OAuth 恢复支付提交。

## 请求与浏览器绑定

已验签创建请求使用 `method=jsapi&oauth=1`，且没有 `sub_openid`；`is_applet=1`、任意非 `0/1` 的 OAuth 值、同时传入买家身份或替换 AppID 均拒绝。原有签名 `sub_openid` 路径继续工作，不要求 OAuth。

创建只持久化订单与 `oauth_payment_intents`，将商户、平台订单、通道、provider、AppID、method 和固定用途 `payment-jsapi` 绑定。返回原受 HMAC 和到期时间保护的 checkout jump；此时没有 provider attempt、支付材料或资金账务。

只有精确 checkout 页面可以签发授权 state。二维码图片路径不会签 state，同步 return 在尚未授权时仅提示待完成。授权跳转固定到微信或支付宝官方 HTTPS 地址，scope 不接受客户端替换；回调地址取可信配置 origin，不取请求 Host 或任意 redirect 参数。

浏览器收到 `__Host-goepay_oauth`：随机 256 位、Secure、HttpOnly、SameSite=Lax、Path=/、无 Domain、五分钟。有效同浏览器 cookie 会复用，多个标签的 state 不会互相覆盖。每个 state 是独立随机 256 位，只保存 HMAC；同时绑定浏览器 cookie 的 HMAC 与完整支付意图，五分钟到期，每个订单最多八条未过期未消费 state。

## 回调与单次消费

回调固定为 `/oauth/callback/{provider}/{channel_id}/{trade_no}/`，只接受 GET；拒绝重复 query 参数、重复/缺失浏览器绑定 cookie以及路径/AppID/scope 与意图不匹配的请求。

外部兑换前在事务中锁定订单、原子消费 state，并持久化 `(provider, AppID, HMAC(code))` 唯一占位。code 在同一应用下不能换 state、订单或商户后再次兑换。原始 code、state、浏览器 token、access/refresh token、secret 不写入 OAuth 表；过期、错误绑定、重放均不兑换。过期按取得锁后的 `clock_timestamp()` 检查，避免事务开始时未过期、等待订单锁后已过期却仍放行；code 唯一占位可能等待后还会再次检查墙钟。

有效回调即使遇到商户禁用、停写或白名单/AppID 变化，也不能在恢复配置后复活原 state。上游兑换失败只能重新打开受保护 checkout 获得新 state、新 code；已有 provider attempt 的订单不会再次签发授权或更换 payer，只能处理原支付材料或查询。

兑换请求只能到固定官方 HTTPS token endpoint，禁止重定向，超时和响应大小有界。微信通过 TLS 验证 token 响应；支付宝必须验证精确 OAuth token JSON 节点的 RSA2/证书签名、SN 和有效期。重复 JSON 身份或签名字段拒绝。返回值只含 provider、AppID 与可信 SubjectID，立即丢弃令牌；网络与上游错误转为静态错误，不回显含 code/secret 的 URL 或响应正文。

兑换前、后以及最终提交前重新检查商户、写开关、通道白名单和支付/OAuth AppID。得到身份后，对原订单保存的客户 IP、实际回调连接 IP 和买家标识重新执行现有黑名单/商户风险/域名策略；不信任回调转发头。随后才调用已有 `ProviderFlow.Submit`，由迁移 24 绑定 payer，并在外部支付 I/O 前持久化原支付意图。

回调只跳回受保护平台 checkout，地址和页面不包含 code、token 或买家标识，不凭浏览器参数入账。新代码不记录敏感兑换参数；仓库 nginx access log 使用仅路径格式，配置部署时也必须保持回调 query 不入访问日志。
