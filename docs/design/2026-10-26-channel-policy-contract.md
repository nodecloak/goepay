# 168 核心通道 / 子通道契约差异与实施切分（只读）

日期：2026-10-26。审查者：audit_remaining_merchant_ux。仅写本报告；未改源码、未启动服务、未联网、未读真实凭据、未调用支付。

## 结论与范围

必须把通道费率、运行资格、选路结果与新订单快照一起落到实际交易链，不能只补 CRUD。当前最关键的可核实差异是：组 `rate=0/空` 已被解析为继承，但费用引擎没有已选通道参数，实际上回退到 `groups.rate_bps`，没有继承本次选中的原通道 `rate`。当前 `subchannels` 只保存资料，未进入交易路由；组 `channel=-2` 仍被严格拒绝。

对照基线：`E:/GOEPAY/.tools/work164-e7129a13-c052-4b8a-8f37-87d5253dbc95`（累计 164 候选，来源冻结 163）。下文 Go/Vue 路径均相对此目录。原材料根 `E:/GOEPAY/epay_pro` 与 `E:/GOEPAY/epay_release`。JS offset 是 UTF-8 文件按 JavaScript 字符串读取后的 **0 起始字符偏移**，非字节偏移；PHP/Go 为 1 起始行号。

原 Pro `app/lib/Channel.php` 可读，已经直接用于确认选路/继承/限额；不能把所有 Pro 后端都说成加密。以下 Pro 控制器/流程是 Swoole Loader 密文：`app/controller/admin/PayApi.php`、`admin/UserSubChannel.php`、`user/Applyments.php`、`app/logic/PaymentLogic.php`、`app/service/OrderProcessService.php`。这些文件仅检查 loader 头与长度，未解密、未执行。Pro 写接口最终校验、模式 1 的现代账务/退款/并发预留细节不能从编译 UI 或 Release 推定。

## 原通道基础字段与单位

证据：Pro `public/static/assets/index-DNfqr2Hp.js` offset 3790 起 ChannelEditDialog、6593 附近 rate/costrate/mode；API 封装 `channel-DoFrrF6Z.js`。Release `admin/pay_channel.php:55-111`、`admin/ajax_pay.php:194-299`；Pro `app/lib/Channel.php:10-73,153-320`。

| 字段 | 可证明的原契约 | 当前 164 差异 / 实施约束 |
|---|---|---|
| id / name | 显示名称，仅展示，不与其他通道重名；add/edit/copy 分支。Release 服务端检查名称重复 | `backoffice/schema.go:78` 是独立资料记录 ID；必须明确与加载运行通道 ID 的绑定，不能用同名当绑定 |
| rate | **给商户的分成比例 %**，不是扣费率。99.50 表示保留 99.50%、手续费 0.50%。UI 空初值；Release add 空/0 用 100，edit/copy 不同分支；Pro 写入默认未知 | 现 channels 无 rate，组 rate_bps 是扣费 bps；不得同名误转。推荐保留原展示字符串、显式转换 `feeBPS=10000-retainedBPS`，确定精度后校验 |
| costrate | **成本百分比**，UI 空默认 0，仅用于统计利润；不是再从商户扣一笔。Release 允许负数字符格式，但 `functions.php:611` 仅正值扣利润 | `channelconfig/factory.go:223` 已读取受控 import config.costrate；`adapters/payment/payment_cost.go:58-103` 0..100、6 位小数、百万分之一百分点，是配置估算，不是账单实际成本。不能重复实现或把成本算成商户费率 |
| mode | 0 平台代收（默认）：支付扣手续费后入商户余额；1 商户直清：不给商户加本金、需从商户余额扣手续费，余额不足不可支付 | UI tooltip 是直接 Pro 证据；Release `api/Pay.php:220,485` 和 `functions.php:644-650,681-687` 有该行为。当前 imported Channel 无 mode，paid 入账仍 credit MerchantAmount；**仅加 mode 字段会错误入账** |
| type | 支付方式 ID，插件列表随 type 联动；与支付场景 method 不同 | 当前 channels.provider 是 alipay/wxpay，method 是 page/jsapi 等，不能将原 type 映成 method；只允许已适配产品，不能因原菜单多类型自动宣称支持 |
| plugin | 按 type 列出插件，必须选择；列表可看插件详情；配置 inputs 由插件声明 | 当前 import 仅 wxpay/alipay 受控适配器；原 plugin 名及原多产品账户语义要做显式映射，不能任意加载执行旧插件 |
| status | 支付开关，与 daystatus 自动停用是不同状态；原查询常要求 status=1/daystatus=0 | 当前 channels.enabled 明确是“启用资料”。通用支付 routeCandidate/providerRoute 并不读取该资料开关；真实通道暂停用 channel_stop 的独立不可变事件。不要把资料开启误认为开通资金权限 |

**mode 边界**：Release 的 merchant/user.mode=1 是“订单加费”报价模式，见 `api/Pay.php:172-189`，与 **channel.mode=1 直清** 是不同维度。不得用同一个字段/枚举替代，也不得把 provider 的 direct/service-provider 账户类型当作 channel 的平台代收/商户直清。Pro 现代清结算加密，Release 仅能证明旧业务意图；168 若要启用直清，必须独立定义成功/失败/未知、预留手续费、退款释放与分录契约并本地验证，不能靠 UI 文案实现。

## 原通道高级限制与继承

证据：Pro admin 模块 offset 27458 state、28600 保存、30600-34300 表单；`app/lib/Channel.php:171-238,311-320,401-465`；Release `pay_channel.php:90-111`、`functions.php:736-752`。

| 原字段 | 单位 / 默认 / 作用 | 已知差异或未知 |
|---|---|---|
| daytop | 单日金额（元），0/空不限，达到后暂停通道；Release 标注修改次日生效，但 ajax edit 有解除 daystatus 的条件 | Release 成功处理累加 **realmoney**，达到 `>=` 才设 daystatus=1；不是选择前预扣限额。Pro 现代成功处理加密，精确口径需确认，不能假定与现 pay-roll daily_amount 一致 |
| daymaxorder | 单日成功支付笔数，0/空不限 | Release 成功路径 count status>0/date=CURDATE 后停用。当前 routeStatsFor 统计 committed route assignments（含未成功），不是同一业务口径 |
| paymin / paymax | 单笔元，空/0不限；等于边界允许 | Pro 候选过滤支付金额 money；不能用 fee/net 作比较；父子交集另见下节 |
| timestart / timestop | 0..23 小时，UI 允许空；跨午夜分支，边界小时包含 | Pro Channel 按 PHP `date('H')`，材料未固定配置时区；当前 pay-roll 明示 UTC+8。0/0 被可读候选逻辑当不限制；空/一端未填的写入校验未知，建议成对校验并明确 UI 时区 |
| rate_limit_seconds / count | 最近 N 秒**发起订单**数；任一<=0不限制 | Pro `Channel.php:457-465` count order.addtime，按 parent channel；子通道自动选择先过滤、返回0避免重复检查；指定子通道返回父限制给后续流程，不能漏掉指定路径 |
| payfee_lessthan / payfee_mincost | 元：手续费低于前者则扣后者；UI 说明空继承系统最低手续费 | 可读 Channel 返回这两个值。Release 使用全局配置且原 net/clamp 与订单加费不同；Pro 最终应用在加密 PaymentLogic 内，无法证明通道字段的空/0分别代表继承/关闭，必须先定确切 DTO |
| ordername / ordername_rules | 通道商品名优先系统/商户；变量 `[name]/[order]/[outorder]/[time]/[qq]/[phone]`，`|`随机；按金额规则数组 name,min,max，保存校验非空/数值/min<=max | 原 UI 证明字段与说明，Pro 实际展开加密未知；不可覆盖商户原 name 或修改用于重放校验的原请求事实；快照 provider subject 与原name须分离 |

当前 `internal/backoffice/pay_rolls.go:15-40` 已有组 limits 和每通道 rules：min_amount/max_amount/daily_amount（元字符串）、daily_count、rate_limit_seconds/count、start/end_hour。`server/provider_policy_routing.go:161-183,448-495` 确实落路由，且并发 advisory locks + quota/assignment 一起提交。它是现有可复用基础，**不是原全局 channel 限制已经完成**：规则只在选中该 roll 时读，独立/其它 group 路由不消费这些 rule；同一通道分散在不同 roll 不能产生互相矛盾的“全局”限制；已用 channel.stop 的资金保护必须保留。

## 商户子通道：字段、资格、父子关系与实际选路

直接证据：Pro `app/lib/Channel.php:37-73,75-99,240-291,498-518`；merchant `index-WhNAEuqO.js` offset 1800-2800、3720、4110、5300-7600、9510、10483、12311；admin `index-C5U5W_96.js` offset 3000-6800、7800-11000、10900-12900。

1. 子通道是 `id/uid/channel/name/info/status/weight/paymin/paymax/usetime`；type/plugin/mode/rate/costrate/应用类型/父限额源自父通道，**没有证据证明一个单独的子通道 rate 写入字段**。列表显示费率 `(100-Number(row.rate)).toFixed(2)%`，这是继承/有效保留比例转换后的手续费率。不得把列表显示值反写成 parent.rate。
2. 用户组 info 中该支付方式 `channel=-2` 表示用户自定义子通道；`getGroup` 只从 status=1 的支付类型生成 subchannel_type 名字与 ID。Admin 新增先读取 merchant.group_subchannel_type，筛选支付方式，保存前再取 merchant 检查；这是 UI 可见资格，**Pro 控制器是否事务重核被加密，无法证明**。Go 必须在服务端锁定有效组+商户+父通道重核，不能依赖下拉框。
3. Pro 实际选择查询同时约束 `B.uid=self, A.type=type, A.status=1, B.status=1, A.daystatus=0`。可额外限 apply_id（进件关联，暂缓不妨碍核心手工子通道）和明确 sub_channel_id；指定别人的子通道不会命中。UI 父 channel_status=0 时显示不可点“已关闭”；子 enabled 不能绕过父 enabled。
4. 先剔除 parent paymin/max、child paymin/max、时段、频率不满足的候选；为空直接失败，不能回退到超限候选。有效 min=`max(parent>0,child>0)`，有效 max=`min(parent>0,child>0)`，空项忽略；父5~100/子1~200仍5~100，父5~100/子10~80为10~80；交集为空必须拒绝支付。
5. `roll_type=0` 随机可用（默认）；1 按 usetime ASC / id ASC 顺序，选后更新 usetime 毫秒；2 权重随机，每个<=0历史权重按1；3 首个启用且满足限制（按ID）。UI 仅 mode2 显示权重列，可写1..99整数。普通 pay-roll weight1..1000和 strategy priority **不是** merchant 子通道 roll_type 的替代契约。
6. info 参数：Pro `Channel::getSub` 与 Release `ajax_user.php:712-754` 证明只有父 config 值为 `[key]` 的占位字段被子 info[key] 替换，其他父参数保留；apptype/appwxmp/appwxa也有显式覆盖规则。Admin 参数界面按声明的 inputs key 构造 payload。当前 `provider_merchant_id` 一个字符串不能替代多参数 schema。实现须服务器声明允许项、加密保存秘密、读 DTO 掩码/不回传原凭据、账户身份指纹绑定；不得让商户通过 info 覆盖 URL、证书路径、provider或parentid等未声明字段。
7. Admin编辑原 uid 禁止改，type/channel 依 merchant eligibility 有条件开放；Release编辑只更新channel/name，非 uid。Go不能允许 owner转移造成历史订单换商户。使用后parent/plugin/account身份改变须新版本，而不是将旧 attempt/receipt 重解释到新账户。

当前 `internal/backoffice/extended.go:12` 只有 tenant 资料定义（merchant_id/name/channel_id/provider_merchant_id/approved/enabled）。全 `internal` 生产 Go 搜索 `subchannel` 只命中该定义；`internal/grouppolicy/info.go:198-239` 不含 -2 支持，测试也明确拒 -2。因而当前不能宣称 enabled/approved 让子通道交易、group=-2已实现、原轮询/费率/测试已完成。现有 approved是本地资料审核，不是 Pro 下游授权；须保持边界。

## 费用、通道路由与订单绑定的先后

### 原已证明顺序

Release `includes/lib/api/Pay.php:140-156` 先获取/插入订单；首次 `Channel::submit` 选通道并取得 effective rate/mode/limits（164起）；随后计算 realmoney/getmoney（172-189）、检查限制与直清手续费余额（201-221）、在 `channel=0` 条件下写 `type/channel/subchannel/realmoney/getmoney`（232），最后 Plugin::loadForSubmit（254）。旧订单复用原 channel/subchannel/realmoney/getmoney（192-197），不是每次根据现配置重新报价。成功分录见 Release `includes/functions.php:601-616,644-650,681-687`；成本只扣 profitmoney。Release 旧代码并发/退款做法不是新Go实现应照抄的安全保证。

Pro 可读 Channel::getSubmitInfo 证明选中的 parent/subID 与 rate/mode/limits 同次返回。Pro PaymentLogic/OrderProcessService加密，不能证明现代订单写入/预留的精确先后。另有一个必须保留的事实差异：Pro `Channel::info:113` 判断 rate!=空字符串，字符串"0"可能覆盖；`getSubmitInfo:190/288/313` 用 empty(rate)把"0"当继承。应依据正常 submit 的已证明契约，不伪称所有辅助接口绝对一致。

### 当前 164 顺序及断点

1. `server/provider_http.go:55` 完成 providerRoutedRequest：验证注册 provider/allowlist/products，选路、group/roll政策、quota和不可变 `provider_route_assignments` 独立提交（`provider_policy_routing.go:198-595`）。按 merchant+out_trade_no 锁；重放不再次分配，已有 attempt/OAuth/mini上下文优先；未知结果不切换。这些应保留。
2. `provider_http.go:68` 取得受信 `PaymentCostQuote`；`:73` 另事务 CreateOrder。`payment/fees.go:192-216` **再次**读取有效组、金额限制与 feePolicy；`:230` 保存 fee_policy/rate/group/net/platformfee。该事务没有选中 channel/subchannel/mode/rate revision 作为费率来源；两个事务间组/通道配置变更需定义一致快照，不能读“最新”费率拼接旧 route。
3. `payment/fees.go:29-94` 开关未启用直接无费；启用则先 groups.rate_bps，再用 info中明确正保留率覆盖。`ParseGroupRetainedRates` 对0/空不产生覆盖，因此回退 group.rate_bps，**不是 parent.rate**。例如100元、group.rate=0、group.rate_bps=200、selectedchannel.rate=99.50，当前feePolicy结果2元，原正常submit应0.50元；这是代码推导示例，非本报告运行的PG断言。
4. `internal/payment/payment_cost.go:7-29` quote字段私有，绑定channel/method/estimate；其用途是 referral/profit-cost 快照（`referral_profit.go:29-68`），不是商户feePolicy来源。成本已做部分运行绑定，不能重复计费。
5. `provider_http.go:84-90` 此后才 Submit/预授权，`payment/provider_attempt.go:56-122` 锁order并绑定唯一channel/method/account/sharing/identity；`payment/store.go:331-340` verifiedpaid后从clearing贷记商户net与平台fee。没有 mode1 直清分支；不能以provider直连接口类型冒充资金直清。
6. 订单本身 orderColumns 没有 subchannel；reports.go:116以 provider_attempt.channel_id 分组，尚无子通道维度。历史无route/attempt的订单要显示未记录，不能根据当前配置倒填route/费率/成本/子通道。

最小安全技术要求：引入受信的“新订单路由/报价事实”作为持久记录（含 full parent/subID、owner、effectivegroup与expiry、policy revisions、retained/fee、mode、product/account identity、必要成本和limit口径）；让 order birth 使用同一已验证事实或在同一数据库事务里完成选择+quote+order。不能接受 merchant JSON 自报这些值，不能把私有 PaymentCostQuote 随便改成公共JSON；不得为历史订单补造事实。保留原请求hash/重复外部单号冲突/未知不重试不换通道/已有receipt绑定等现有约束。

## 原界面操作契约与当前实际缺口

| 页面 | 原实际动作与条件 | 当前与验收要求 |
|---|---|---|
| Admin通道列表 | kw ID/名称、plugin、type、dstatus全部(-1)/开1/关0；size20；可选成本/日限/笔限/时段列；单行开关 | 当前通用资料筛选/开关不足以等价。列需区分资料、可执行、暂停原因与权限；筛选/count/export一致且fullID不转Number |
| Admin通道批量 | 选择后open/close/delete；delete确认；wxpay选中子集才能批绑微信公众号/小程序，可解绑选择0 | 必须服务端逐项验权限/类型，明确全部成功或逐项结果；不能部分变更后UI假称全成功；不得重解释旧订单账户 |
| Admin通道动作 | 配置密钥动态inputs/证书上传；编辑；删除确认；测试输入名称+元金额默认1；dropdown订单 `/trade/order?channel=id`、付款`/transfer/list?channel=id`、copy、stat、advanced | `index-DNfqr2Hp.js:40358,45500`。测试是会建订单/可能发起真实支付的动作，不是纯连通性ping；本次只能合成adapter。copy原Release含config/apptype绑定，不能把凭据显示给无权角色或默认为开启；使用后禁止硬删损坏历史 |
| Admin通道统计 | channelPayStat 返回today/yesterday+时间曲线；支付金额realmoney、退款额、手续费利润、总单/成功单/成功率 | offset23500-27000。当前channels report仅attemptchannel维度并非原弹窗完整统计；成本未知显示未知，不捏造利润；成功/退款/未知口径和时区需固定 |
| Admin子通道 | kw ID/备注、uid、parent channel、paytype、dstatus，size20；商户/父通道跳转；编辑/删除确认/订单subchannel筛选/测试；默认隐藏参数和费率列 | offset7800-12900。不存在该页“批量/复制”证据，不能为求齐全声称原有。只实现证据支持动作；商户不能借URL看他人子账户/秘密 |
| Merchant子通道 | kw ID/名称、paytype，size10；type/name/代收标记/有效费率/今昨¥/今日成功率；父关闭则禁开关；轮询设置0..3；仅2时权重1..99；订单链接/测试/收款链接/限额设置 | offset3720/4110/5300/9510/10483/12311。收款链接从本人onecode.code_url加 `type=typename&channel_id=childID`，这个channel_id是**子ID**，不能直接当server现有parent channel_id。所有入口需统一受信解析；不要生成看似可用但被当parent或越权的链接 |

本页只发现 merchant 保存 paymin/paymax（元，空/0不限），未见商户编辑父rate/plugin/mode/secret的动作；通道轮询不是进件授权。实际进件插件暂缓不应成为延后核心子通道的理由，也不能假装进件成功自动获得能力。

## 建议最小实施顺序与切分

1. **168A 受信运行政策与费用闭环（P1）**：先做合成PG/HTTP RED：group0/空继承两个不同通道rate、明确group率覆盖、不开费的旧语义、100/0与小额舍入；注册通道与资料ID明确绑定，parent.enabled/stop/allowlist/type校验；route assignment与新订单fee同一事实，修改group/费率后旧order/attempt/replay不变。mode先只允许已实现的平台代收，历史未配置保持旧行为；直清不能默默按代收执行。交付必须以实际订单字段+成功分录证明，不能仅schema。
2. **168B 核心子通道执行闭环（P1）**：typed model、owner不可转移、允许参数schema及版本化账户身份、group=-2能力、父子enabled/limits交集、0..3选择+weight1..99、显式childID与parentID分离。用A/B/C合成租户+fullint64：owner、foreign、disabledparent、disabledchild、wrongtype、group失效/过期、无eligible候选、伪参数注入；并发顺序/weighted/unknown重放；每笔order/attempt/receipt绑定parent+sub且退款/查询只能原账户。未适配账户参数必须返回不可用，不能用任意parent发款。
3. **168C 全局通道限制与quota口径（P1/P2）**：把parent全局min/max/time/rate限制用于所有入口/路由，不只某个roll；明确成功日额/笔额 vs发起频率和已存在安全quota的并存优先级。RED涵盖边界、跨午夜、0/空、多个roll共享parent、并发最后一笔、失败/未知是否占额度、显式child与自动child一致；不能把成功日限悄悄替换成发起日限。
4. **168D 管理/商户UX和统计（P2，但与对应能力同批验收）**：原字段/筛选/状态原因/确认取消/focus/权限；批量部分结果、复制不泄密、合成测试支付、关联订单&付款筛选实际落SQL且count分页一致；桌面/375无页面溢出、fullID/金额精确。子通道fee/统计来自保存事实，不从现配置反推历史。API list DTO排除参数秘密、上传路径、身份session与管理员信息。
5. **单独的直清模式账务补丁（P1，须完整契约后实施）**：Release已证明意图但Go现无对应模型。先写余额预留/未知/确认失败释放/成功仅扣手续费/退款/重复回调/对账/settlement与reward/profit交互的事务和分录测试，确认Pro加密未知项的产品决策，再开放mode1。不能只开放下拉框，也不要让mode字段改变旧交易分录。

每个切分都应从冻结基线逐文件oldSHA隔离，实际PG+HTTP负例优先，浏览器 mock只能证明UI。Go相关现有route/group/fees/referral/payment/refund/sharing/checkout/OAuth/mini/scan/preauth回归应按改动影响选取；本报告未执行这些测试、未宣称实现或通过。当前所有发现是静态本地代码/编译JS契约审阅，Pro加密后端未知项已单列，未访问上游。

## 新证据对 151–161 路由结论的校正

这里依据**本机 Pro 可读实现**，不再使用“Pro 路由后端加密”这个过宽边界。未修改任何旧冻结目录。对当前 164 同样适用：

| 原选择 | Pro 可读证据 | 161/164 实现判定 |
|---|---|---|
| 0 关闭 | `Channel.php:167-169` 立即 false，不取默认通道 | `grouppolicy/info.go:213-215` Disabled，routing:334 拒绝；语义一致。费率0继承与通道0关闭仍属不同域 |
| -1 随机可用 | `:170-191` type/status/daystatus候选，先筛金额、最近发起频率、时段，**无剩余即失败**，再array_rand；非Release旧版“过滤空又回退原候选” | 当前automaticGroup以ID升序组织获准产品候选，crypto/rand等权选择并持久化；无候选失败且不外呼，符合选取形态。原全局limits尚未接入是168差异，不应重复说随机策略没实现 |
| -3 随机可用轮询组 | `:293-301` 取**所有同type/status1组**，无LIMIT1；先array_rand选一个组。`:304-309` 才解析被选组，失败false；`:401-454` 才在此组内筛通道status/daystatus/金额/频率/时段并应用kind。**不会先剔除当前无可用通道的组再抽，也没有组失败后再抽** | `server/provider_group_random_roll.go:17-65` 全局同type启用组，先取结构合法候选再crypto/rand；routing:392、448-495之后才限额/产品/账户/停止检查，失败不二抽。该核心时序已正确，不需因新证据改runtime。结构合法性、1000上限、global与merchant范围是Go明确安全约束，Pro该查询无merchant范围字段可据以相同推定 |
| -4 顺序可用 | `:193-215` 按**全局parent channel.usetime ASC，id ASC**，筛可用后取首，选后更新该parent.usetime毫秒。不是组自己的index | 当前 routing:339-350、502-503、568-570 用 `group_route_counters(group_id)` 持久组游标。符合Release旧版group.index，但与当前Pro在候选变化、不同组共用通道时可出现不同选择，属已证明runtime parity差异（P2），不是全等 |
| -5 首个可用 | `:217-238` type/status/daystatus、金额/频率/时段过滤，ID升序首项 | 当前automaticGroup精确ID排序后first，核心语义一致；全局原limits同168A/C补齐，不将单纯列表首项代替有效候选首项 |
| 显式 channel / roll | `:304-314` roll先筛成员，固定channel至少查存在/status；父通道信息返回给后续校验。roll kind0轮询index、1权重随机、2首项、3最高权重并随机打破同权，`:435-454` | Go payRollIndex four strategies 与此形态对应，现有受信allowlist/停止/产品与不可变绑定比原可见单处检查更完整，应保留。不能为“复原”移除这些保护 |

**-4 可重现的区别（静态推导，后续实际PG RED建议）**：通道1/2/3初始可用，先选1、再2，当前组cursor=2；随后关闭1，剩[2,3]，Go `2%2=0` 再选2，Pro取usetime最早未用的3。另两用户组共享1/2：Pro更新的是全局parent.usetime，第二组也受第一组选取影响；当前按groupID游标各自从1开始。最小修正必须先决定是否采用Pro全局usetime合同，做并发锁/精确排序/失败回滚/重放不推进测试，不能直接删旧counter或重写既有assignment。它不影响 -3 原已正确的一抽时序。

应在**新的累计源码文档**纠正（旧冻结文件保留）：

- `docs/plans/2026-10-26-original-random-roll-groups.md:9`：Release LIMIT1差异可保留，但“Pro服务端加密，无法证明随机分布”须改成已由本机 `app/lib/Channel.php:293-309` 证明多组随机及先抽组后筛成员；真正未知仍是加密写控制器/财务流程。
- `docs/plans/2026-10-26-original-group-route-semantics.md:3,7`：其Release证据是正确的，不能再将“与原组index对应”外推成当前Pro全等；注明Pro -4使用全局parent.usetime及上述runtime差异。
- `docs/plans/2026-10-26-original-group-routing.md:3`：152已禁止部署的历史结论继续保留，只在新勘误里补“-4共享组游标”是Release/现Go合同，Pro实际不是该实现。
- `docs/plans/2026-10-26-original-group-settlement-fees.md` 等所有“rate0继承”说明需链接本报告费用断点：parser已保持原意不等于实际feePolicy已读通道。不要改151–161当时验收收据或伪称过去做过通道rate PG测试。

## 168A 可立即实施的最小补丁建议（新单平台代收费率闭环）

这是建议设计，不是声称已有实现。以最新冻结累计 164 的逐文件 manifest 为父；本报告静态锚点取上述 work164，同名相关生产文件须在实施前按父SHA复核。

**明确第一步交付**：为已注册 parent channel 配置原 `rate`（商户保留百分比），新单选中该parent后，group该支付方式正rate优先，否则0/空继承这个parentrate，并在订单出生时固定 merchant_amount/platform_fee/fee_rate_bps及来源。只支持原平台代收；不在第一步开放mode1、更换凭据、子通道参数或历史重价。

**建议需要一份新迁移**，原因是当前 `orders` 只有最终fee值，provider_route_assignments只有路由rule快照，没有稳定的通道费率来源/版本与该order的关联；仅往可变backoffice JSON加rate再现场读取无法证明旧route用的是哪版。

最低数据事实可以这样切（命名由实现者决定）：

- 一张 typed parent fee policy（key为已注册runtimechannelID，不是任意资料记录），保存 retained_bps、revision、更新时间/受控审计，并禁止通过泛用record writer绕过。精确rate范围/精度应遵循已支持的组0..100%两位合同；新UI空默认100须由服务器同样确认，不能存NaN/科学记数/多余小数。未设置的新政策与旧资料行为要明确，不能悄悄让原现网费用从group.rate_bps变成零。
- 一张**不可变新单费用来源事实**（可与route assignment的新增私有quote字段组合，或用关联表；无需重复现有账务金额表），至少关联merchant+out_trade_no、parentID、method、route事实hash、effectivegroupID/政策来源和revision、retained/feeBPS；order出生时再绑定orderID/原fee四字段。若要同tx完成route+order，可直接做immutable order-route-terms表避免中间quote。HMAC/摘要和现有append-only审计方案沿用项目惯例，金额依然int64分，ID输出字符串。
- 迁移新增nullable关系/新表，不UPDATE历史orders或补造已支付route事实；旧fee快照照旧，只有确切新出生订单消费新事实。原assignment存在但order未创建的重试须定义：若它没有新费率事实，应受控失败/显式旧版本兼容，不能把它悄悄绑定“此刻最新rate”。新事实的出生与assignment/幂等键必须原子提交。

**代码落点**：

1. `internal/backoffice/schema.go:78` 的channels资料不能直接成为可执行权威；新增 typed policy service/专用GET+CAS PATCH，服务器从 `groupPaymentChannels()` 的受信runtimecatalog核验parentID/type/产品，不接受自报plugin或provider。通用保存channels只允许资料字段，原session/CSRF/权限/审计规则保留；是否仅super变更按已有资金政策安全合同定，商户无写权。
2. `internal/server/provider_policy_routing.go:330-595` 取得group与selectedparent时产生或核验费率事实；复用原routeLock和排序锁，不另抽一次路由。**优先将route quote与CreateOrder置同一transaction**；若现有调用架构不宜合并，就让assignment保存完整immutable quote并在birth消费该quote，禁止二tx第二次自行决定group/rate。到期组的锁等待后clock_timestamp检查也不能丢。
3. `internal/payment/fees.go:29,192-230` 将feePolicy的费率来源改为受信quote（显式正group保留率覆盖、0/空继承parent），继续用calculateFee现有“先四舍五入保留额，fee=gross-net”的精确分算法；默认旧入口如何处理是显式版本分支，不允许payload自行声明quote。保留payment_fee_enabled现有开关语义，禁止为了新rate无意给sandbox/历史加费。
4. `internal/payment/group_payment_rates.go` / `grouppolicy/info.go` 已正确把0/空当继承，不应把0改成100%手续费以绕过断点。`internal/payment/payment_cost.go` 是成本估算私有quote，不能替代本次fee来源，不得将cost再扣商户。
5. 全部new-order调用点核对：`server/provider_http.go:68-73`、`provider_scan.go`、`mini_launch.go`、browser-submit专用route路径、`collection.go:206-211`，以及recharge/会员/打印等各自purpose入口；哪些是已授权运行支付、哪些是离线sandbox要分明。仅修常见mapi而漏二维码/checkout/OAuth/mini/scan/重放，会产生同通道不同费率。`provider_attempt.go`和verifiedreceipt继续以原身份绑定；不以本次费用补丁放宽付款/退款/结算权限。

**必须先做真实本地合成PG/HTTP RED、再GREEN的最小收据**：

- 同商户两个runtimechannels，99.50/98.75保留率；group.rate分别缺失、空、0、0.00都由本次选中通道落fee；group99.00明确覆盖两个parent；groups.rate_bps故意设不同，证明没有误回退。
- 100元分别得到99.50/0.50及98.75/1.25；0.01/0.03等小额、100%保留、非法负数/溢出/精度；验证实际paid分录而非只断言DTO。
- 选路后阻塞并改group/parentrate或过期membership：新order只能使用同一已承诺事实或明确冲突，不能拼接；两个并发相同out_trade_no只有一份terms/order/attempt，changedamount/name/explicitchannel拒绝，不产生第二次模拟provider写。
- 创建后变rate/禁用parent/改组，再query/replay/refund/重复回调：原orderfee和account/route事实不变；未知结果不得换通道或重报价；旧历史无terms不补造，所有旧列/行对照不变。
- A授权parent、B未授权、C外商户，非法/未知/fullint64parentID、普通商户写政策、普通管理员越权/CSRF/CAS冲突；不暴露config/secret/account明文。桌面/375的原rate字段、取消、有效来源、保存后刷新与大ID最小交互。

第一步通过后才能把资料rate标成“参与新单计费”；-2子通道和mode1必须在各自闭环交付后再显示可执行。-4 Pro usetime差异可另切小补丁复原，不能混在rate核心中重写历史分配。

## 原材料证据指纹

以下为本次本机读取文件的 SHA-256；只记路径、长度与摘要，没有把专有源码复制进发布树。为复查定位而保存，不代表允许分发原文件。

| E:/GOEPAY/ 相对路径 | 字节数 | SHA-256 |
|---|---:|---|
| epay_pro/app/lib/Channel.php | 25794 | 65c6d57bcd4678fe805834e6927a34576be31f382a821d717743e06cf8803de8 |
| epay_pro/public/static/assets/index-DNfqr2Hp.js | 55064 | 08e17ab6832f15b2c952827f865ecc04a7e6cd505021c58913faef43906555c8 |
| epay_pro/public/static/assets/index-WhNAEuqO.js | 13889 | a6ce84e32c6730a55f6dff0e7adfd917252303599cbfad6063ffd68219ce1981 |
| epay_pro/public/static/assets/index-C5U5W_96.js | 15269 | dc2304e4d95f3c585c4fb2129e6e8b6c278f486b3bcd7bfb1b417f775e076b8a |
| epay_pro/public/static/assets/channel-DoFrrF6Z.js | 2488 | ed8de0f4c74ca705de205f965e6dcfe29c396b322c9f15eee0465b8b4763274e |
| epay_release/includes/lib/Channel.php | 18820 | ae0ce2a8a0d6958c838fa70c889a0eae55cf806d21608eeec9ccb194c3bc3635 |
| epay_release/includes/lib/api/Pay.php | 37382 | 083e4a941d734f85df3eb6d98b1aec873e8090f196af91c4c694a9c31c73a0f6 |
| epay_release/includes/functions.php | 58156 | 5c90a36916e984c7f558b76c7f470e550b58cbe073fa8bd8382e130006202865 |
