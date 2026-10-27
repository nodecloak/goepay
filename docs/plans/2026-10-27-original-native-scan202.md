# 微信与支付宝原生扫码

继续复用既有容器与数据库，不请求生产或真实支付，不发起真实资金交易。先可编译的HTTP/组件功能RED，实际失败确认后实现；原192付款码准备、明确确认、提交、恢复与查询流程不变。

本机 Pro 公开 index-Kun9uPxi.js 可确认微信使用固定jweixin-1.6.3.js，POST scanpay/jsconfig仅传当前href去hash；返回debug/appId/timestamp/nonceStr/signature/jsApiList给wx.config，ready后scanQRCode(needResult=1,qrCode/barCode)。支付宝使用固定3.1.1 alipayjsapi及ap.scan(type=bar)，依次取code/barCode/qrCode/result。普通浏览器摄像头、扫码枪和手输保留。回调只提供付款码，仍需原流程明确确认，不自动付款。

原Scanpay控制器已加密，无法确定公众号选择规则；不认定其使用login_wx，也不把OAuth授权当作扫码授权。原WechatAPI可确认stable_token、jsapi ticket、提前约200秒过期缓存与SHA1键排序jsapi_ticket/noncestr/timestamp/url。实现使用管理员明确选择的现有加密wxpay/web资料，独立enabled默认false、profile_id/revision/fingerprint；加密源轮换后必须重新确认。范围取该资料MerchantIDs；不复制secret明文落库、返回或日志。

Server使用不可变ScanJSProfile202集合在启动/原子reload发布。独立签名器按配置实例缓存token/ticket并去重请求，固定官方主机，TLS校验、禁代理/跳转、限长/超时；不请求用户待签URL。POST /console/v1/merchant/scanpay/jsconfig仅原始持久本人会话及CSRF，URL必须匹配配置siteorigin和/merchant/scanpay路径，拒外站、userinfo、fragment和控制字符。管理员本人设置为GET/PATCH /console/v1/admin/settings/scanpay-js，独立明确启用及二次确认，不改变其它设置。

前端按UA使用固定SDK，加载失败/取消可回到现有输入，不自动重复扫码；切client/CSRF/商户/本人状态或卸载/取消后迟到script/config/ready/scan回调不得恢复码或请求付款。同一次扫码仅接受一次结果，付款码不写URL/storage。使用受控wx/ap与本地TLS官方Host替身验证精确签名、并发缓存/过期/轮换、权限和零上游拒绝、无凭据输出，以及375/1440交互；测试不访问SDK或微信公网。

首次实际保存发现原资源枚举约束未允许独立设置，返回23514；由114迁移仅追加scan-js-settings202，原26种资源、旧行和历史证明保持。升级/重跑幂等及未知资源继续拒绝均通过真实PG回归。此数据库结构变更与201的113独立。
