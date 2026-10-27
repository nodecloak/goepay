# 订单详情同步重新通知

继续复用既有测试容器和数据库；不访问生产、不请求真实通道、不发起真实资金或外部通知。其它业务插件仍按用户指示延期。198资料字段与199凭据之外，补齐原版订单详情的同步动作。

本机 Pro 公开前端 index-Dk-66xD5.js 的详情动作按异步、同步排列；order-B-UR7CeV.js 使用 order/notify，字段 trade_no 与 isreturn=1，获得 url 后打开新窗口。现 Go 仅异步 resend-notification 入队，不能把它当作同步入口已完成。

增加 POST /console/v1/{admin|merchant}/orders/{trade_no}/return-notification，严格空 JSON 对象，只从已保存且已支付的订单生成回跳签名地址，返回 url。复用现有 notification.URL、签名算法与原始存储资料；只允许本人的商户或具备订单查看权限的管理员，复核持久会话、状态及归属。禁止浏览器提供目标地址，不请求目标或通道，不创建异步任务，不改变通知状态或账本。敏感地址 no-store 且不写日志。详情能力 can_return_notification 独立于异步 can_resend_notification，字段与按钮按真实状态启用。

先写实际 HTTP/PG 和组件黑盒 RED，再实现最小后端与前端。覆盖旧异步保留、可用与不可用订单、管理员/本人/他人/委派或失效来源、空地址和非法输入、RSA/MD5签名、现有字段/任务/账本无变化、窄屏与同步打开动作的取消/迟到响应安全。根任务接主路由、OpenAPI，按固定来源复用现有环境验证；本项通过不等于全量兼容验收完成。
