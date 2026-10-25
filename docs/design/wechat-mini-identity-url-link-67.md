# 微信小程序独立标识查询 URL Link

此增量只扩展64的独立微信小程序用户标识查询，不具有付款、退款、转账或自动绑定通道权限。支付宝应用授权另见66/68验证记录，服务商/子商户和电商账户不因此获得支持。URL Scheme不在本增量范围。

## 协议和配置

2026-10-24核对[微信 stable_token 文档](https://developers.weixin.qq.com/miniprogram/dev/server/API/mp-access-token/api_getstableaccesstoken.html)、[generate_urllink 文档](https://developers.weixin.qq.com/miniprogram/dev/server/API/qrcode-link/url-link/api_generateurllink.html)与[URL Link 能力说明](https://developers.weixin.qq.com/miniprogram/dev/framework/open-ability/url-link.html)。对应原明文`epay_pro/app/lib/wechat/WechatAPI.php`的链接生成用途，不复制其明文token缓存和默认600秒期限。厂商账号实际资质和权限不通过本地测试推定。

在既有加密`identity_profiles`的直连wxpay+mini档案中增加可选布尔`enable_url_link`，默认false。显式true才构建独立客户端；不新增支付产品或伪订单。原OAuth总开关、独立标识开关、可信HTTPS回调源、configured模式、通道和商户白名单约束继续生效。省略/false保留原JSON和指纹字节；启用及任何secret/AppID/path/revision/白名单变化纳入完整绑定指纹。

能力响应每档案可选`url_link_available`，省略视为false；顶层为任一获准档案可尝试。创建响应顶层只对应本次所选档案。可尝试不代表链接已生成，也不是厂商权限证明；能力查询和原身份请求创建零厂商I/O。

## 原请求与状态

新增迁移67的`profile_identity_links`和不可变事件。每个原64身份请求最多一个链接意图；必须仍为pending、原本人session有效、商户配置版本和档案指纹一致、微信mini且原期限剩余超过70秒。沿用原request_id/state和固定小程序path，禁止客户端传金额、订单、OpenID、redirect或任意query。

核心每步先锁原父请求，再锁子意图，统一锁序。来源HMAC封存父证明、state哈希、session/用户授权版本、商户版本、档案指纹、原出生/期限与当前管理员类别。链接意图有独立状态HMAC，ready URL用AEAD及来源证明加密，不在普通原身份DTO中暴露。新行出生事务与created+claimed审计必须同事务提交，唯一提交成功的Begin调用才得到私有Generation资料和一次发送资格。

`creating → ready | failed | unknown`为单向终态；已有任何意图都不再发送。数据库异常导致创建事务确定回滚时可以重新尝试Begin；已提交后超时、进程终止或保存失败都保留发送屏障，不能因为新会话、重新授权或新按钮点击重新生成。GET只读本地，不调用厂商。失败或unknown不消费原SDK code/state，仍可沿原SDK流程完成身份查询；原请求完成/过期/撤销后链接不可读取。

## 外呼和期限

先POST固定`https://api.weixin.qq.com/cgi-bin/stable_token`，`force_refresh=false`；token仅当前函数内存使用。然后POST固定`/wxa/generate_urllink`，query只携带临时access_token，body为原固定path、`request_id/state`、`env_version=release`、`expire_type=0`及原期限向下取整Unix秒。原五分钟期限不延长；两次调用前均检查剩余超过70秒，以覆盖厂商大于一分钟的限制和整个调用最多10秒的传输预算。

使用独立标准TLS transport，验证官方域名和证书、TLS至少1.2，不继承代理/cookie/redirect/任意RoundTripper。GetBody清空且关闭连接复用，不缓存token、不刷新、不自动重发。响应体、头和JSON字段有界且拒绝重复/null/异常UTF-8，错误不返回厂商原文或凭证。链接POST后不确定结果使用专用unknown错误；明确厂商拒绝为failed，二者都不能重发。

返回链接仅允许精确HTTPS `wxaurl.cn`或`wxmpurl.cn`，拒绝端口、包括空@的userinfo、fragment、控制字符、路径穿越和dangling ?/#。唯一可选`cq`遵守官方限定且decoded最大256；不自行追加。核心对适配器结果独立复核provider/AppID/request_id及requested_expiry，不能仅凭HTTP成功保存不匹配链接。`requested_expires_at`只是发给厂商的请求期限，不声称响应提供实际链接期限证明。

## HTTP与界面

`POST /console/v1/{admin|merchant}/identity-profile-lookups/{request_id}/url-link`只接受`{state:原SDK state}`，需要原本人会话和CSRF；GET同一路径无query，仅本地读。管理员仍按原64权限规则，未扩大super/operator范围。发送前后重新读取当前可信绑定，core重验原会话与商户版本；浏览器断开后用三秒后台上下文尝试持久化终态，但不会绕过撤销后的禁止保存/读取。

界面显式生成、显式打开实际返回链接，不自动重试/轮询/打开；未知只能手动查询原本地状态，不能重新生成。URL与原state只在当前组件内存，过期、换上下文和卸载清除。SDK资料及显式code完成入口保留，页面不能把链接获取失败解释成身份验证成功。

## 验收边界

适配器真实合成TLS、核心PostgreSQL、HTTP与前端分别保存实际RED/GREEN、并发与撤销证据。所有证书、应用和密钥为合成，官方hostname仅将TCP映射到本机测试TLS；不访问真实厂商或生产环境。固定68/69/70均不含此增量，最终固定候选及部署状态须另记，文件存在不等于已部署或全部PHP兼容。
