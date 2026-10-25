# 64：可信独立OAuth应用与网页/小程序标识查询

本阶段落实研究文档 `identity-lookup-next-stage.md` 的A部分。支持微信与支付宝直连账户的独立网页、独立小程序用户标识查询；授权AppID可以与关联收款通道AppID不同。AppAuthToken、服务商/子商户、电商账户及URL Link/Scheme生成仍未实现，不能将其配置解释成直连或伪装成成功授权。本功能没有支付、退款、转账、余额或账本权限。

## 配置与开关

新增显式 `GOEPAY_ENABLE_IDENTITY_LOOKUP=false` 默认关闭开关。开启还必须显式开启总OAuth开关 `GOEPAY_ENABLE_OAUTH=true`，使用configured模式、固定HTTPS回调源和原加密配置导入；本地模拟模式不读取配置、不展示虚构用户标识。独立查询不依赖付款 `AllowLive`，也不要求收款通道启用JSAPI、小程序付款等产品，但关联通道本身必须通过现有真实适配器配置校验。关闭付款写入不会自动打开独立查询。

原加密Import新增 `identity_profiles` 数组，最多256项。以下仅为字段说明，字符串均为合成占位值，不是可直接启用的实际配置：

```json
{
  "id": "11",
  "revision": "1",
  "channel_id": "7",
  "enabled": false,
  "provider": "wxpay",
  "kind": "mini",
  "app_id": "synthetic-independent-app",
  "account_mode": "direct",
  "path": "pages/identity/index",
  "merchant_ids": ["1"],
  "config": {"appsecret": "synthetic-placeholder"}
}
```

- `id/revision/channel_id/merchant_ids`均为规范正int64十进制字符串。ID在整个导入内唯一，包括未启用项，不能后项覆盖前项。商户白名单1..1000项且不重复。
- `enabled`默认false；启用档案必须显式写`account_mode=direct`。未知类型、服务商/子商户/电商角色、AppAuthToken配置均拒绝；实际付款工厂也拒绝已识别但未实现的这些角色字段，避免静默降级。
- `kind=web`时`path`必须空；`kind=mini`时必须为1..256字符的固定安全小程序页面路径，无query、片段或相对路径段。
- 微信只接受`config.appsecret`；支付宝接受`appsecret/appkey/sign_type`及现有受信任公钥或完整证书链模式。证书字段`app_cert_path/alipay_cert_path/root_cert_path`沿用加密导入内的PEM内容语义，不读取请求指定文件路径。
- 导入严格拒绝重复、未知、null字段和非字符串嵌套凭据值。档案指纹包含独立OAuth凭据、完整档案和完整可信关联通道配置，包括支付宝SellerID对应的真实`appmchid`字段。即使只轮换关联通道付款凭据，也保守地使旧档案请求失效；指纹只作为服务端HMAC输入，不在普通JSON中返回。
- 修改secret、证书、AppID、路径、商户白名单、关联账户或revision后，原请求不能继续；服务端每步重新构建当前可信绑定。明确被失效的请求即使恢复旧配置也不能重新使用。

## 查询生命周期

`GET /console/v1/{admin|merchant}/identity-profile-lookups/capabilities`返回当前商户可用的公开档案，不调用厂商。管理员须显式传`merchant_id`；商户只能本人。拒绝代登录。`available=false`只表示未启用或无当前可用档案，不伪造授权地址。

创建请求需原控制台会话、CSRF、`profile_id/profile_revision`，管理员还需明确`merchant_id`。同一durable会话下，新64与旧47查询合计最多5个活动请求，创建另有限速。最大TTL五分钟，且不超过原会话到期时间。档案来源HMAC绑定**精确请求ID**、原会话身份/版本、商户、通道、应用、类型、路径、指纹、期限及网页授权令牌哈希。独立launch HMAC再绑定原state与浏览器；持久来源和状态转换有数据库不可变约束。

网页创建只返回一次`authorization_url`。固定服务端入口首次绑定独立Secure/HttpOnly/Lax浏览器cookie，再跳转固定官方最小scope授权。回调固定在`/identity/profile/callback/{provider}/{profile_id}/{request_id}/`，校验原浏览器、state、厂商及原应用；支付宝`app_id`必需，微信如提供也必须相符。禁止未知或重复query。页面只显示固定完成提示，不在回调URL、HTML或状态接口中返回用户主体。

小程序创建只返回SDK对接资料，不生成支付二维码，也不声称已经生成厂商URL Link：

```json
{
  "kind": "sdk",
  "provider": "wxpay",
  "app_id": "synthetic-independent-app",
  "path": "pages/identity/index",
  "method": "POST",
  "completion_url": "https://fixed.example.invalid/identity/profile/mini/wxpay/11/REQUEST_ID/",
  "body": {"state": "ONE_TIME_OPAQUE_STATE"},
  "expires_at": "RFC3339_TIME"
}
```

集成方必须在所列原小程序内使用其钱包SDK取得本次临时code，然后向固定completion URL提交`{state,code}`。小程序请求域名需由实际应用事先配置。控制台也提供原会话加CSRF的显式提交与确认入口`POST .../{request_id}/complete`。两入口共享原state和code消费，不接受客户端OpenID、sid、access_token、session_key、支付ticket或任意回调URL。SDK资料和code仅保留当前组件内存，离页、换主体/档案、超时及提交后清空，不写storage或日志。

服务端先原子消费state与实际授权码realm，再只调用既有OAuth适配器：微信网页与mini分别是两个实际交换realm，支付宝网页和mini共用`alipay.system.oauth.token`，purpose不能制造第二次兑换机会。迁移64补齐支付宝历史mini的全局realm映射，保留47历史付款claim检查与原HMAC算法；旧网页付款、小程序付款和独立标识查询不能重放同一真实realm授权码。

结果只加密存储用户主体，丢弃access/session/refresh token；AES-GCM AAD为精确请求来源HMAC。`GET .../{request_id}`只返回状态与公开绑定。`POST .../{request_id}/reveal {}`重新核验原会话、原版本、当前档案、商户与期限后，原子执行一次领取并提交审计，提交成功才返回明文。并发仅一位调用者得到结果。错误、超时或未知响应均不自动兑换/领取第二次；只查询原请求。返回的主体仅属于该provider/AppID，不承诺跨应用相同。

## 验收与限制

纯crypto及真实PG测试先复现并修复了相同会话短期限请求的AAD共享：两请求精确来源必须不同，离线绕过SQL触发器复制密文后，目标请求仍不得解密，原请求仍可领取。另覆盖伪32字节HMAC、SQL不可变来源、单浏览器绑定、配置恢复不能复活、会话auth_version撤销、审计等待后期限到期、全局授权码realm与并发一次消费。

真实本地TLS加PostgreSQL合同分别完成微信/支付宝×网页/mini四条路径，付款AllowLive=false仍能显式独立查询。还在实际TLS交换中撤销原会话，确认无主体保存/领取、原state不可重放；所有测试只有合成应用/密钥与回送本机的HTTP传输，没有访问生产或实际官方授权。每条合同检查订单、厂商付款尝试、账本、任务均零新增。

UI单元与浏览器覆盖默认关闭、原会话隔离、SDK确认、未知响应只读原请求、一次领取、离页清除和375px显示。完整原付款web/mini授权回归、全前端门禁、独立审查与最终固定源码SHA以`docs/verification/identity-profiles-64.md`及原始日志为准；本设计文档不是部署验收声明。
