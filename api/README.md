# 接口文档

真实计划任务使用本人原super的 `/console/v1/admin/scheduled-tasks`、`/{task}`、`/{task}/run`、`/{task}/runs`。配置与手动安排均需原CAS、CSRF及末次等待后会话校验，手动返回仅pending。静态registry不消费旧cron资料，不接受命令/SQL/URL；未配置的原worker显示未接管，已配置后真实开关/频率参与原job lease准入，启用不授予资金或外发权限。原Check风控核心尚未接入，进件/投诉插件暂缓，分账自动首次发送未知分支单列，不能把本阶段称原11任务全部完成。完整ID是字符串，列表仅公开状态/真实起止/有限计数，不返租约、授权或内部证明。见[来源、执行链和边界](../docs/plans/2026-10-26-real-scheduled-tasks.md)。migration100在隔离开发分支，须由root连同并行98/99完整登记验证后整合，不是部署声明。

管理员商户密钥独立使用本人super的 `GET /console/v1/admin/merchants/{merchant_id}/credentials` 和 POST子路径 `reset-md5|generate-rsa`；仅接受 `{expected_fingerprint}`，拒绝query、未知/重复字段和非规范ID。返回商户ID/修订均为完整十进制字符串。GET只提供当前公钥、算法、是否配置MD5及HMAC状态指纹，不提供已有MD5或私钥。新密钥、修订和无秘密审计原子提交；会话在锁等待和最后审计等待后复核，降权/撤销/过期不能提交。

重置MD5生成32位随机十六进制，只保存AEAD密文；RSA固定2048位、PKIX公钥及PKCS8私钥，只保存公钥。原算法保持不变；仅成功响应一次返回新MD5或RSA私钥。并发同指纹只有一次成功，丢响应后旧指纹409，禁止自动刷新指纹后重发。前端关闭或切换商户清除一次性显示，生成后清除未保存密钥草稿，普通资料草稿保留；既有商户本人RSA流程不变。原MD5+RSA兼容模式另项核对，此增量不宣称该模式已对齐。

用户组 `info` 恢复原“随机可用轮询组”：`{"alipay":{"channel":"-3","type":"roll","rate":"99.50"}}`；管理员保存仍使用原资源接口。随机组只从同方式、全局商户0、启用轮询组中选取，继而使用该组原策略和限制。失败不换组，同号请求保留原组/通道。目录超过1000组时明确失败，不截断选择范围。

轮询资源新增可选布尔 `default_route`，缺失按旧Go默认true处理；true范围内每商户/方式仍仅一个启用默认组，false可配置多个命名组，供用户组指定或随机选择，不能隐式覆盖原继承路由。未知或非法布尔值拒绝。既有路由、计数和请求不回填、不换号。Pro UI声明随机选择；Release参考代码SQL实际LIMIT1，本实现与该参考限制的差异见[设计边界](../docs/plans/2026-10-26-original-random-roll-groups.md)。

管理员商户资源的原字段 `settle` 为字符串 `"1"` 开启、`"0"` 关闭，保存遵循资源 PATCH、角色权限与 CSRF；商户本人不可修改。目录 `dstatus=settle_1|settle_0` 与列表的“结算”状态使用同一规则。缺省兼容此前 Go 的开启行为；原 `settle` 优先于旧 `settle_status` 别名，明确无效值不能放行。兼容商户信息接口的 `settle_status` 返回实际商户权限，并不代表用户组或全局规则均已满足。只读 `settlement_permission_revision` 不接受请求赋值。

管理员新增商户（原“添加商户”）可不传 `api_key`：服务端按重置MD5同一格式生成32位随机十六进制密钥（AEAD 密文保存，不在响应中返回，需要时由super重置MD5或商户本人重置后一次性查看），`signature_algorithm` 缺省 `MD5`；显式传入的密钥仍须至少24字节。同一请求可带仅新增可写、从不返回的字段：`fee_mode` 为 `"0"` 余额扣费（缺省，不写策略）或 `"1"` 订单加费（写入商户首个买家加费策略）；`settlement_type`（`alipay|wxpay|bank`，缺省 `alipay`）、`settlement_account`、`settlement_name` 及微信必填的 `settlement_app_id` 组成首个结算收款版本，账号与姓名同时留空表示不使用平台代收。收款按“结算收款资料”同一规则校验，受 `settle_*` 开关约束，需 super/operator，与商户、账户及审计同事务写入；任一项被拒则商户不会创建。这些字段不进入商户 profile，编辑时提交返回403；之后手续费模式由商户本人在“手续费承担设置”中修改，收款在“修改结算收款资料”中修改。

关闭商户结算权限后，新提现、代付及未发送批次/结算出款均在事务内拒绝；原请求号重放和原结果查询仍可用，不会自动重发。关闭后重新开启不会恢复旧的未来生成/付款授权，须明确重新授权；历史原资金、费用、请求和证明保持原样。此字段不改变退款规则。

新增管理员订单操作：`/console/v1/admin/orders/{trade}/admin-status|archive|restore|manual-complete|manual-refund`。前四项使用原请求号/原因/后台审计修订，状态标记不覆盖支付事实，删除保留财务记录。补单只核验原单，202不能当完成。手动退款与通道API退款独立，必须确认已线下退还；首次变更无论全局开关都需本人super的短时资金授权，资金与内部HMAC结果/审计同事务。详见OpenAPI；同支付方式API退款仍使用原`/refund`与未知结果保护。原UI职责和尚未对齐项见[全页面复核](../docs/verification/pro-ux-review-2026-10-25.md)。

模板使用admin本人super专用 `/templates`、`/templates/{type}/{name}/config`、`/templates/{type}/selection` 与PNG/JPEG图片接口；原通用template-configs只读。type为home/pay/qrpay，内置default/compact；配置保存和模板切换共用同类型revision CAS。图片重编码入库，只有当前选择的有效配置所引用图片可从 `/template-assets/{sha256}` 公开读取，撤回即404且no-store。配置实际被原支付身份/token校验后的渲染消费；不能上传PHP、HTML或任意脚本。

打印平台设置 `/console/v1/admin/printing/settings` 仅本人super GET/PUT，商户设置 `/console/v1/merchant/account/printing` 本人GET/PUT；字段完整提交并带expected_revision，密钥/设备仅写入AEAD后返回掩码。订单收据为 `/orders/{trade}/receipt`，原订单记录 GET `/orders/{trade}/print-jobs`，手工创建 POST同路径，三个显式字段external_no、previous_job_id字符串、acknowledge_duplicate布尔。`202`只表示原意图持久化，`accepted`仅网关受理，均不是实体打印完成。未知结果不能自动重发；丢响应保留同一external_no。本人 `/print-jobs/{id}/recover` 只恢复原未发送dead处理任务，绑定expected_task_id且幂等，不换打印请求号、不新增收据。没有HTTP dispatch入口。独立打印worker仅显式local-printing-fake profile、enable标志与sandbox同时满足才运行本地模拟；外部打印执行不开放。

结算计划使用admin本人super专用 `/settlement-plans` GET/POST、`/{id}` GET/PATCH、`/{id}/authorizations` POST、`/{id}/runs` GET/POST、`/settlement-plan-runs/{id}` GET与`/settlement-plan-authorizations/{id}/revoke` POST。定义只接受name/mode=generate_only/schedule/merchant_ids（1–100个显式ID），新建默认关闭，修改用expected_revision且关闭旧授权；不接受enabled、自动付款、threshold或百分比留存。schedule六字段必需：frequency=hour/day/once，interval为数字字符串（hour1–168/day1–366/once0），anchor_at为带时区RFC3339整秒，execute_hour/minute/second为整数且非day必须0。按UTC+08业务日和固定D1资格，运行完成仅表示生成待审核申请并冻结，不表示审批或付款。原审批及出款授权仍独立。

计划授权POST始终要求原动作资金授权，**即使全局enforced=false**也不能无码创建。原body八字段全必填：expected_revision、expected_authorization_id、valid_from、expires_at、max_item_gross、max_run_gross、max_day_gross、max_day_items；ID、修订、金额分与数量均十进制字符串，时间为RFC3339。挑战绑定原计划范围/政策/参与/收款配置快照及精确上限；挑战成功不保证最终资格，事务内复核。紧急revoke只需本人super+CSRF及空JSON，不需资金grant。扫描POST仅`{expected_revision}`，只扫描或恢复当前合法due slot，返回202原run，不能指定任意日期；未知结果查询原run，不换号自动重提。列表仅after_id/limit，返回items/next_cursor，其他接口禁query。

商户本人 `/account/settlement-schedule` GET/PUT管理默认关闭的参与，拒代登录和merchant_id选择器。PUT为`{expected_revision,enabled,recipient_config_id}`，绑定当前masked recipient.config_id。GET的recipient_config_id是原已绑定配置（初始0），嵌套recipient是当前可选配置；二者不一致需明确重新确认，旧同意不延用至新收款配置。保存设置不创建申请。专用页面为admin `/settle/plans`和merchant `/account/settlement-schedule`，普通cron资料不自动触发资金执行。

邀请返现使用本人商户的 `/referral/overview`、`/referral/token`、`/referral/token/{rotate|revoke}`、`/referral/invitees`、`/referral/rewards`；拒绝管理员假扮或代登录会话。令牌CAS变更仅一次返回完整bearer，默认90天有效，后续只读掩码；新链接使用fragment，前端在路由和请求前移除地址中的promotion_token，仅当前注册表单预填，OTP与用户明确确认后才作为JSON `promotion_token`提交。注册准入`invite_code`独立。关系列表不返回OTP、邮箱或证明，奖励金额/汇总均为分字符串；累计已发、已撤、退款暂存不代表可提现余额。`/user/invite/inviteInfo` GET与`inviteList` POST为同安全DTO/Go身份的精确入口别名，不宣称加密PHP完整JSON或PHP会话兼容，也不扩宽Cookie Path。

超级管理员通过 `/referral-policies/{scope}/{scope_id}` GET/POST管理普通支付政策。六字段完整CAS：expected、enabled、rate_bps、mode、basis、refund_policy；mode可选immediate/daily，basis可选fee/gross/profit，能力须严格为true。奖励只由原订单手续费出资，gross手续费封顶，profit来自可信通道costrate配置的估计利润（非实际账单），不能由支付请求传入；任意成功退款撤销整笔原奖励，unknown先暂存。日结按原单固定条款次日北京时间待专用调度器，默认调度关闭，不能当作已到账。

余额会员推广政策独立使用 `/console/v1/admin/membership-referral-policies/{scope}/{scope_id}` GET/POST；仅本人super及独立资金授权，准确三字段expected/enabled/rate_bps。global/0、group/正组ID，null继承、false/0明确覆盖；尚未配置global的GET返回id/revision0和null字段，表示缺省关闭，POSTglobal仍必须明确bool和整数。会员购买只消费其原P收入并即时奖励R，0<=R<=P，不能借普通手续费或其它会员收入；原购买一次扣款/一次奖励，旧购买不追发。委托商户会话不能购买会员，管理员必须经过原购买资金授权。外部tid4购买仍未开放。71/72仅工作源码，须以新总候选验收和部署交接记录确认实际运行版本。

五列批次文件使用 `POST /console/v1/{role}/transfer-batch-imports/preview`：JSON传 `format:csv|xlsx`、标准base64 `file_base64`，管理员另传规范 `merchant_id`；商户不能选择他人。文件最多512 KiB、100条，UTF-8 CSV或XLSX，旧XLS暂不支持。标题为“序号、收款账号、收款人姓名、转账金额（元）、转账备注”；金额为元十进制，响应 `total_cents` 为分字符串。拒公式、外链、宏、数值账号与损坏/超限ZIP/XML，不能猜测恢复Excel已丢失的账号精度。账号/姓名预览为掩码，`draft_items`完整数据仅供同认证浏览器当前内存，响应禁止缓存；文件、原账号/姓名不写持久存储、日志或审计。上传只校验，不创建批次、预留资金或付款；显式应用草稿后仍走原批次确认、资金授权及独立执行。见 [原始格式证据](../docs/design/remaining-console-evidence.md)。

独立用户标识授权使用 `/identity-lookups/capabilities`、创建请求、原号状态查询和显式一次性 `reveal`，见 [授权设计](../docs/design/identity-lookup.md)。仅直连微信/支付宝标准网页授权；本地沙箱明确返回不可用原因。授权回调不建立后台会话、不创建支付订单，结果不写浏览器存储。公开授权链接含一次性能力，代理按路径脱敏。

[console.openapi.json](console.openapi.json) 为当前 Go 双后台的 OpenAPI 3.1 文档，覆盖登录、订单/退款/预授权扣款与解除、商户通知重发、转账、资源、审批、个人资料、报表和任务接口。路由使用 `{role}`，角色 Cookie 必须对应；商户数据隔离、管理员权限、服务端 schema 和产品能力仍是最终裁决。它不是原 PHP 全部后台接口的一一兼容声明，也未覆盖所有特殊业务动作。

工作源码新增独立自动付款授权73（尚未部署）：`/console/v1/admin/settlement-plans/{plan}/auto-authorization`读取、`auto-authorizations`准确11字段POST签发、`auto-authorizations/{authorization}/revoke`严格空对象撤销。生成授权本身不允许付款；签发始终要求本人super独立资金确认及明确费用范围，不因全局确认关闭而跳过。钱包通道目录仅列明确商户准入、具体产品、AppID和付款账户掩码；机构另收费不纳入G=N+F限额，不保证机构成本封顶。独立执行器默认关闭，队列只处理当天原run申请，unknown仅原TF查询，授权更换不重置已消费额度。见[实现与验证](../docs/verification/settlement-auto-73-2026-10-24.md)；不能将此工作源码描述为本地83部署已具备。

后台成功通常是 `code=0`，兼容支付 V1/V2 的成功码与签名另见 [兼容设计](../docs/design/2026-10-23-compatibility.md)、[实际约定](../docs/design/api-conventions.md) 和 [两插件方法映射](../docs/design/two-plugin-methods.md)。不能用后台通用响应替换旧商户支付协议。

通用退款、转账等请求 `amount` 使用人民币元字符串；`/channel-sharing` 专用接口的所有金额（含回退 `amount`、规则 `fixed_amount`/`minimum_amount`）均为人民币分的整数字符串，例如 0.10 元传 `"10"`，比例 `rate_bps` 为万分比。历史领域响应存在分单位和大写 Go 字段，必须按具体接口解析，不能仅凭字段名推断单位。幂等号重复只允许原身份、原订单、原金额；资金请求 `202` 或渠道分账结果未知 `409` 代表待查证。客户端不能自行生成新的业务号以逃避未知状态。

四个原渠道分账菜单使用 `/channel-sharing/channels`、`receivers`、`rules/{channel_id}`、`payment/{trade_no}`、`orders` 和 `returns` 专用接口。管理员必须明确商户，GET 用 query、POST 用 JSON；商户身份只能来自当前会话，禁止提供 `merchant_id`（包括空值或自己的编号）。列表使用 `cursor`（默认0）和 `limit`（1..100），返回 `items`、字符串 `next_cursor`，0表示没有后页。

接收方先保存加密草稿，再单独绑定；列表仅返回掩码。规则发布使用 `expected` 原规则ID作CAS，首次为0，发布后只影响未来支付的原始分账声明。提交PS只接受原平台订单及业务请求号，不接受客户端自行指定分账金额；新旧规则不能套用历史未声明订单。分账逐接收方结果、执行结束和剩余资金释放分别显示，不能把 `completed=true` 当作全部成功。

回退通过 `POST /channel-sharing/returns` 传原分账业务号、原成功 `receiver_id`、独立业务请求号与正整数分 `amount`，只回到原商户，不能当作买家退款；累计额度由服务端按原成功出账与已有回退核对。结果未知保留原PS/PR/PF号，先本地GET恢复记录，再用对应 `/{external_no}/query` 查询原上游请求；查询不消费新资金授权、不重发资金。模拟通道明确 `available=false`，不能因为创建资料成功而宣称远端分账成功。

独立完结先用 `GET /channel-sharing/finish-preview/{trade_no}` 核对原单，再 `POST /channel-sharing/finishes` 提交原订单及独立业务号。当前只开放可验证的直连微信V3；支付宝无可信终态证明的独立完结保持关闭。`source=sharing` 返回原PS已可信释放的结果，无须另建PF；新PF受理回执只记pending，不能因为提交响应 `FINISHED` 就释放。后续查询必须核对原交易、原商户及原gross金额，可信终态后才释放原merchant_net。`finishes` 列表仅使用limit（无cursor），返回最新items；按原业务号可查旧记录。

PF已可信失败后，原号重放仍读取原失败历史；只有重新预览返回 `eligible` 且 `previous_failed_id>0` 才能显式创建新业务号并重新授权。新意图保留原失败关联；unknown/pending不能换号或借用失败恢复入口。该恢复动作不会自动发生。

管理员独立资金密码及逐动作授权见 [资金授权设计](../docs/design/admin-funds-authorization.md)。开启政策后，首次金融意图要求 `X-Payment-Authorization`，服务端把授权消费、资金预留及审计同事务提交；错误资金密码返回403，缺授权返回428。查询、回调及原有商户签名接口继续按原身份工作。令牌和密码不得放进URL、浏览器持久存储或日志。

资金challenge只绑定核对的原请求和当前快照，不保证操作最终符合业务资格。实际资金事务会在消费授权前重新验证原订单、原结果证明、额度与通道账户；例如原PS仍处理中时，即使已经签发PF challenge，也会拒绝执行，保持0外呼、0授权消费。

结算收款资料通过专用AEAD接口保存不可变版本，申请时固定原收款快照和费用。钱包和明确银行卡产品在 `/settlement-payouts/{id}` 操作，发送原净额并使用既有专属预留；审批通过不等于出款成功，历史缺快照不能自动接管。银行卡能力另声明 `bank_product`，微信BANK_FAIL与支付宝REFUND使用各自证据；成功后费用留原申请账户，`funding.state=paid_fee_held`，不计平台收入。可信退票一次退回本金和保留费用，`state=reversed`、`funding.state=returned`；缺证据保留 `reversal_pending`，不得重发。

44新增付款账户绑定：微信固定AppID和MchID，支付宝固定当前直接应用身份；密钥轮换不改变公开账户。原普通付款、批次和结算不能因为同AppID换MchID而继承新账户查询/发送。历史真实转账无可信账户摘要时失败关闭，列表及已证明未发送取消仍可用，不把当前配置补成历史。

生成与离线结构检查：

```powershell
node docs/tools/build-openapi.mjs
```

生成脚本来自已实施路由，检查本地引用、必填路径参数及 operationId 唯一性；真实权限/资金契约由 `tests/contract` 和 PostgreSQL 测试验证。修改接口后同时更新脚本、前端调用和契约测试。资源的具体字段由同身份 `GET /console/v1/{role}/resources/{resource}/schema` 提供，禁止向客户端返回已有密码或完整 API 密钥。

商户消息提醒：本人会话GET/PUT `/console/v1/merchant/account/notice`；授权管理员GET/PUT `/console/v1/admin/merchants/{merchant_id}/notice`。使用原msgconfig九场景、元金额字符串、voice_order、显式targets/clear_fields及expected_profile_id比较版本。未提交的目标保留；显式清除单列；保存不发送消息。禁止代登录与query商户覆盖，写入要求CSRF。目标和凭据只返回掩码。

设置路径追加 `/events` 可GET只读通知记录，limit默认20/最大100，before_id为规范非负ID，按ID倒序返回items及next_cursor。未知、重复或格式错误分页参数返回400。记录不含消息正文或接收秘密；accepted仅表示发送服务受理，unknown包含永久claim已提交但缺少结果。没有重发或清除接口；读取不改变投递状态。此80增量尚未进入运行93。

2026-10-25 全后台UX修复中的新增契约（工作源码，尚未切换固定98部署）：`GET /console/v1/{role}/membership-sales` 对管理员返回销售配置及修订，对商户返回其可见目录。本人super通过 `POST /console/v1/admin/membership-sales` 原子保存开关、上下架、价格、月/永久期限、排序、可见组与简介。会员购买通过明确的 `/workflows/memberships` 接受可选整数quantity=1..300，总价、期限及原奖励收入在同一事务绑定；旧按天配置及旧证明格式继续兼容。迁移84为新购买保存不可变销售条款，1..83迁移内容不改。

投诉统计 `GET /reports/complaints?view=overview` 返回本地已登记投诉的今日/昨日概要及按商户排行；不返回支付净额、手续费，缺金额为null，近7日无支付分母时比率为null。该视图不能同时使用from/to；CSV与列表口径相同，导出继续要求审计。完整OpenAPI现有181路径/213操作，旧接口保留。

商户API信息的签名方式使用GET/PATCH `/console/v1/merchant/account/signature-policy`。keytype=0为MD5+RSA兼容、1为仅RSA；null保留旧signature_algorithm严格配置。V2请求在所有模式下仍须RSA，RSA时间窗不变。只有本人原merchant持久会话可修改，PATCH精确提交keytype与expected_fingerprint，明确确认、不自动重试；无有效RSA公钥不能改为仅RSA。密钥生成与策略设置分开，均不暗改另一项。GET只读完整字符串商户ID、当前模式、原算法、RSA可用性及HMAC指纹；不返回MD5秘密或私钥。

管理员密钥页采用同一实际keytype0/1方式，通过本人super会话 GET/PATCH `/console/v1/admin/merchants/{merchant_id}/signature-policy` 和同事实CAS设置。普通商户DTO仅增加只读keytype。显式模式下旧algorithm字段只代表历史配置，通用资料保存不能改变它；RSA-only时公钥有效性由真实商户事务锁/CAS内核验，不因旧algorithm=MD5或调用方漏传Keytype绕过。前端明确提交模式时清除旧算法草稿，取消不清；处理期间与资料保存和密钥生成互斥。

商户GET `/console/v1/merchant/workflows/internal-transfers` 使用安全方向DTO读取本人转出/转入，可筛选direction=in/out与status=pending/approved/rejected，count/分页同本人谓词；不因此获得审批/修改权。id/peer_merchant_id为完整字符串，amount是正元字符串，completed_at仅真实匹配终态事件，没有记录则null；remark当前无原持久来源而固定null，不泄露actor/私密幂等号。管理员和其它workflow范围不变。
付款列表及CSV新增原加密意图remark，保留原合法tab但拒NUL/CR/LF，私密cipher/Title/当前profile不作为展示备注。cost_amount仍整数分总花费、pay_date为原真实回执时间，缺失显示未记录。
授权域名支持唯一左侧通配符`*.example.com`，仅匹配非空子域；根域和evil-example.com不匹配。plain域保留原root/suffix行为，大小写/单尾点统一规范，所有权/approved/enabled与域名修改后重审在真实支付前检查消费；不自动开启enforce_domains，不扩大黑名单匹配。

商户订单GET返回merchant_actions={query,notify,refund,print}四个布尔能力，一次本人批量元数据/原用途验证，不在GET调用通道。菜单只显示真实条件允许动作，点击后再读取原详情并复核当前状态；写入权限仍由原服务端操作执行。query补单不是mark-paid，通知/退款/打印继续使用原持久流程。
账务/退款GET及CSV接受精确trade_no，不忽略参数；重复、空、过长、NUL/CR/LF或冲突q/search_field拒400。物理账户scope与历史余额window保持，关联只取真实FK或原writer精确canonical key；分账收款/推广商户只看本人分录，不显示他人订单内容；无可见分录与不存在同为空。

已接入支付通道费率使用本人super GET `/console/v1/admin/channel-policies`、GET/PATCH `/console/v1/admin/channel-policies/{channel_id}`。这里channel_id是真实注册支付通道，不能与通道资料ID/名称混用。PATCH精确{rate,mode:0,enabled,expected_fingerprint}，费率为商户保留百分比：99.50对应手续费0.50%；父0.00是0%保留，组0/空仍表示继承所选父率。未配置DTO为configured=false/rate=null/revision="0"，保持旧费用语义；不捏造100%。
选路和私有费用事实同事务，出生费用/成本/组/支付产品账户身份由原事实验证消费，写入与实际支付均不能在事实丢失后降为legacy。旧false出生标记/原HMAC保持，不为历史重新报价或补造证明；原已有attempt继续原账户恢复。历史只有assignment无order/quote、或旧bornorder无attempt且缺原费用证明，在通道配置新政策后首次支付明确拒绝，需要新订单号。收款mode1直清尚未实现完整账务，不得默认为代收执行；受控import和policy均拒显式1，和account_mode不同。

邀请当前政策：`GET /console/v1/merchant/referral/current-policy` 只读解释当前有效组、预算和条件；100元示例是所选基数比例计算，实际订单收入未知时不估算。不包含他人组资料、token或成本证明。

本人联系QQ和个人单笔金额：`GET/PATCH /console/v1/merchant/account/personal-settings` 只修改`qq/pay_minmoney/pay_maxmoney`；金额为元字符串，空或0不额外限制。原session、CSRF、CAS、字段名审计同事务，最后写后按数据库时钟核验；无关profile和密钥不覆盖。现有新订单/报价金额引擎实际执行，不能放宽其它限制或改历史金额。个人API开关、密钥登录及买家加费另阶段，未通过这些字段假开放。

Console登录与session的`user.id`统一为完整字符串，防止大商户编号在浏览器比较本人归属前丢精度。仅变更console传输DTO；内部身份、Cookie/CSRF、持久session及历史证明不变。

商户资料 GET `/console/v1/merchant/account/profile` 新增原字段 `uid`（完整字符串）、`addtime`（UTC+8展示时间）及 `registration_time_known`。只读字段不接受PATCH；原name/email/pay保留。未提供原注册时间的历史导入账号，addtime为空并明确显示未提供；管理员商户列表也不显示导入时钟为注册时间，注册日期筛选只匹配已知原时间。原有商户出生时间保护和金融证明不变。

脱敏迁移schema1的Merchant可选 `created_at`，例如 `"created_at":"2018-07-08T09:10:11+08:00"`。需确认旧系统时区后提供明确RFC3339时间，不接受零时间、未知偏移-00:00或晚于exported_at的日期。旧快照不增加该字段仍可导入，原字节摘要和资金导入规则保持；缺少原时间时仅标记未知，不捏造或改写历史。


核心风控实际接口与25个全局/2个组继承字段见 OpenAPI CoreRisk* 与 docs/core-payment-risk-compatibility.md；98子通道与171同TxCheck整合见 docs/core-payment-risk-integration.md。
