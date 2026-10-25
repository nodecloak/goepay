# 原管理员业务提醒83（schema82）

本地明文依据：Pro `app/lib/MsgNotice.php::send/get_msg_tpl`、release `user/ajax.php` 与 `includes/functions.php` 的注册分支、`user/apply.php` 提现申请、`user/ajax2.php::addDomain`。不访问生产、实际通知服务或真实资金。82已补原pay审核状态；其1335文件阶段快照正在独立回归，不编辑快照。

## 原语义

四个核心场景：`regaudit`（仅新注册pay2）、`apply`（商户手动提现申请）、`domain`（商户提交待审核域名）、`internal_transfer`（商户提交站内转账）。使用80已核对原PHP的同名模板。管理员uid0开关是0关闭/1邮件/2机器人/4Telegram/5QQ，internal_transfer读取msgconfig_domain。这与77异常告警msgconfig_risk的0邮件/1机器人/2Telegram/3QQ不同，不能复用数值或未选中运输时回退邮件。

只增加三项非秘密设置msgconfig_regaudit/msgconfig_apply/msgconfig_domain。保留原mail_recv→mail_name、msgtelegram_chatid和qqbot_openid目的地；已显式保存空值不得用进程默认补回。机器人URL、Bot token/AppSecret等继续来自77同名受保护运行配置，不进入通用CRUD。缺目标/运输配置不伪造成功，不换运输重发。

## 来源和存储

新businessnotice领域独立按用途派生AES-GCM/HMAC键，共享80纯模板与已验证的SMTP/机器人/TG/QQ协议适配器。schema82新增admin_business_notice_events/claims/results三表，原backoffice_audit与ledger_request_events仅加可空业务来源事务/场景元数据，旧行NULL、所有原列与1..81迁移不改。暂不处理已明确暂缓的投诉、进件等业务插件来源。

发布器只接受原audit ID或request-event ID，重查原身份/动作/父资源/事务xid8；不能接受HTTP任意正文、目标或跨商户参数。注册来源必须在邮箱证明消费后、原注册审计pay2同事务；域名来源是本人首次提交或改名后重新待审核的原审计；资金来源是本人新建pending事件，actor精确merchant:<id>，排除自动结算和管理员代建。支付领域提供验证过的不可变原申请/费用/收款类型事实，通知领域不自己解密或猜测费用、当前账户。未知原收款类别明确显示“未知”，不伪装真实支付产品。

事件AEAD/HMAC包含原来源、场景、捕获开关、网站名、原业务时间、固定模板输入和唯一原job ID。source同事务出生校验；后续按原不可变审计与账务事实重验，审核/付款/删除域名等后续正常操作不使历史消息失真。每源最多一事件；关闭的原事件无job，之后开启不补造历史来源。可选投影使用SAVEPOINT，存储失败不回滚原注册、审核或资金预留；完整性检查报告缺失来源，不自动修复/补发。

## 发送约束

使用独立默认false的GOEPAY_ENABLE_ADMIN_BUSINESS_NOTICES。关闭时不读取通知凭据或设置、不加入该job kind；API生产者不接触Bot凭据。每个事件只允许一个永久claim，绑定原job/租约、原事件/源MAC、运输、目的地HMAC和实际发送账户scope HMAC，首个外部请求（包括QQ取token）前提交claim和审计。

事件捕获运输模式；首个执行若当前模式与原模式不同，则永久not_sent，不转另一运输。与77一致，首个claim前按当前管理员目的地/受保护发送账户选择；不声称捕获了API进程未知的历史Bot目标。首次claim以后，改收件人/凭据/模式/任务重领或丢ACK都不能再次发送。纯工厂只构造发送器；模板先渲染封存，工厂不能改原消息。QQ公开AppID独立与发送器账号核对，目标先纯校验。结果accepted仅上游受理，外呼后错误/超时为unknown，缺ACK同样unknown；晚到ACK使用有限WithoutCancel事务保存，不靠当前配置恢复发送权。

## HTTP和后台

GET /console/v1/admin/business-notices 为管理员super/operator只读游标分页，规范before_id/limit，拒重复/未知参数；返回场景/源标识/北京时间/捕获状态/任务/运输/受理状态，无原邮件、账号、URL、正文、凭据。无手动重发API。挂在现有系统设置页面的提醒记录区域，不增加原菜单必需项；设置项保留原字段名，并按原模式显示中文选项。

只读CLI新增invalid_events/invalid_deliveries/unknown_deliveries三项，检查事件、原审计/申请、原job、claim和结果MAC及孤立来源；unknown诊断不修复。预计schema82有197张public表（含迁移元数据）、31项CLI计数、37个Go包、既有8个运行二进制。以上数量须用后续实际固定候选验证，不能当作已通过结果。

验收：四源出生/重复/历史不补发/后续正常变化，0/1/2/4/5与risk不同命名空间，原模板与80已核对的PHP一致；并发一次、错租约/源/任务/角色/目的地、工厂篡改、丢响应/晚ACK/配置变化无第二次；默认关闭零凭据读取/零HTTP、管理员分页/手机桌面、原列及全表行保留、完整候选全库/race/原PHP/本地升级与恢复。
