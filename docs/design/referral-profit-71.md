# 配置估计成本与利润基数奖励（71实施计划）

2026-10-24。用户要求继续参考原Pro完成资金规则；根任务授权此增量。本文在产品代码前写明实施合同与实际证据边界。冻结83、历史迁移1..70、原v1/v2证明函数不变；不连接生产/真实厂商、不读取真实配置/密钥。实施采用真实RED→GREEN、独占本地测试数据库、纯本地TLS及离线数学互操作。

## 证据与明确语义

Pro `app/lib/Channel.php:14,42`带入通道costrate，后台 `public/static/assets/index-DNfqr2Hp.js:1`明确“通道成本/%、仅用于统计利润、可留空默认为0”；`index-C6la0DtP.js:1`提供手续费利润基数。Pro OrderProcessService/PaymentService/PaymentLogic编码，因此不声称已恢复其隐藏计算与权限。

可读Release `includes/functions.php:604–614,707–734`明确：手续费F=max(round(realmoney-getmoney,2),0)，配置成本C=realmoney*costrate/100，P=round(F-C,2)，type2奖励按P乘invite_rate、最终正数才发。成本是配置估计，非厂商实际账单；本增量命名“配置估计利润基数”。原Pro/Release定时任务按昨天SUM(realmoney)及当前组费率，并不是利润逐订单快照；Go保留现有更明确的原订单条款immediate/daily合同。

根任务确认采用Release旁证的**差额最终half-up**。不得先round成本再减手续费。负利润在奖励计算时归0，但必须有真实合法成本证明；缺失/未知成本不能变成0成本。

## 最小闭环与实现顺序

1. **严格、独立的可信成本配置。** 使用既有认证加密导入的 `Channel.Config["costrate"]` 精确十进制字符串，避免添加商户可写字段；新成本版本指纹绑定规范率、provider/AppID与强收款账户。缺key与显式"0"不同，显式空值非法；只由服务器批准配置加载，不能从商户HTTP/param/profile读取。百分比0..100、最多6位小数，拒绝符号/指数/空白/过精度/溢出；这是新实现的明确验证范围，原JS没有证明相同精度上限。缺失不自动沿用Pro默认0，历史不补0。运行时cost信息不含凭证；JSON安全DTO不暴露私有HMAC、配置秘密或账号主体。
2. **在出生时固定最终奖励。** 实际server/provider_http.go先选route再CreateOrder；collection也先route后CreateCollectionOrder，scan/preauth/mini已有可信channel参数。新增typed服务端成本context，将selected channel/provider/AppID/强physical-account/method、配置revision/fingerprint与精确rate传到创建事务；同TX生成原订单费用条款、最终利润R与新的cost证明。不会在paid时回填成本或重算旧v1条款。不可用成本在外呼前拒绝，前置路由预检不能把不可配置通道假定免费。recharge等原excluded目的继续不奖励。
3. **最终attempt必须同源。** beginProviderAttempt原order锁与first claim事务内，核对原成本fact和最终channel/provider/AppID/强physical-account/实际支付method。禁止仅provider/AppID的弱scope回退、通道换账号、mini/OAuth延迟完成后换成本、失败或unknown后换通道/重签新成本。原绑定存在时重放只能读取原已封成本；成本配置当前变化不修改旧订单。新外部claim前成本证明损坏则0外呼。
4. **仅新增迁移71与成本proof家族。** 扩展政策basis允许profit，增加不可变原订单cost snapshot/source audit/结构约束与缺父缺子检查。原policy proof、v1terms proof、v1reward/clawback proof、v2daily proof函数体和域保持原字节；新cost HMAC绑定order/policy refs/原termsHash、M/F/精确率/成本商余数/P/R/route/physicalscope/revision/fingerprint/sourceversion。通过原不可变PolicyIDs解析basis，决定实际启用profit必须有匹配costfact；不得按marker是否存在回退fee/gross家族。负利润/合法0奖励也有证明，删除、复制、错source或伪0均失败。原条款HMAC已封的disabled/promoter_disabled及recharge/no_relation无需成本；关闭奖励不阻断无成本通道付款，后来开启不重新定价旧负条款。判断不能仅依赖Enabled或R，因为合法利润计算R0仍必须证明。
5. **出资不变、两mode与退款完整接通。** R仍只来自该原付款已验证的原手续费F，不从clearing或其它订单/会员预算补贴。配置成本是计算事实，不伪造实际expense journal。immediate复用原reward；daily复用原F→scopedpending→award及K/E退款状态机，所有环节读取同一个出生已封R。退款unknown留原预占，失败恢复同来源，任何可信成功按现有合同撤整笔原奖励；paid/退款重放不能二次发或二次补资金。
6. **checker完整、历史保持。** 原fee/gross历史没有cost也必须继续验证；新profit缺cost/缺原policy/audit/attempt/receipt/funding或存在孤儿cost都可检测。新旧家族不能混用，HMAC有损不静默忽略。保持分页有界与canonical账户锁，审计、资金grant或SQL失败整体rollback。不得修改旧迁移来容纳新字段，71新增约束必须显式防SQL NULL三值绕过。
7. **HTTP/前端收口后才开放。** 政策API新增profit枚举与明确能力；保留super原会话、CAS、原资金授权frozen body和锁后重检。只显示配置估计成本/估计利润，不宣称真实账单或全部Pro隐藏逻辑。商户无成本写字段；V1/V2付款不能注入成本，即使传相同名字也不读取。精确capability/DTO交根分配前端，后端/HTTP与负面门禁未完整绿前不半开放UI。

## 精确整数算法

金额M/F/P/R均整数分。成本百分比精度scale=1,000,000，U是百分比乘scale，D=100*scale=100,000,000；0<=U<=D。保留U/D这一精确比例，不用float。

```text
q = (M / D)*U + ((M % D)*U)/D
r = ((M % D)*U)%D
C = q + r/D                       // 精确估计成本（分），不是先round的分数
P = max(F - q - (r > D/2 ? 1 : 0), 0)
R = roundHalfUp(P*rewardRateBPS/10000)
```

这里严格使用`r>D/2`，成本恰半分时，正差额half-up会保留上一分，不能写>=。合法范围下q<=M，中间余数乘积<D²=1e16，配合原金额上限可在int64内精确计算；每个边界仍需显式验证。R的quotient/remainder算法复用已有受限rate<=10000的整数舍入，保证0<=R<=P<=F。不因展示已舍入成本而再次计算P。缺成本与P0是不同状态。

例F1分、M100分、costrate0.5%：精确C0.5分，P1分；如果先把C舍入到1分会错算P0。成本超F则P0/R0，有costproof的合法负利润不等于unknown。

## 预期改动范围（具体新增文件名以TDD收口为准）

- 新 `db/migrations/000071_referral_profit_costs.up.sql`；原1..70 SHA不动。
- `internal/channelconfig`从既有encrypted Config map读取明确costrate，新增精确配置构造/secret-safe测试，不新增商户可写成本字段，不读任意文件。
- `internal/adapters/payment`新增成本来源typed interface；只声明服务器配置，不新增厂商请求或把估计成本塞进支付线协议。
- `internal/payment`新增profit_cost/profit_terms/profit_check测试与sourcefact；窄接fees birth、referral typedloader、provider attempt、paid/refund/daily/checker。原v1/v2 proof函数先保存精确baseline再逐字后验。
- `internal/server`普通付款、collection、scan、preauth、mini/OAuth原创建线程与资金政策摘要/能力；`cmd/api`配置连接若必需仅传受控成本来源。
- `api/console.openapi.json`等文档及前端由根协调；前端预期仅referral政策basis/能力/确认文案，避免争用App.vue。新增成本字段禁止merchant任意写入。

## 实际RED/GREEN矩阵与门禁

| 族 | 必须实际覆盖 |
|---|---|
| 数学/解析 | 半分及其两侧、cost0/显式0/缺失、负利润/零fee/零rate、金额和率最大值、中间溢出、过精度/指数/负数/空白；与离线合成PHP round数学向量对照，不执行真实SDK |
| config/身份 | constructor0IO、配置未加载不假0；channel/AppID/强physicalaccount/method/revision/fingerprint绑定；密钥轮换和物理账户改变区分；秘密不进JSON/日志 |
| birth/attempt | 两成本通道路由同businesskey/并发sticky；原条款同TX最终R；OAuth/mini延迟原身份；错成本/缺cost/更换runtime在firstclaim前拒绝0外呼；重放无当前配置回填 |
| 原证明兼容 | 历史v1fee、gross69、daily70金钱与HMAC函数字节相同；新profit从原PolicyIDs派生；缺marker不降级；新fact删除/复制/错subject/缺audit/SQLNULL均失败 |
| 支付 | 本地TLS实际请求/签名/receipt身份，unknown/超时/late success/重复通知一次奖励；未付/授权冻结/未确认材料不奖励；recharge、会员购买不串普通profit |
| immediate+daily | immediate正确R来源；daily出生→可信paid激活→UTC+8due→award；费用/账户/receipt真实来源，次日和lockwait时钟；撤权/审计rollback；blocked不误报0 |
| 退款 | partial/full，K0/KR、pending/awarded、unknown永久→failed恢复/可信success撤R，fresh last-failed CAS，重试/并发/不足/晚到，源预算和原F不被重复支出；费用资金/hold/sharing交叉基线 |
| checker | 缺父/缺子/末页孤儿/复制HMAC/错误原账户、原receipt、原costversion；20counter既有字段保持，纯损坏证据能增加相应invalid，不增加无意义新0字段 |
| HTTP/UI | super/merchant/operator/delegated/CSRF、严格JSON/金额string/未知字段/成本注入拒绝或不读取、原funds target/CAS/summary、context epoch/oncebody、375px/mock；无真实请求 |
| 最终 | 独立GUID数据库/随机schema，实际使用新专属PG50 profit GUID库与统一最多2个DDL slot wrapper，p1；不碰PG46最终回归或部署PG48。相关全回归、独审，再固定新候选并完整Windows/Linux/CLI/浏览器验收；失败原日志保留 |

实施证据路径 `.tools/referral-profit71-*`。根已授权连续实施，但有无法同时满足出生时序、真实资金来源或原证明兼容的阻塞时，先报告具体失败，不用宽松fallback换绿。

## 独立复核缺父修复

独立审查指出新R0订单terms+cost+event全丢会伪装旧历史。真实RED日志referral-profit71-entire-source-loss-red.log确认checker零、claim owner=true及重放成功；root在尚未发布71增加orders.referral_source_version/hash，所有当前Go出生订单在原事务写独立HMAC父锚（绑定原order身份、旧termsHash及可选costProof），旧v1/v2 proof13函数逐字未变。SQL仅原created事务可0→3且之后不可改；profit必须父3，已有订单不回填。checker从父orders与terms并集分页，缺所有子表仍可见。实际同一负例GREEN5.332秒拒绝claim/replay并变红。历史兼容用明确原pre57 raw订单测试，不能从当前引擎创建后删新source冒充历史；新增实际70→71无回填/晚加锚拒绝测试。完整关联回归在执行，旧失败证据保留。
