# 小程序支付客户端81

本增量补齐迁移30的客户端工程，不修改支付、账本、OAuth接口或1..80迁移。依据为本地Pro `app/common.php` 的 `wxminipay_jump_path`、`alipaymini_jump_scheme`，以及已实现的 `/oauth/mini/complete/{provider}/{channel}/{trade}/?state=...` 契约。94固定候选继续独立测试，不编辑其任何文件。

微信与支付宝各提供可导入开发者工具的工程，共用无第三方依赖的CommonJS控制器。页面保留原路径 `pages/pay/pay` 及 `money`、`url` 参数；只在用户点击后取一次SDK code，以JSON POST兑换，再使用原返回的JSAPI材料调起支付。GET code兼容不恢复，不向客户端分发AppSecret、商户API密钥或签名私钥。

公开构建配置固定 `enabled`、HTTPS `origin`、`appId`、`channelIDs`。默认关闭；示例origin/AppID仅为合成占位配置。运行时SDK实际AppID必须一致。参数的完整origin、provider、规范正整数channel、原trade及唯一state均验证；允许SDK已经解码的URL或只解码一次的原URL，不允许二次转义、任意外链、额外query、user-info、fragment或非HTTPS。金额只展示，不加入服务器请求，不作为支付金额授权。

客户端生命周期为ready→authorizing→exchanging→paying→verification_required；异常或SDK取消均终结为error/cancelled/verification_required。每个页面控制器只启动一次，同一小程序进程对同一state最多兑换一次；第二次点击与并发页面不重用code。超时或响应丢失后不重新登录、兑换或付款；只提示查看原订单。页面卸载取消尚在进行的请求，并屏蔽所有晚到回调；已经提交的服务端意图仍由原订单恢复机制处理。

成功响应须HTTP200、code严格等于数值0、原trade/channel/method一致，attempt为pending且material_type为jsapi，方可调SDK。processing/unknown/failed或材料缺失只进入待核实，不重新建单。微信签名参数须原AppID及规范字段，支付宝只传原 `provider_trade_no` 相同的 `tradeNO`。SDK成功/9000不宣称服务端支付成功，页面始终提示以原订单查询或可信通知为准。checkout链接仅在验证同origin、原trade、expires/token且无额外参数后展示；不得向日志、存储、剪贴板、任意web-view传播state、code或材料。

离线测试注入SDK替身，覆盖两平台、双编码、错AppID/provider/channel/trade、重复点击/跨页、挂起/超时/卸载、坏HTTP/包体、非法材料及SDK取消。构建器生成两套独立工程并校验每个文件SHA256，构建产物不得带测试代码或密钥。没有真实设备SDK编译、发布审核、域名白名单或生产产品权限的验证，不把本地模拟称为实际资金联调。

公开文档核对：2026-10-24查阅[微信V2小程序调起支付](https://pay.wechatpay.cn/doc/v2/merchant/4011939566)、[支付宝my.tradePay](https://idocs.alipay.com/miniprogram/miniprogram/mpdev/api_openapi_tradepay)、[支付宝my.getAppIdSync](https://idocs.alipay.com/miniprogram/miniprogram/mpdev/api_basic_getappidsync)。支付宝国际文档仅支持客户端API形状，不证明国内全部产品权限；服务端仍以本地PHP/SDK及已有TLS契约测试为准。
