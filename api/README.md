# 接口文档

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
