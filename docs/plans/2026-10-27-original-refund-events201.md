# 平台退款事件订阅

沿用既有测试容器和数据库，不访问生产、真实通道、资金或外部收件人。投诉等业务插件延期，本项仅 refund。先真实行为 RED，再实现和验证；不以队列处理完成声称商户已送达。

本机 Pro 的 SendEvent.php、Order.php、公开 index-CgFnG600.js 可确认：API对接信息页在签名方式设置之后展示“事件消息订阅设置”，原字段 event_url/event_types，URL最长500；仅平台提交退款，不包含外部支付API退款。Order::$api只是上游调用方式，不能据此判断入口。表单参数 event/content/pid/timestamp/sign_type/sign，按原平台 RSA SHA256 canonical 签名，content保留原字段/金额格式/中文和斜杠。成功响应按原ack条件清连续失败；十个独立实际发送失败清types和计数、保留URL并通知。原版不自动重发同一任务，禁止单笔重试十次导致自动解除。

GET/PATCH /console/v1/merchant/account/event-subscription。读取 event_url、event_types(仅[]或[refund])、64位hex fingerprint、0..9 consecutive_failures、last_delivery_state、subscription_notice。PATCH精确 event_url/event_types/expected_fingerprint，CAS写入；仅原始持久本人会话，CSRF、归属、禁用/认证版本/原到期和末写复核。未知写结果只读核对，不自动再写。UI按原位置、标题、字段、说明与保存订阅按钮；明示仅平台提交退款事件，不提供延期的投诉选项。

113迁移和独立不可变来源、设置与事件证明；仅明确新建退款分类api/platform，旧历史来源不猜，不改变历史MAC canonical。可信succeeded结果证据同事务唯一捕获平台事件，重放不重复，未确认成功不派发；手动退款和后续查询确认均适用。安全httpclient保持DNS/目的地/TLS/限长/超时/禁止跳转；签名key不进入任务payload、数据库明文或日志。事务outbox与一次传输claim/result分离，已传输失败/未知记真实state而不自动重发；只对确定未传输的前置故障调度。解除邮件复用195合法managed发送能力，不伪造既有私有消息seal；额外站内notice标为增强。

根任务负责主路由、worker kind/白名单和接口文档，后端与前端独立分工。实际PG/HTTP覆盖入口分类、成功证据原子性/幂等、旧历史不回填、十独立失败/成功清零/未知不重发、配置轮换与CAS、末写过期、审计失败回滚；本地TLS或受控传输验证精确签名与ack，浏览器375/1440及上下文迟到响应保护。全部使用合成数据和既有环境。
