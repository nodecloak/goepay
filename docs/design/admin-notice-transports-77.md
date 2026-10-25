# 原管理员通知运输77

本地明文依据：`epay_pro/app/lib/MsgNotice.php:125–143、272–378`、`TelegramConnect.php:153–186`、`QqBot.php:42–47、76–146`。76已固定到90，1..76迁移不得修改；这里只做下一增量。当前实际部署89/schema75，新代码只在本机临时TLS服务和隔离PG中测试，不访问生产、不联系真实机器人或收件人。

原`sendAdminMessage`按`msgconfig_risk`选择：1且URL存在时webhook、2且chatID存在时Telegram、3且openid存在时QQ Bot；未进入分支则邮件回退。已选择的运输报错后返回失败，不转另一运输再次发相同消息。Webhook支持钉钉markdown、企业微信markdown、飞书/Lark text；按精确主机判断，不能沿用原substring接受仿冒域名。Telegram使用bot token的sendMessage、HTML解析模式，QQ Bot先client credentials取token再/v2/users/{openid}/messages发msg_type2 markdown。

Go保持原选择和成功应答字段，增加严格TLS、禁止重定向、固定允许主机/路径、公共DNS验证及拨号固定、请求/响应/时间限额、固定脱敏错误。原PHP禁用TLS证书/主机校验的做法不继承。构造/校验配置不联网，不在日志、数据库明文、HTTP摘要中暴露URL token、Bot token或secret。QQ webhook验证、Telegram登录/账户绑定属于其他独立功能，不因本发送器而冒称已完成。

实施顺序：

1. 先实现独立transport适配器及纯配置选择，实际本地TLS红绿分别覆盖四Webhook主机、Telegram表单、QQ token与最终消息；坏证书、重定向、错主机/路径、伪造成功、超大响应、取消及错误脱敏先测。配置只能来自显式允许的合成/加密运行档案，不能读取生产材料。
2. 在77迁移新增route版本/哈希来源；保留76既有SMTP claim的全部旧列、HMAC、审计和状态，不回填或重新发送。新运输claim绑定kind、目的地、发送账户/配置指纹及原停用job，首外呼（含QQ取token）前原事务提交。删除route来源或降为旧SMTP不能通过旧回退。并发只一次；未知或崩溃不因换运输/收件人/凭据再次取得发送权。
3. worker只在既有明确开关启用时领取该通知kind；缺配置不外呼，默认关闭。选择器和profile字段按原名映射，受保护配置与原普通资料分开，不把通用明文字段当可用secret。后台显示实际通知运输及pending/attempted/accepted/unknown，accepted仍仅上游服务受理。
4. 验证真实76形状及旧SMTP claim无重置升级，原Source查询、停用/恢复和账本检查不回退；再固定新候选完整验证。90仍独立完成原76全量验证，不修改其文件。

这一项只补管理员异常通知运输；商户各通知场景、每日汇总/语音以及其它仍列入剩余核心清单的分支必须继续逐项验证。它不构成整个Pro通知体系、全部菜单或生产替换完成声明。

## 已落地的配置与持久契约

原字段`msgconfig_risk`、`msgtelegram_chatid`、`qqbot_openid`、`mail_recv`、`mail_name`已加入系统设置的允许项，保留原字段名及中文含义。worker每次新通知以一个MVCC语句读取当前五项；已保存的空目标覆盖进程默认，未保存字段才使用进程默认。重复、非字符串（包括null）或非法规范值阻止发送；普通唯一索引限制重复设置，读端额外防御异常导入。保存设置不发送测试消息。

进程默认映射为`GOEPAY_ADMIN_MSGCONFIG_RISK`、`GOEPAY_ADMIN_MSGTELEGRAM_CHATID`、`GOEPAY_ADMIN_QQBOT_OPENID`、`GOEPAY_ADMIN_MAIL_RECV`、`GOEPAY_ADMIN_MAIL_NAME`。受保护凭据为`GOEPAY_ADMIN_MSGROBOT_URL`、`GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN`、`GOEPAY_ADMIN_QQBOT_APPID`及`GOEPAY_ADMIN_QQBOT_APPSECRET`，不能由通用明文settings提供。两个原邮箱字段均为空时保留旧`GOEPAY_ADMIN_NOTICE_RECIPIENT`邮件默认。SMTP继续使用既有严格TLS配置。整体仍要求`GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES=true`；默认关闭时不读取设置或凭据，不加入通知任务kind。

77仅给`channel_stop_notice_claims`增加`route_version`、`route_mode`、`route_scope_hash`、`route_hash`。V0旧SMTP所有原列、HMAC和审计原样保留，新列为0/空/NULL，不回填；V1按新域绑定模式、收件目标HMAC和发送档案HMAC。V1降为V0不能通过原MAC。结果审计继续绑定完整claim proof；原任务租约、停用事件及原自动付款/TF来源校验均保留。公开状态增加`notice_transport=smtp|robot|telegram|qqbot`，未领取时省略，不暴露目标、URL、token或档案摘要。

QQ token缓存仅存于当前发送档案内，并发刷新合并；相同有效档案复用，换凭据不复用。期限从取token请求前计算，提前过期且不超过上游期限；缺少expires_in时才使用原7200秒默认，已给出的期限必须为有上限的规范正整数。取token和消息均在永久claim提交后执行，失败不重试消息。Webhook缓存复用还验证当前原始目标，避免等价URL写法使旧缓存拒绝新目标。

77不增加表，完整schema77仍为182张public表（含迁移元数据）、22项CLI检查。Telegram代理、QQ webhook/登录绑定、商户全部通知场景继续单列后续范围。新候选必须重新验证，不能沿用90全量结论。
