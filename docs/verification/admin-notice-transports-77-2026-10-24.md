# 通知运输77当前实现记录

仅主区开发，不在固定90或实际部署89中；已接入持久route来源、原字段配置选择、worker与后台运输显示。尚未完成新候选全仓库验证与部署。未访问任何真实Webhook/Bot/SMTP目的地，所有看似官方主机均由测试client固定拨号到临时127.0.0.1 TLS listener，并使用自生成证书和测试信任链。

新增包`internal/adapters/adminnotice`，主区现34包；固定90仍33包，不能更改其包清单或源码。遵循本地MsgNotice/TelegramConnect/QqBot明文请求字段：钉钉markdown、企业微信markdown、飞书/Lark text，Telegram原四项表单，QQ client credentials后以QQBot token发送msg_type2 Markdown。

现有证据在`.tools/settlement-auto73-next-notice77-*.log`及邻接RESULT：

- `webhook-red`真实失败后`webhook-green`1.178秒通过四种真实TLS正常响应及仿冒主机/错误路径/重复query等无IO配置校验。
- `webhook-security`1.808秒通过HTTP503、重定向不跟随、重复JSON、假成功字段、字符串0、超大/畸形/拼接响应、取消/错误目的地和坏CA。错误固定脱敏，发送器String不泄露URL token。
- `webhook-control-red`发现编码TAB仍被接受，改为所有query值仅允许可见ASCII后`webhook-control-green`1.566秒通过。原样保留红日志。
- `telegram-red`后`telegram-and-webhook-green`1.808秒，实际表单/path/HTML标题/chatID、严格ok布尔及错误不重试。
- `qq-red`后`qq-and-transports-green`1.918秒，实际两次本地TLS请求核对appId/clientSecret、Authorization、Markdown和消息ID；token/消息失败分别只请求一次/两次，错误和配置均脱敏。

共用HTTP client克隆时去掉cookieJar、禁止重定向、10秒限时；默认使用既有公共DNS校验及拨号固定客户端。必须验证TLS链，拒绝显式InsecureSkipVerify；JSON响应32KiB上限、UTF8/递归深度/重复key/额外文档严格校验。支付发送行为不变，SMTP只新增纯路由指纹/目标校验方法。后续QQ档案缓存与离线race已补，详见下文；全仓库77仍待新候选完整执行。

## 持久路由、原设置和worker集成

- `domain-red`先复现原邮件校验拒绝Telegram，`domain-green`14.310秒验证六消费者只一次、未知后改运输/账户/目标不重发、V1降为V0后HMAC与检查器报错。`qq-domain-tls`12.434秒使用独立连接证明首个QQ取token前已可见已提交claim，token/消息各一次，公开状态qqbot，审计不含secret/原目标。
- 真正只应用1..76的历史形状，先生成有效原自动付款错误和未知V0邮件claim，再执行77，逐表校验全部原列哈希及旧proof/结果，原unknown资金不变且不重发。181原业务表及额外local_plan_grants测试表均保留。`reviewed-upgrade-green`13.217秒通过。早期23505与错误181总数属于夹具构造/计数错误，保留失败日志，不当作产品行为红绿。
- `worker-red`后`worker-green`0.837秒覆盖原risk选择、缺目标mail_recv/mail_name回退、坏选择不换运输。`settings-red`/`runtime-red`后`settings-runtime-green`中payment41.332秒、backoffice4.823秒、worker0.322秒；实际读取数据库当前五项，同档案复用、换凭据换档案，失败读不使用旧缓存。默认关闭时不读取配置/凭据。
- 导入null被误作空目标的真实红绿：`settings-shape-red`后`settings-shape-green`11.317秒，null/数字/对象/数组均拒绝。原唯一索引继续验证，额外显式漂移夹具验证重复行读端拒绝。
- QQ八次独立通知原取token八次的真实红绿：`qq-cache-green`2.457秒后并发只取一次，档案隔离、坏期限/矛盾错误不受理，另有真实短期限过期刷新。bytea_output=escape先实际22023失败，改用PG bytea输入转换后`bytea-cache-green`中payment14.324秒/adapter2.053秒通过。等价Webhook地址旧缓存拒绝当前目标先真实失败，补当前精确目标校验后worker回归通过。
- 原76与77联合回归`regression`184.080秒通过。后续原设置、缓存和生产内部命名清理继续由新候选重新验证，不将此日志当作全部后续源码的全量结果。
- 命名清理与最终夹具修正后的`naming-regression`：adapter3.654秒、mailer1.499秒、worker0.942秒、backoffice5.597秒、payment44.257秒全部通过，选择器、当前目标缓存、旧76升级、bytea两种输出和新来源均覆盖。此为77定向回归，仍不等同34包全仓库测试。

77只增加四个route列，旧MAC域/原列不改、不回填、不重发；新MAC绑定模式、原目标及发送账户的HMAC。结果只表示attempted/accepted/unknown。配置映射、缺省行为和安全边界见[77设计](../design/admin-notice-transports-77.md)。

## 前端与独立静态/race证据

`.tools/main77-frontend-ce4b6b9296574c07a1222b17fe87e3a3/RESULT.json`：67文件299单元、typecheck/build通过，146 mock浏览器/9真实部署opt-in跳过/0失败/0flake（34.687秒），前端清单前后相同；使用已有安装，非fresh npm ci。375/1440真实浏览器截图已查看，保留Pro侧栏、面包屑、页签、表格及明确恢复确认，显示实际通知方式；accepted不等于送达。JS570.34KB的>500KB提示保留。

`.tools/notice77b-race-04b211139d894b0dad2d4f29379c2766/VERIFIED.json`：895文件独立Go副本只读挂载，实际Docker network=none、退出0、无OOM、前后SHA相同；adminnotice/mailer/worker三包Linux race57通过事件。两个SMTP注册/恢复PG opt-in因该容器无数据库跳过，不算完整仓库验证；相同副本Windows34包vet/build均0。该副本早于后续仅历史测试表数修正及生产内部命名清理，最终候选仍须重新全量运行。

下一步为新固定候选fresh前端/PHP/源扫描、34包Windows完整PG及Linux完整PG race、无重置部署、实际9浏览器与2通道本地模拟、182表完整备份恢复。Telegram代理、QQ webhook/账户绑定、商户其他通知场景尚不在本项范围；不得把专项或旧89/90组合冒称77全量通过或全部Pro兼容。
