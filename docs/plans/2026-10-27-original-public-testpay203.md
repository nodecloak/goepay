# 原版公开测试支付与验证码

复用既有容器和数据库，不访问生产、真实验证服务、真实通道或资金。先实际HTTP/真实App功能RED，再独立模块和根接线，测试全部使用模拟支付与受控验证服务。管理员通道测试不能替代此公开入口。

本机 Pro 路由与公开代码确认页面路径为 /test，API为 /user/testpay/init、submit、success，不要求后台登录。init原公开字段csrf_token/trade_no/money/paytype/sitename/captcha_open；trade_no在该页面是服务器签发的商户订单号。submit原money/typeid/csrf_token与相应极验证明。成功页须读取原订单可信支付状态，不能用查询ok参数伪造成功。原安装SQLtest_open=1、test_pay_uid=1000，验证码具体测试/登录开关未在该SQL出现；加密控制器默认和会话细节不得冒称已证实。Go未明确配置时安全关闭，不把固定商户缺失或验证服务不可用变成假成功。

新Go规范路径 /console/v1/public/testpay/{init,submit,success} 与 /console/v1/public/captcha。独立公开DTO保持原字段，不扩张publicSite191品牌13字段白名单，不返回固定商户秘密或captcha_key。持久browser-intent绑定HttpOnly SameSite cookie、随机CSRF、原out_trade_no、固定测试商户与真实支付类型目录；提交时封存金额和类型，幂等/未知结果只读查询原意图，不改号重付。只允许预先选定商户、原商品名/路由和明确允许的产品，不接收浏览器自报pid/notify_url/return_url。复用原支付政策/路由/风险/checkout能力，不调用管理员测试接口或伪造商户RSA签名。

公共测试支付、后台登录和付款风控验证码为三个独立purpose，证明不能互认。原公共captcha接口为V3/V4启动参数；V4按lot_number与密钥HMAC-SHA256并远端双success，V3按本机可读GeetestLib协议远端验证。原库md5(challenge)弱fallback和demo替代不能移植为成功路径。captcha_key独立加密/HMAC保存，不采用旧明文；固定可信上游、TLS/限长/超时/禁跳转，验证证明与browser/purpose/intent绑定一次性消费。付款verify_slide的__defend是另一条独立链，不能混作testpay证明。加密控制器未公开的细节以新安全绑定明确说明，不伪装为已还原算法。

根任务拥有Server公共分派（在Enrollment catchall之前）、App初始化前/test识别、设置/cmd注入与API文档；agent拥有独立页面、后端模块、验证码适配器和115迁移及行为测试。先完成公开TestPay与公共CAPTCHA，登录和payment-defend消费点分别接线验证后才能声称完成。覆盖公开初始化不读取后台会话、严格字段/方法/目的、一次模拟编排/重放、未支付与可信已支付、验证服务不可用零支付副作用、无敏感持久化、离开取消与375/1440交互。
