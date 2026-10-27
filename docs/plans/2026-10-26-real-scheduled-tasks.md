# 真实计划任务控制台与调度器（171）

> 父冻结166，1881文件，逐字节校验复制至隔离scheduler171；migration100预留，98/99由并行任务使用。此分支暂有编号gap，仅开发收据，root最终整合98/99/100完整登记链后验收。默认disabled，无生产/上游/真实支付；不启动现有出款/通知worker。

## 原证据与界限

原CronTaskInterface要求identifier/name/parameterDefinitions/execute；AbstractCronTask默认空参数。11个cron业务类和command/Cron.php可读；CronTaskRunner/adminCron加密，仅记录loader/大小/SHA，不能推断其内部并发/校验。证据文件.tools/scheduler171-pro-evidence.json留原SHA。

公开cron页index-CQrUK7ri.js SHA 0fae037e52ea4fbb39c7367d2d6f7cf2a8ccea968f3c8fc1b6f28f4f3c7c6a4d：offset2535频率second/minute/hour/day/once、interval、execute_at；3165 info；3667 runTaskNow（只安排next_run_time，不证明完成）；4420 enabled；4771 params；10118上次执行/10529状态，running时不能立即运行。原URL密钥入口与Swoole运行环境说明不直接移植为Go URL secret入口。

现cron-tasks只是未消费backoffice资料，/system/cron还显示jobs队列；新页面不得拿二者冒充调度器。保留旧记录用于只读核查，不迁移旧enabled，不补造执行历史。

## 实际registry与业务执行

只执行Go编译期registry，不接受SQL/命令/URL/类名/上传脚本。首批真实本地handler：
1. referral_daily：原Order.php邀请每日结算子能力，调用已有Store.TickReferralDaily；冻结DB业务日期（UTC+8）及limit1..100，原已付款/预算/退款/原订单证明与逐order幂等不变。与Order真实到期通知/每日返现核心分别显示，非原插件hook替身。
2. settlement_generate：原Settle.php生成列表阶段，只处理已有受信generate_only有限授权；冻结原plan ID/revision/due slot；原CreateSettlementPlanRun和GenerateSettlementPlanRun实际生成pending_review请求。不得调用含threshold_auto的全局ScheduleSettlementPlans后只报告queued；不得审批/付款。已存在原run按原slot恢复；跨slot或跨日无原run明确失败，不能换新slot。仅在完整真实执行及PG负例完成后标可用。

后续核心阶段在同一隔离目录持续实现，不以首批两个handler作为本任务完成：

- notify：实际接管原自动通知重试job的claim，首次发送与持久人工resend及原已审计jobs retry独立。按原昨日UTC+8零点至今、到期优先、最多20笔；不使用滚动48小时。每次调度冻结原job ID、下一attempt、原kind/event_key/payload摘要及有限五分钟准入；原worker在原job lease事务内消费，handler再次验证。原HTTP handler仅在确实发生HTTP交互且未成功时产生私有、与原job/lease/payload绑定的失败类型；配置、授权、证明、取消等错误不计为回调失败。失败观察与原queue Retry同事务，按本次接管后可证实的五次失败依次1分/9分/40分/1小时/终止；不把旧attempt数字伪装已确认历史。
- notify失败风险：原auto_check_notify/check_notify_count/check_notify_notice设置实际消费。仅自动重试实失败后核对最近三日原订单、最新N笔各自原job当前attempt的已证明失败，再锁商户、CAS关闭pay、追加risk-record与独立源审计；不关闭既有unknown回执查询，不扣款，不推断未知旧失败。可选邮件冻结原profile邮箱的AEAD密文，独立原stop源/MAC与start审计、jobs只增私有不可回退head；原merchant-notice部署开关和受控SMTP账户仍必需。commit claim前不I/O，末次审计写后复核原job lease；原worker复用既有SMTP sender消费。accepted仅提交成功；unknown/无result永不因换账户、重领租约或私有行丢失而重发。公网/生产均未运行。
- OrderAfter映射：原Notify.php -2每十分钟调用的OrderProcessService本体加密，未知插件hooks仍未知。可验证的Go支付入账、平台费、原返现/分账预留、paid状态、inbox/outbox与首次payment.notify在原同一事务，局部失败整体回滚，原receipt重放一次，无需新造-2资金补偿意图。可选merchant.notice原投影采用原savepoint，缺少原出生通知源时不能采用当前资料补造历史邮件；此差异保留明确边界。
- order_query：只接管普通payment-reconcile原job，保留原attempt/渠道/unknown。原unpaid_minutes最小10默认60及其后六小时窗口实际筛选，包含Go created/submitting/pending/unknown四种未付款状态；preauth及capture/release、退款、代付、返还查询独立。真实原query消费者已验证付款一次且不再次create。
- daily_report/balance_notice：真实冻结merchant列表、DB UTC+8业务日、scene/hour参数及私有进度MAC；分别登记原通知源，原merchant.notice worker消费。真实本地HTTP sink验证原job一次送达。重启接管同一intent，已登记源幂等，不能采用后来新增商户或改业务日。旧notice周期发布器在明确配置该scene后让位，未配置保持原部署行为。
- transfer：原Transfer.php是余额阈值自动转账，不能称“转账查询”。复用balance_threshold原计划及本人opt-in、阈值、最多五个整钱包选择、余额/支付/收款人/用户组/手续费/预算快照；任务冻结原plan/revision/slot及独立auto authority ID。原GenerateSettlementPlanRun实际生成冻结pending request；QueueSettlementAuto限定本次run/authority登记原job，再给原worker有限准入。不造新自动权限；无匹配授权/运行配置明确失败。实际fake-provider unknown仅发送一次，同周期重放仍只有一份request/transfer/selection。费用沿已有受控报价合同，不猜未知上游费。
- settle：真实generate_only原计划生成待审核申请。没有独立auto authority时记录awaiting_authorization，不出款；匹配有限auto authority及受控运行配置才登记原自动job准入。任务完成不代表付款完成。与transfer按原不可变definition mode分别接管旧结算扫描及消费者，不能混淆普通结算与阈值分支。
- profitsharing：profitsharing_limit/settle_query_limit两参数各自实际限制原PS/PF查询job，每类每轮安全最多100。旧query消费者实际校验准入并读取原签名结果，不新建分账/解冻意图；返还/refund查询独立。自动first-send依赖的ProfitSharingLogic.php本体Swoole加密（92176B，SHA d0f34a7e51c112aebfadd04986218277ca17fb4ec1a3c117c16ce450324f87f6），此插件分支精确暂缓，不能声称整类等价。
- order：真实冻结enabled商户列表、原UTC+8业务日与进度，按原最新已验证会员购买证明只登记group到期通知，由现merchant.notice原消费链一次发送；不会串入日报/余额scene。采用原TickReferralDaily结算足额预留、已到期、本身逐订单幂等的奖励（每批100、最多100批；满上限明确失败而非假完成）；独立referral_daily策略已配置则由其控制该能力，旧referral loop在Order接管后让位。到期组fallback由merchantpolicy.ReadEffectiveGroup真实消费，支付/结算最后等待后时钟仍校验；黑名单失效由backoffice.ActiveBlacklistReason真实消费，通道日限额由既有日期分桶/订单用量核验，非每日清空旧账。保留过期邀请码/订单、账务、幂等证据，原物理删除不移植；客服插件日志清理和cron_daily未知hook不标已实现。原真实到期证明、同日幂等、原worker消费与旧publisher让位均有本地PG/HTTP测试。
- check：原调用RiskCheckLogic::execute，内核Swoole加密79744B SHA e1555026a118ab6070650189a964a8ffd3feb0bcd1aa095669e2a6c6d154592b。**这是仍未完成的风控核心，不是用户同意永久跳过的其它业务插件**；本阶段明确unavailable，待root后续来源可证风险规则及真实消费者接入后开启，不发明规则或安排noop。applyrefresh/complain才属于已明确暂缓的进件/投诉外部插件。原11任务不得宣称全部完成：8原核心已真实接入，另2本地子任务，1风控核心待接入，2业务插件暂缓，PS自动首次发送未知分支仍单列。

未配置的新策略对已有周期worker显示legacy_managed/未接管，不能显示“已关闭”却继续旧循环。明确配置后开关与频率由真实执行器消费；callback与原显式人工查询仍保留独立原权限。没有第二套发送器，不周期reset dead、不重置unknown、不生成新的资金幂等号。

## 时间、配置与API

专用本人原super持久会话GET /console/v1/admin/scheduled-tasks及/{task}；PATCH精确{enabled,schedule:{frequency,interval,execute_at},params:{原静态定义键},expected_fingerprint}；POST /{task}/run精确{expected_fingerprint}只安排一次，返回pending；GET /{task}/runs规范limit/before_id。严格duplicate/unknown/null/trailing/query/规范key验证，所有响应no-store。run ID/revision是完整字符串，参数/间隔为整数。无admin身份、租约token、原params之外的私有业务ID/证明返回。原未接管、不可用、待运行、执行中、真实终态分别展示。

时间单位与原页一致，明确UTC+8显示；execute_at须含时区且秒精度，interval边界明确。循环按固定anchor计算最新到期slot，跳过错过旧槽，不突发补跑历史；once只执行一次。手动触发不推进自动槽。数据库clock决定due、lease和完成时间。

配置默认未配置、disabled/revision0。首保存创建HMAC配置+非秘密审计；MAC覆盖taskkey、enabled、schedule、params、revision/generation、saved_at和私有头；CAS绑定配置修订和最近manual请求，不因周期auto头推进持续失效。策略行禁DELETE，缺行而有原审计不能当未配置。改配置取消尚未claim的旧pending；任务正在执行时按task锁串行，不能把停止当回滚已提交原业务。本人super/CSRF/原会话审计后最终DBclock复核，审计失败回滚。

## 持久事实与并发

migration100只追加：
- scheduled_tasks：配置当前行、修订/时间/MAC；不消费旧cron-tasks。
- scheduled_task_runs：不可变执行意图（配置快照/MAC、业务日/自动槽或manual、受信业务快照、created_at/intent MAC），可变lease token/fence/until/started_at；唯一自动槽防多worker重复。
- scheduled_task_results：原run终态、有限counts/error code、真实完成时间/MAC，不可变，不保存raw exception/外部响应。
- scheduled_job_admissions/claims：原job/下一attempt/有限有效期与原intent的MAC承诺，消费时绑定原lease token摘要并在原queue同tx登记。换代策略不采用旧准入；在途原lease可完成但不能以旧准入获取下一attempt。表/行/子事实丢失不能降级legacy，独立configure审计及migration100登记共同锚定。
- scheduled_notification_failures/stops/mail_claims/mail_results及jobs两个nullable私有head：原lease下已确认交互失败、关闭pay原源及唯一邮件提交承诺；旧row不回填。MAC/AEAD、不可变触发器与独立非秘密审计互证，不能以完整旧合法行或双表丢失恢复旧预算/新发送。私有事实不加入通用jobs/audit/runtime列表DTO。

抗回退不只校验MAC：配置revision=本人admin原configure+manual-run提交数，generation=configure数，逐修订序列核验，same-value提交仍是新修订；auto槽/last/manual头另与原run及独立intent审计逐项匹配。执行原epoch与独立lease提交数/随机token不可逆SHA256承诺/首次真实开始时刻匹配；checkpoint与独立index/进度MAC承诺匹配；已完成result不能恢复旧pending再执行。通知失败head与最新不可变failure和其独立观察审计匹配。audit从不保存可使用lease/authority token、私钥、邮箱明文、SMTP或AEAD凭据；原真实operation/system/runtime/jobs GET测试确认没有内部恢复材料；意外数据库错误仅复用原固定SQLSTATE运行日志，不输出原异常字符串。

同task持久lease和PG session advisory lock共同保护：claim/renew/finish用数据库clock及token+fence CAS；旧owner过期不能finish。session lock贯穿真实handler，旧进程活着但lease过期时新worker不能并行执行；崩溃后连接释放，过期lease只接管原run意图，不产生新幂等号。lease控制不能替代原逐订单/原结算slot金融幂等。锁失效/timeout不报成功；错误只显示固定公共码。

独立复审修订v2：原Queue最短租约1秒，真实scheduled_job_claims最后写延迟1.2秒可造成已过期running/attempt/claim提交，handler后续拒绝仍会白耗原重试次数。现lease用实际clock_timestamp在领取时出生，策略SHARE锁等待不提前消耗租约；原候选到期筛选也用真实时钟。原Queue在所有claim/admission写完成后、commit前复核原job ID/state/token/attempt/kind/event/payload与数据库时钟lease；Store同时复核有限admission及原lease。跨租约全部回滚，0已提交attempt/claim/外部调用；正常策略锁等待后仍可领取完整新租约并实际消费，未配置无Policy旧路径正常positive保留。migration100、预算、原任务频率/业务时钟与旧proof格式均未改；旧交付清单/RED/收据保留，新v2另列。

## 调度入口与UI

新增cmd/task-scheduler，GOEPAY_TASK_SCHEDULER_ENABLED空/false直接退出且不连DB/读凭据。true才启动registry轮询，支持-once供真实本地测试。自动job登记还需独立GOEPAY_TASK_SCHEDULER_AUTO_QUEUE_ENABLED及与原auto worker同条件的受保护runtime身份，只调用无I/O的QueueSettlementAuto，不调用provider发送方法。旧独立worker部署开关仍默认disabled；未配置新策略时行为保持，明确接管后由相应policy控制真实消费，不能把readonly metadata冒充等价。

/system/cron用新专用TaskSchedulerPage替代原资料+jobs主视图。任务卡与原名称/identifier/频率/参数/启停/上次下次/状态/失败原因/立即运行对应；确认/取消/焦点/Esc/busy、unknown手工刷新CAS、client+csrf上下文stamp、375/桌面、fullIDs。未运行显示未执行，pending不显示完成，不提供无法执行的URL复制/假Swoole状态。

## TDD与验收顺序

1. 先真实PG/HTTP RED：专用目录/默认disabled、原super与role/delegated/CSRF/CAS/audit、strict DTO、旧资料不被消费、manual pending无业务执行；migration100真实登记后保原行列/旧proof/no enabled backfill。
2. 再实现静态registry/时间规则/持久MAC配置与API，离线单位验证时间边界与完整ID。
3. 先写lease/两worker/过期旧owner/锁等待/重启接管/失败不可假成功RED，再真实claim/renew/finish；驱动原generate_only真实pending请求，重复/恢复不重复金额，原未授权/过期/撤销/threshold_auto拒绝。
4. CLI默认disabled不连DB，-once真实消费manual/due；每日返现保原DB日界，不能用未来日期绕过原资金校验。
5. UI模型/组件与浏览器RED→GREEN，桌面/375，权限/cancel/focus/late GET/POST/CSRF原地变化/409/unknown不重试，大ID；仅vue-tsc --noEmit。
6. 影响集：原referral tick、结算生成/原授权/原预算/退款/业务日、jobs与所有旧cmd默认禁用、super原session、cron元数据只读。源清单parent oldSHA/newSHA、所有真实RED/失败/GREEN单独保存，交独立review。只交补丁，不提交/推送/冻结。
