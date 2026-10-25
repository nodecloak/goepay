# 结算计划、批次导入、打印与模板的本地证据和后续实现边界

核对日期：2026-10-23。工作目录 `E:\GOEPAY`。本次仅读取本机明文源码、静态安装表结构、静态前端和现有 Go/Vue；未连接生产、读取真实配置或密钥、调用打印服务、发起资金请求。本文件是下一阶段的研究输入，不是业务实现或全量验收报告。分账 schema55 候选源码保持冻结。

后续独立增量：CSV/XLSX五列导入预览已按本文证据实施，见 `docs/verification/transfer-batch-import-2026-10-23.md`；下文“当前缺口”表保留研究时点事实，其文件导入一项不代表该增量之后仍然缺失。旧XLS仍不支持。

## 证据解释

- **Pro 明文前端**证明菜单动作、发送字段、展示字段与选项，不能证明加密服务器的鉴权、事务、幂等、计算顺序或错误 JSON。
- **Release 明文 PHP**可证明该版本实际执行路径；两版本同名功能不能据此宣称服务器行为完全一致。
- **安装 SQL**只证明数据模型及出厂默认值；没有读取运行数据库、原用户数据或生产凭据。
- `docs/baseline/routes.csv`、`menus.csv` 是入口索引。`docs/verification/backoffice-action-coverage.md` 的结算、分账等段落包含早期状态，不能直接作为当前缺口清单。以下已与现有源码重新比对。
- 本文中的新 API、字段和限额均标为**建议**，不是原 Pro 已确认的 JSON 契约。所有源路径相对仓库根；压缩 JS 基本为第 1 行，用文件名及锚点定位。

## 当前可直接确认的缺口

| 功能 | 现有实现事实 | 仍需实施的行为 |
| --- | --- | --- |
| 自动生成结算申请 | `settlement_terms.go` 已有最低/最高、费率、费上下限、固定留存、日次数快照；`funds_availability.go` 已执行全局 D+0/D+1；`ledger_requests.go` 创建时预留，审批不等于出款 | 真实运行计划、商户结算开关、组/商户覆盖、百分比留存、可配置日切、计划运行明细及去重 |
| 结算批次 | `web/src/workflows.ts:7..8` 的 `/settle/list`、`/settle/batch` 都映射 `settlements`；已有单申请钱包/银行出款及退票闭环 | 独立批次 ID、不可变成员、汇总、按批筛选/导出、批次下逐项准备与执行；批选审批不能替代 |
| 批量代付文件导入 | 已有持久 `transfer-batches`，1–100 项、整批预留、逐项 claim、原号查询、未发送取消、资金授权；`BatchRecipientEditor.vue` 逐行录入，无文件输入/文件解析 | 原五列 XLSX/XLS/CSV 的下载模板、解析、预览、逐行错误与显式创建；已有资金核心可以复用 |
| 打印 | `internal`/`web/src` 未发现 `print_order`、`orderprint`、`print_devid` 设置消费者或打印任务；worker 无打印 kind | 平台/商户专用设置、订单小票内容、成功事件自动任务、手动重印、任务状态；第三方设备 connector 需要独立适配 |
| 模板配置 | `internal/backoffice/extended.go:21` 明确 `template-configs` 为展示资料，字段仅 name/title/accent_color/enabled | 三类模板目录、当前选中项、manifest 动态字段、图片上传、预览、保存后实际渲染生效 |

不能把已落地的单结算出款误写为缺失：`internal/server/settlement_payout_console.go` 已有 `prepare/execute/query/cancel-unsent/capabilities`；`settlement_payout.go` 绑定原申请/专属 reserve/原受款快照/原通道身份；`settlement_bank_payout.go` 对接原银行生命周期。后续批次必须调用这些用例。

## 一、结算规则与运行计划

### Pro 可读事实

`epay_pro/public/static/assets/index-C6la0DtP.js:1` 的 `__name:"settle-settings"` 定义、标签和保存整体对象明确：

| 原字段 | 明文前端含义与选项 | 当前 Go 对照 |
| --- | --- | --- |
| `settle_open` | 0关闭、1每日自动、2手动申请、3自动+手动；说明“自动结算指自动生成结算列表” | 无对应自动生成/手动总开关执行逻辑 |
| `settle_type` | 0 D+0、1 D+1 | 已有全局0/1执行，不应重新实现为另一套余额 |
| `settle_set_hour`, `settle_hour` | 是否启用组日切；D1日切0–23，空默认0；开启后用户组日切设置才有效 | `funds_availability.go:38` 固定 `Asia/Shanghai` 零点，未见小时/组覆盖 |
| `settle_transfer`, `settle_transfermax` | 手动结算自动转账；超额不自动转账，空不限制 | 有管理员显式单申请出款，无申请即自动付款 |
| `settle_auto_transfer`, `settle_auto_transfer_hour`, `settle_auto_transfermax` | 结算列表自动转账、0–23时间点（空不自动）、超额阈值；说明与自动生成结算列表共用计划任务 | 无运行消费者 |
| `settle_maxlimit` | 每日手动申请次数，空不限制 | 已有 `settlement_daily_limit`，当前按所有结算请求计数，不能声称仅手动次数语义一致 |
| `settle_money`, `settle_rate`, `settle_fee_min`, `settle_fee_max` | 最低金额、结算手续费、费上下限 | 已有整数分/bps政策与新申请固定快照，原字段名不是当前 API |
| `settle_alipay/wxpay/qqpay/bank` | 各受款方式开关 | 现有通道能力/商户 transfer 开关不能自动等价为这些结算开关；QQ不因展示标签而支持 |
| `auto_settle_money` | 商户满金额自动转账；空关闭；前端说明仅支付宝、需先启用转账、计划任务会增加URL | 无对应自动阈值付款消费者 |

`index-CCjDRx0B.js:1` 的用户组设置有 `settle_open/type/hour/transfer/money/rate` 与方式开关等继承字段；`index-fMgyEUZ4.js:1` 账户设置包含 `remain_money`。这些只证明前端字段；下面的 Pro 明文 cron 进一步证明部分实际规则。不能将“控制器加密”推广成所有 cron 业务文件都加密。

`index-CQrUK7ri.js:1` 的计划任务 UI 明确：

- `GET ./cron/info` 返回 `mode/cronkey/tasks`；模式 `url` 或 `swoole`。
- `POST ./cron/saveConfig` 发送配置；`saveTaskParams` 发送 `{id,params}`；`setTaskStatus` 发送 `{id,enabled}`；`runTaskNow` 发送 `{id}`，成功文案“将在1分钟后执行”，返回 `next_run_time`，不是同步完成证明。
- task 展示 `frequency`（second/minute/hour/day/once）、`interval`、`execute_at`、`parameter_definitions`、`params`、enabled、运行时间/结果。URL 模式组装 `cron?do=<task>&key=<cronkey>` 加参数。
- 路由原文 `epay_pro/route/admin.php:75..80`；`admin/Cron.php`、`controller/Cron.php` 在 `docs/baseline/encrypted-files.csv` 标为 Swoole 加密。

当前 `internal/backoffice/extended.go:17` 的 cron-tasks 只是 name/kind/interval_seconds/enabled 资料；`cmd/worker/main.go` 只消费通知和明确支付、退款、转账、银行、授权释放、分账/回退/完结核对队列，没有扫描这些资料生成结算计划。因此已有队列、可手动重试任务并不等于原任务计划实现。

### Pro 明文 cron 的实际执行事实

后续检查确认 `epay_pro/app/cron/Settle.php`、`Transfer.php` **是明文**；`app/service/SettleQueryService.php`、`app/logic/TransferLogic.php` 仍为加密，未解密或执行。下列规则已经有直接 Pro 证据，不再仅靠 Release 推测：

- `Settle.php:28..35` 先生成结算列表，再尝试列表自动转账；identifier是settle。
- `:40..47`：启用日切时settle_time与当前小时起点比较，否则与当天零点比较。`:76..85` 选择用户组非empty的settle_hour否则全局hour，只在当前小时相等时处理；若该商户当天已有**任何**结算记录就跳过。因此group hour为字符串0时 `!empty` 会回退全局；不应把这种边界当作合理新规范。
- `:49..74`：先按全局最低额筛启用、有结算权限、有账号/姓名商户，强制实名未满足则跳过；组settle_open>0时2禁自动，否则全局须1/3。`:86..118` 组settle_rate存在即float覆盖；余额留存支持百分比与固定额，百分比有效区间0<rate<100；应用全局最低额、全局费min/max再得净额。
- `:120..136`：插结算后changeUserMoney扣款，循环末写settle_time。报告count在规则跳过前即递增，不能作为实际创建数。该可读函数没有引用settle_type/D1可结算额度，不能因UI有D1就推断自动生成也遵守；Go必须统一以可出账额度为准。
- `:141..169`：列表自动付款开启且hour合法，到该小时及之后才执行；当天settle_auto_transfer_time已有完成标记则跳过。只扫描**当天**status0结算，按id升序。
- `:177..213,279..288`：最大自动额比较的是毛额money；方式1/2/3/4映射支付宝/微信/QQ/银行；组transfer_app只要不是null/空串/`_`即使用（包括0），否则全局通道。受款账号/姓名和净额取原结算行，当前用户channelinfo参与通道加载。提交号由当前时间+随机数构造。
- `:214..265`：异常和非code0结果直接写失败；code0直接标结算完成并写上游信息，可能附wxpackage，再发通知。此行为不证明未知失败可安全重出或微信已实际到账。`:271..275` 循环后写当天标记并报告成功/失败/跳过。
- `Transfer.php:26..68` 的阈值自动付款：全局auto_settle_money、必须全局支付宝通道；只取余额严格大于阈值的最多5个启用支付宝受款商户，按uid倒序；组禁自动/费率覆盖，费用仅上限；随机原单号。`:70..106` 付款后才插结算与扣款，某些错误会关通道。其策略与Settle生成路径不同。

这些日期标记、查询后写入、浮点运算、随机付款号和异常即失败存在明确安全改造需求；它们是原始事实，不作为新实现的并发/资金保障。时区仍取PHP运行环境date默认值，本地文件没有证明生产时区；Go继续显式Asia/Shanghai。

### Release 可读执行事实及不能照搬的部分

`epay_release/cron.php:19` 的 `do=settle`：

1. 使用配置 `settle_time` 与当天零点比较，已运行则退出；扫描启用商户、结算开关、收款账号/姓名、余额最低条件，另检查强制实名。
2. 读全局手续费和用户组；组 `settle_open=2` 跳过，未指定时全局须为1或3；组 fee 覆盖全局。
3. `remain_money` 可为百分比（0<百分比<100时按余额比例留存）或固定额，之后再次校验最低结算额；费率计算后应用 min/max，再得到净额。
4. 写 `pre_settle` 的受款方式/账号/姓名/毛额/净额/时间/status0，再扣商户余额；循环末更新 `settle_time`。

`cron.php:350..400` 的 `do=transfer` 是另一条路径：`auto_settle_money` 阈值、只取最多5个支付宝方式商户，读取组费率，以当前时间随机单号向通道付款，成功后才插结算记录和扣余额；特定错误外会关通道。该路径没有证明与上面的留存、最低费用、日次数逻辑一致。

不应照搬日期配置 check-then-write、付款后才记账、每次新随机单号、任意失败自动关通道。Go 必须先持久化/预留，未知保留原单核对。Release 的缺陷不是兼容要求。

### 建议实施模型和 API

先实现**生成申请计划与运行记录**，复用现有申请事务；计划出款是独立授权能力，不能暗中借用管理员交互会话或绕过资金密码。

- 专用 `settlement_plans`：id、name、enabled、version、timezone（首版固定 Asia/Shanghai）、generation_hour、商户/组范围、规则版本；受款方式白名单、是否仅生成待审核申请。不要存任意 URL、shell、SQL 或 Go 函数名。
- `settlement_plan_runs`：plan_id、policy_version、business_date、scheduled_at、started_at/finished_at、状态、scan/create/skip/error 数；唯一 `(plan_id, business_date)` 加明确版本/补跑语义。
- `settlement_plan_run_items`：run_id、merchant_id、结果 reason code、request_id、政策/受款配置版本、毛/费/净快照；同一 run/merchant 唯一。原申请幂等号由计划身份稳定生成，不能由页刷新生成新号。
- 建议 `C/admin/settlement-plans` GET/POST；`/{id}` GET、带 expected_version 的 PATCH；`/{id}/preview` POST（只读演算）；`/{id}/runs` GET；`/{id}/runs` POST（显式创建一次运行意图）；`settlement-plan-runs/{id}` GET。`C=/console/v1`；这些尚未实施。
- worker 只 claim 已知 kind；生成任务与每商户结果同事务或可恢复分段提交；锁与幂等跨多worker成立。补跑需指定业务日、有界范围，不自动补齐所有漏日。
- 全局/组/商户的规则优先级必须明确写入新版本政策；Pro明文cron已证实的覆盖按上节记录，未读到的手动申请/控制器路径继续保留未证实标记。百分比留存用整数比例，日切变更必须统一影响 quote/申请/所有出账检查，不能只调整 UI。
- 自动出款另需持久、可撤销、有限额/时段/通道/受款范围/有效期的授权政策和审计；本阶段可保持默认关闭并通过本地假通道验收。单纯保存 enable=true 不应宣称无人值守付款已完成。

## 二、原结算批次与批量代付导入

### 原结算批次并非普通代付批次

Pro `settle-C3-SgkfR.js:1` 明确 `batchList/create_batch/deleteBatch/complete_batch/batchTransferList/transferChannels/transfer/export`；参数分别有 `{batch}`、`{batch,type}`、`{id,type,channel}`。`index-5nUERs6a.js:1` 展示 batch/allmoney/count/time，支持结算列表按 batch 跳转、按方式转账、四种CSV导出；删除确认明确“仅删除批次记录，不会删除结算记录”。

Release `admin/ajax_settle.php:52..65` 将所有 status0结算记录写 batch、status2，累计 **realmoney净额**，另插 `pre_batch`。`67..70` 的 complete_batch直接批量改 status1。`admin/settle_batch.php` 和 Pro batch-transfer UI 均逐记录调用转账，区分 success/done/failed/loading，不是一次供应商多收款人 API。

安装表可验证相同领域形状：Pro `app/sql/install.sql:353..376` 的 pre_settle 含 batch、money/realmoney、受款快照、transfer_no/channel/status/result/date；`409..415` pre_batch 含 batch/allmoney/count/time/status。Release相应 `302..325`、`358..364`。

**建议**独立 `settlement_batches`、`settlement_batch_items`，成员绑定现有 request_id/terms/recipient/funding，不从可用余额再次预留，不接受文件里的自由受款账号覆盖原申请。创建时原子锁定有资格的未归批申请；每个申请最多一个活动批次。按商户和request顺序锁，跨商户批次的原资金隔离不可丢失。

建议 API：`C/admin/settlement-batches` GET/POST（POST `{external_no,request_ids,expected_members_digest}`）；`/{id}` GET；`/{id}/prepare` POST（按成员类型显式通道映射）；`/{id}/execute` POST（有界逐项，独立资金授权）；`/{id}/query` POST（只核对已claimed原单）；`/{id}/cancel-unsent` POST；`/{id}/export?format=summary|alipay|mybank|wxpay` GET。具体 DTO 待核心设计，不直接包装内部handler循环。

批次状态由成员原状态聚合，unknown不能改为完成；“归档批次”与“所有项可信付款成功”分开。原删除/complete按钮可以改为审计归档、计算汇总，但不删除财务证明或凭人工注释伪造付款成功。若真需人工外部付款对账，另建有外部凭证且不触发再次付款的对账流程。

### 五列文件导入格式已由两版共同证明

原文件：

- `epay_pro/public/static/files/transfer.xlsx`
- `epay_release/assets/files/transfer.xlsx`
- 两文件 SHA256 都为 `6628B8B4971815E73E85A1D213DFF36FFBF5C2A153E884CBE4DC8A51334B27C7`。
- 只读检查 ZIP 内 `xl/worksheets/sheet1.xml` 和 `xl/sharedStrings.xml`：第一张表 A1:E1 是 **序号、收款账号、收款人姓名、转账金额（元）、转账备注**；后10行只有序号1..10，账号/金额空白。

Pro 管理员 `index-CYTHVGS4.js:1`、商户 `index-Cn-r1JA8.js:1` 和 Release `admin/transfer_batch.php:49,91..105` 均：允许 `.xlsx,.xls,.csv`，只读第一张表，`sheet_to_json(...,{header:["index","account","name","money","remark"]})`，跳过第0行；缺序号/账号/金额的行被静默跳过。Pro 对账号、姓名、金额、备注 `String(...).trim()`；管理员提交 type/channel/paypwd/account/name/money/desc，商户不提交channel。通道和方式在页面选，不在五列文件里。

这证明的是**导入预览和逐行提交**；原前端没有证明服务端整批预留。当前 Go durable batch 比其更严格，文件导入应接现有批次而不退回直接循环付款。

`index-DwfHm1L9.js` 的另一个“支付宝批量付款单”是 alipayfund 扩展，支持账号类型/1000行，**不是上述通用五列格式**。用户已暂缓其它插件，不据此扩展通用导入范围。

### 导入建议与安全边界

第一阶段可只新增前端纯解析器与模板下载，预览后填入现有 BatchRecipientEditor，再用现有 `C/{admin|merchant}/transfer-batches` 创建。无需为了文件上传增加服务器存储；服务端仍逐字段验证原JSON。若需要集中预览API，建议 `POST .../transfer-batch-imports/preview` 只解析不预留、不创建出款，输出 source_row/错误代码/归一化金额及总数；不可把preview response当最终授权。

- 五列顺序兼容；提供同头模板。空白模板行跳过；不完整非空行必须报原文件行号，不能静默丢款项。超过100条应整份拒绝并说明当前限额，不能默默分批付款。
- 账号为文本，前导0/长ID不得经 JS Number 转换。Excel已写为数值且丢精度的长账号应拒绝并要求文本重录；不猜测恢复。金额用严格十进制字符串→整数分，禁止parseFloat总计、科学计数、负零、超两位小数、NaN/Infinity。
- XLSX有界解压、sheet/cell/文本总量限制；不执行公式/宏、外链，不根据单元格发URL请求。拒绝公式账号/金额与不支持的加密/损坏文件，明确CSV编码（UTF-8/BOM；如实现GB18030需显式选项与fixtures）。`.xls` 若未接解析器必须明确不支持，不能只靠accept扩展名宣称完成。
- 文件行序号只是显示/定位，不是幂等号。相同账号多行可为有意多笔，显示警告而非暗中合并；批次 external_no及有序规范化items决定幂等摘要，网络未知回查原号。
- 预览用 Vue 文本转义；账号/name不进URL、控制台、localStorage、analytics或错误原文。清空表单/退出时清除草稿。平台与商户身份由session/授权确定，文件不能指定他人owner或任意通道。
- 微信银行产品仍须显式银行代码；旧五列没有此字段，不能猜银行。可在预览中逐行补填；Alipay bank不应强加微信字段。

### 结算导出与五列导入不能混为一种格式

Pro batch页面列出 summary/alipay/mybank/wxpay四种导出。Release `admin/download.php:38..112` 可读出：

| 导出 | Release明文格式 |
| --- | --- |
| 汇总 | 序号、转账方式、收款账号、姓名、元净额、备注；UTF-8 BOM |
| 支付宝 | 标题行+序号/账号/姓名/元金额/备注；GBK |
| 网商银行 | 姓名、账号、开户行名、联行号、金额、附言；支付宝/银行方式；GBK |
| 微信 | 批次单号/名称/AppID/元总额/总笔数/备注元信息，再“转账明细”及商家明细号/OpenID/姓名/身份证/元金额/备注；UTF-8 BOM |

这些列不是新版本供应商当前可上传协议的外部验证；本研究没有调用供应商或联网查模板。可先做内部汇总和有版本标识的legacy格式，不能标成真实供应商已验收。导出真实受款资料需专门权限、范围审计、有界行数/字节，不能从masked账号生成无效付款文件，也不能因普通列表读权限就无条件解密；CSV按字段转义并防公式注入。

## 三、打印设置与订单小票

### 已确认字段与执行路径

Pro `index-C6la0DtP.js:1`：平台 `orderprint` 开关、`print_appid`、`print_appsecret`，文案对接启易付打印机，须商户同时填写序列号并启用。`index-fMgyEUZ4.js:1` 商户设置有：

- `print_order`：0关闭、1仅聚合收款码、2所有订单。
- `print_devid`、`print_count`（默认1）、`print_voice`；返回数据结构 `print_config:{devid,count,voice}`。
- `index-Dk-66xD5.js:1` 订单页仅在平台开关且merchant print_order>0时展示打印，动作按 trade_no，成功文案是“指令已发送到打印机”。
- 原路由 `epay_pro/route/user.php:160` 的 `order/printOrder`（USER-R100）和 `:223` 的 `account/editPrint`（USER-R143）；控制器加密，不能保证原API返回字段与Release完全一致。
- 安装字段 Pro `app/sql/install.sql:340..341`、Release `install/install.sql:292..293` 为 print_order与serialized print_config。

Pro `app/lib/Printer.php:20..33,46..70` 本身也是明文，直接证实相同小票内容、默认count1/voicefalse、固定网关和路径、biztype3以及appid/timestamp/biz/biztype/reqid串联HMAC；code0仅代表命令受理。服务器无响应/非JSON会抛异常。具体手动与自动调用入口仍需区别于加密控制器。

Release明文补充：

- `user/ajax2.php:384..390` 保存 print_order/devid/count/voice；`user/editinfo.php:229` 前端张数1..5。
- `includes/lib/MsgNotice.php:30..33` 订单成功事件自动打印条件：平台开启，merchant模式2，或模式1且tid==3；自动调用配置的count/voice。
- `user/ajax2.php:786..800` 手动重印按 trade_no+uid查自己订单，拒绝未支付/未开启，内容来自订单；调用 Printer 时未传count/voice，故该版本手动默认1张无语音。不能据此声称Pro手动行为相同。
- `includes/lib/Printer.php:19..32` 小票含codename、订单/支付金额、优惠0、支付类型、成功状态、单号、时间、备注；base64编码XML样内容，支持count和tts。固定网关 `https://saas.excelsecu.com`，路径 `/openapi/v1/iot/printer/print`，biztype3；请求含appid/reqid/timestamp/biz及HMAC-SHA256签名（`:45..64`）。仅返回code0表示命令受理，没有纸张打印完成证明。

这里的金额用原订单 `money`；现Go已区分毛额/实付/费用，模板应显示明确口径，不擅自用手续费后净额替换支付金额。支付状态必须来自可信事件，不允许打印配置或商户传“成功”。

### 建议 API 与实现界限

- `C/admin/printing/settings` GET/PUT：enabled、provider固定白名单、credential_reference、配置版本、凭据是否存在；secret只写不回显，AEAD/外部secret文件可选。不能导入原生产appid/secret来本地测试。
- `C/merchant/account/printing` GET/PUT：mode0/1/2、device_id（普通读取脱敏）、copies1..5、voice bool、expected_version。merchant只能操作自己，设备绑定/解绑记录审计，限制任意设备批量试打。
- `C/merchant/orders/{trade}/receipt` GET：拥有者订单的安全小票预览；`.../print-jobs` POST `{external_no}`：显式手动重印意图；`C/merchant/print-jobs` GET及`/{id}` GET可查询。admin代操作需相应订单/设备权限。
- `print_jobs` 保存merchant/order/source_event/config_version/device_ref/content_digest/外部reqid及状态；自动任务唯一 `(source_event_id,kind)`，回调重放不多打；手动重印每个明确external_no一项。打印失败不得回滚已确认付款或阻塞支付异步通知。
- 状态用queued/sending/accepted/unknown/rejected等事实，不用“已打印”冒充物理完成。网络超时可能已受理，connector无查询/幂等证明时不得自动无限重发；显式重印应提示可能重复，限流并审计。
- 所有订单文本必须按打印协议转义并有长度上限；禁止备注注入XML标签/控制指令。固定网关不开放任意URL，设置没有“测试生产打印”默认动作；本地httptest假打印网关验证签名/脱敏/超时即可。
- 用户“其它插件先不做”不应被解释为基础小票预览/设置全不存在；外部设备connector若暂缓应明确“尚未接入”，保存配置不伪报打印成功。

## 四、首页／支付页／聚合收款页模板

### Pro 与 Release 的确切差异

Pro安装默认 `app/sql/install.sql:12..14` 分别是 `template_home/pay/qrpay=default`。`index-CSBB45Vm.js:1` 页签是home/pay/qrpay；调用 `settings-mNzlkkmh.js:1`：

- GET `./template/templateList?type=...`，消费 `list/current`，项包含name/title/has_config、预览信息。
- POST `./template/setTemplate` `{type,name}`。
- GET `./template/templateConfig` `{type,name}`，消费title及inputs；POST `./template/saveTemplateConfig` `{type,name,config}`。
- POST `./template/uploadTemplateImage` multipart字段 `file`，消费 `url`。
- inputs字段至少key/name/type/required/value/note/show及选项；UI明确支持text/input、textarea、select、switch、checkbox、radio、image。checkbox是数组，其它取value；UI有条件show和说明HTML。不要把原HTML说明直接作为可信Go运行内容。

本机 `epay_pro/public/template/{home,pay,qrpay}/default/info.json` 只有name/title/author/link/version/desc，home inputs为空对象；没有非空动态配置manifest样本。因此“动态表单类型可由UI确认”与“某具体模板含哪些业务字段”是两件事，后者仍无材料。

Pro `pay/default/cashier.html:24..90` 直接引用 trade_no、row.name/addtime/money、支付方式列表、payAmount/feeText，`:146` 转 `/payment/submit?typeid=...`。`qrpay/default/index.html:28..35` 有dopay表单和uid/token/type/direct/payer等变量。`home/default/index.html` 使用config_get、template_path与登录态。这些含ThinkPHP表达式，不能原样当Go模板执行。

Release `includes/lib/Template.php:6..38` 从配置template选择PHP目录、缺页回退default并include；`install/install.sql:19` 默认index1，`admin/set.php:286..307` 是目录预览切换。它没有证明Pro三类模板的通用配置契约。Go不应为复刻该加载器而执行上传PHP或允许任意路径。

### 当前 Go 真实运行情况

- `internal/backoffice/extended.go:21` 的template-configs无type、选中项、inputs或图片资源，不被收银台消费。
- `internal/server/provider_http.go:403..` 直接写固定收银台HTML，已有expiry/HMAC token、支付原身份检查、CSP nonce和对不同上游material的限制。模板切换不得削弱这些边界。
- `internal/server/collection.go:27..58` 确实读取collection-code title和collection-style的合法颜色，`:187..` 在固定HTML渲染；这部分颜色是真生效，但不等于全模板系统。不能误写“所有样式都是假资料”。
- 未发现home/pay/qrpay模板注册表、选中配置读取或模板图片上传消费者。

### 建议实施字段、API、安全与验收

- 构建时/部署时审核的模板registry：`type,name,title,author,version,description,preview_asset,config_schema,capabilities`；首版各至少两个本地模板用于证明选中生效。模板逻辑为受审Go/Vue资源，上传仅图片，禁止上传可执行模板或在线插件安装。
- 选择记录按type唯一，含name/version/config_version；配置字段从manifest白名单验证，未知字段拒绝、checkbox/开关类型明确、选择项有限；secret不作为普通模板字段。空manifest也应能选择并真实渲染，不假造原默认模板含不存在的配置。
- 建议 `C/admin/templates?type=` GET，`/{type}/selection` GET/PUT `{name,expected_version}`，`/{type}/{name}/config` GET/PUT `{expected_version,config}`，`/images` POST，`/{type}/{name}/preview` GET（只读受控预览）。原前端类型/字段可映射，新API不宣称兼容加密PHP后台RPC。
- 图片校验大小、真实MIME/magic、像素/解码资源、重编码；首版PNG/JPEG/WebP，不接SVG/HTML；随机或hash文件名，独立静态目录，无目录穿越/可执行扩展/覆盖源码，引用返回受控asset_id。不得服务端抓取任意图片URL。
- 服务端typed view model提供真实订单/金额/支付方式/材料与受控URL；模板只改变外观，不能选择新通道、更改original receiving account、执行代付、生成可信paid状态或泄漏签名密钥。公共checkout仍先验证token与原意图，preview使用假数据或单独授权，不能偷用他人trade_no。
- CSP、URL scheme、HTML转义和上游可信材料处理继续由核心renderer控制；配置不进入template.HTML或动态脚本，禁止eval/任意函数调用。失败回退本地default并记录可诊断事件，不回退到任意文件。
- home应有明确public handler；pay覆盖QR/跳转/受控HTML/JSAPI/mini/付款码状态/return状态；qrpay覆盖带token的金额输入与原幂等意图，不能仅改管理页截图即算完成。

## 推荐拆分顺序及下一步测试要求

1. **五列文件导入**：边界清楚，可独立复用现有资金批次；解析成功只填草稿。单测XLSX/XLS/CSV原模板、前导0/长ID、公式、金额/限额/坏文件、行号；浏览器选择文件→预览→资金授权→原批次创建、未知时回查不换号，测试全部本地假凭证。
2. **真实模板registry与renderer**：独立于资金迁移；三类选择、配置、图片上传、刷新后生效，公共token/转义/CSP/原通道不回退，截图及HTTP contract固定证据。
3. **结算专用批次**：复用已完成单申请出款，在PG验证跨商户成员唯一、并发归批、原reserve不重复、execute/cancel竞争、未知原号只查、银行退票聚合、不可信完成/注释不能改paid。UI有单独批次列表/成员/状态而非通用审批替身。
4. **生成申请计划**：持久run/item与多worker去重，先只有生成+人工审核；时间边界、组覆盖、百分比留存、停用/并发变更、日次数口径需要明确。自动付款另行有界授权与本地故障注入，不借cron CRUD声称落地。
5. **小票与可控打印任务**：先完整设置/预览/本地假设备命令闭环；事件重放、owner、格式注入、超时unknown/限流、凭据脱敏与支付确认不受打印失败影响。

本次没有运行新业务测试，也没有修改业务源文件。上述“当前实现”来自源码核对；schema55完整候选的全库测试由父任务独立进行，不以本研究替代其结果。
