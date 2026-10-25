# 34：支付通道策略路由

本增量将管理后台 `pay-rolls` 接入真实的通道选择，覆盖支付 API、聚合收款和商户余额充值。验证使用本地 PostgreSQL、临时密钥与 httptest 上游，不读取生产密钥，不代表已完成真实通道联调。

## 来源与实现范围

Pro `epay_pro/app/lib/Channel.php::getChannelFromRoll` 先按金额、时段和近期订单数过滤，再执行轮询、权重随机、顺序首选、最高优先级四种策略。Release `epay_release/includes/lib/Channel.php` 有前三种；最高权重策略与数量限流以较新的 Pro 为依据。

对应实现：

- `internal/backoffice/pay_rolls.go`：后台字段和严格配置解析。
- `internal/server/provider_policy_routing.go`：候选、本地前提、锁、配额及持久分配。
- `db/migrations/000034_provider_policy_routes.up.sql`：分配、游标与数据库约束。
- `providerCreate`、聚合收款与充值钩子：调用路由，随后才建立订单/提交付款。

这是受控 Go 规则，不执行 PHP 插件、脚本或请求中提供的任意路由规则。保存通道资料不会加载凭证或增加厂商产品权限。

## 规则作用域

新规则必须有明确的 `merchant_id`、`type`、策略和通道列表，只有 `enabled=true` 的规则参与新分配。

| 字段 | 语义 |
|---|---|
| `merchant_id` | 规范十进制字符串。具体商户 ID 大于零；`"0"` 是全局默认作用域，不是账本商户。 |
| `type` | 仅 `alipay` 或 `wxpay`。 |
| `enabled` | 布尔值，后台新建默认 false。 |
| `strategy` | `round_robin`、`weighted`、`first`、`priority`。 |
| `channel_ids` | 按顺序保存的 `通道ID:权重`，逗号分隔。 |
| `limits` | 组级限制的 JSON **文本字符串**，默认 `"{}"`。 |
| `rules` | 按通道 ID 映射限制对象的 JSON **文本字符串**，默认 `"{}"`。 |

同一商户/type 只能有一条启用规则；全局/type 也只能有一条。具体商户的启用规则优先于全局启用规则。具体规则存在但没有合格候选时拒绝，不退回全局或其它通道。

没有启用规则时，按当前商户许可的配置通道顺序选择首个合格候选，首次选择仍会持久绑定。**禁用轮询规则不等于禁用所有付款通道**：它停止该规则参与新分配，新订单可能使用全局规则或许可通道的默认选择。要撤销某通道的支付权限，应同时调整运行配置或商户白名单。

旧 `pay-rolls` 资料若缺少明确的商户/type，不参与执行；不会迁移为隐式全局规则。后台要求补齐新字段后才能保存为可执行配置。规则采用禁用保留记录，后台不提供删除入口。

## 四种策略与权重

所有策略都在过滤后的候选上执行，保留配置中的相对顺序。

| 策略 | 选择方式 |
|---|---|
| `round_robin` | 数据库游标对合格候选数量取模；首次分配成功后更新游标。不是进程内计数。 |
| `weighted` | 用整数累计权重和 `crypto/rand` 选择，不使用浮点比例。 |
| `first` | 选择配置顺序中的第一个合格通道。 |
| `priority` | 只在最高权重候选中随机选择；同一最高权重的候选等概率。不是逐个发送失败后再换下一家。 |

通道 ID 必须是大于零的规范整数，不能重复。最多 100 个候选，整个列表不超过 1000 字节；单个权重为 0–1000000 的整数。省略 `:权重` 时按原 Pro 解码语义取 0：轮询/顺序策略可以使用，权重随机策略要求总权重大于零；过滤后总权重为零也拒绝。`priority` 全为零时所有候选并列。

这里的通道 ID 指 `Server.ProviderFlow.Providers` 中实际加载的配置 ID，不是任意后台通道资料记录的 ID。

## 限额、数量和 UTC+8 时段

`limits` 是整组的约束；`rules` 只允许列出 `channel_ids` 内的通道，值使用同一限制结构。两层同时适用。

| 限制键 | 类型与边界 | 行为 |
|---|---|---|
| `min_amount` | 元的定点字符串，如 `"0.01"` | 单笔金额下界，包含边界。0 为不设下界。 |
| `max_amount` | 元的定点字符串 | 单笔金额上界，包含边界。0 为不设上界；非零时不能小于下界。 |
| `daily_amount` | 元的定点字符串 | UTC+8 当日累计分配金额，包含本次申请；0 为不限制。 |
| `daily_count` | JSON 整数，0–1000000 | UTC+8 当日分配数量；0 为不限制。 |
| `rate_limit_seconds` | JSON 整数，0–86400 | 滚动窗口秒数。 |
| `rate_limit_count` | JSON 整数，0–1000000 | 滚动窗口允许的分配数量。窗口两项必须同时为正或同时为零。 |
| `start_hour`、`end_hour` | 两项同时提供，JSON 整数 0–23 | 固定 UTC+8 的小时窗口，包含起止小时。起点大于终点表示跨夜；两者相等按原逻辑表示全天。 |

例：22–2 包含 22:00–23:59 与 00:00–02:59，03:00 不符合。UTC+8 当日从当地 00:00 开始；不随容器或宿主机时区改变。金额在程序中转为整数分，统计使用订单支付总额，不是商户净收入、余额或实际已支付金额。

金额必须是字符串，不能是 JSON 浮点数、指数、负数、前导零或超过两位小数。数量/小时必须是 JSON 整数，不能用 `"60"`、`60.0`、`6e1` 替代。解析保留整数精度，拒绝重复键、未知键、数组及尾随 JSON；每个文本最多 20000 字节。

组级额度按规则 ID 统计其新分配。通道级额度统计该通道的分配，并纳入没有新路由记录的历史 provider attempt 所对应的订单；同一订单不会同时作为新分配与历史 attempt 重复计数。通道统计不是仅本商户的成功交易数。

## 配置示例

在仅使用本地模拟/测试通道的环境，通过有权限的管理员会话和 CSRF 保护，向 `/console/v1/admin/resources/pay-rolls` POST：

```json
{
  "name": "测试支付宝轮询",
  "merchant_id": "1000",
  "type": "alipay",
  "strategy": "round_robin",
  "channel_ids": "7:2,8:1",
  "enabled": true,
  "limits": "{\"min_amount\":\"0.01\",\"max_amount\":\"1000.00\",\"daily_count\":1000,\"daily_amount\":\"10000.00\"}",
  "rules": "{\"7\":{\"start_hour\":22,\"end_hour\":2,\"rate_limit_seconds\":60,\"rate_limit_count\":20}}"
}
```

7/8 只是示例，必须换成测试环境实际加载、同类型且商户有权使用的通道。示例策略为轮询，因此 2:1 权重不会改变轮询比例；需要按权重选择时显式使用 `weighted`。给规则设置 `enabled=true` 不会打开部署的支付写开关，也不会加载任何外部凭证。

## 候选产品与接入入口

候选必须同时满足当前运行配置、商户白名单、相同支付 type、请求方法与身份前提。先调用原通道路由的纯校验，再执行适配器本地 `ValidateCreate`、扫码验证器或小程序材料前提检查；不在候选验证中发送付款或兑换身份。

普通支付 API 使用 `providerRoutedRequest(ctx, merchantID, params)`，保留已配置且满足请求的产品；默认方法不会选择 scan。扫码必须显式请求并通过付款码校验，JSAPI、OAuth、小程序和预授权仍有各自的可信身份与完成流程。规则不能自动补造 OpenID、换 AppID 或把一种产品伪装成另一种。

聚合收款和余额充值使用 Go 调用端固定的 `providerRoutedBrowserRequest`。它在选择与提交配额前只允许 page/wap/native/h5/app，排除 scan、jsapi、mini、applet、preauth；请求中的伪造开关不能放宽此范围。充值业务自身进一步只接受支付宝 page/wap、微信 native/h5。

各入口在分配前校验金额、订单号、名称长度、URL、实际或已签名 IP 与业务风控。支付 API 不修改已经验签的参数集合；用副本传入解析后的 IP。聚合收款采用 `collect:{收款码ID}:{external_no}`，充值采用 `recharge:{external_no}` 的稳定商户订单号，并在开始支付时重建原订单元组后重用分配。

显式 `channel_id` 不能绕过启用规则的候选、额度、商户白名单或产品校验；绑定以后也不能用它更换通道。组内没有合格候选时拒绝，不访问其它上游。

## 持久绑定、并发和配额事务

`(merchant_id, out_trade_no)` 是路由唯一键。分配保存通道、实际路由方法、金额、规则快照和带密钥 HMAC。HMAC 绑定商户/订单、通道及可信 AppID、方法、订单不可变内容和相关买家身份；不绑定请求时间戳、签名值或可变化的连接 IP。付款码只进入 HMAC，不保存原文。

事务先锁商户订单键，并在已有订单时取得订单行锁；随后按规则和排序后的通道 ID 取得数据库事务锁，再读取数据库时钟、检查组/通道额度、选择并写入分配，最后更新规则计数和轮询游标。唯一约束和锁保证不同服务实例、并发重试只分配并递增一次。分配提交以后，HTTP 层才调用支付流程；任何上游请求都不在路由事务锁内执行。

**配额是保守的申请计数，不是资金冻结或成功收款额度。** 首次分配提交即计入数量和金额；后续建单失败、进程中断、支付失败或 unknown 都不会返还该计数，也不会删除分配以便重试。滚动窗口/当日自然过去后不再占对应窗口，但不可变绑定仍在。已有绑定的合法重试不会再次累计，也不因当前额度耗尽而另选通道。

数据库 trigger 同时拒绝修改/删除分配，并拒绝在另一个通道或方法上插入付款 attempt。applet 路由只允许映射为该类型对应的底层付款方法（微信 jsapi、支付宝 mini），不能任意更换产品。

## 撤权、历史订单与 unknown

| 已有状态 | 后续请求处理 |
|---|---|
| 有路由绑定，尚无付款 attempt | 保留原分配；重新检查当前白名单、产品及身份前提。原规则被禁用、改为不匹配作用域/type 或移除该通道时拒绝，不改选。不会重新计算已预占的窗口额度。 |
| 已有付款 attempt，包括 processing/unknown/failed | attempt 的通道与方法优先。规则禁用或顺序变化不会改选；仍检查当前许可、能力和身份，不能因撤权而向另一家重发。 |
| 历史 attempt 没有 34 路由行 | 使用其不可变通道，不按当前重试内容回填新的身份 HMAC，不消耗新轮询计数；后续原支付身份校验仍生效。 |
| 历史网页/小程序准备 intent 尚无 attempt | 保留原通道和路由产品；冲突或双重矛盾 intent 拒绝，不能当作新订单重新选组。 |

unknown、连接超时、上游未查到等结果都不是切换通道的依据。已提交资金请求只通过原 attempt 的可信查询/通知核对；配置编辑或重启服务不会授权第二次付款。路由拒绝不会凭空标记支付成功、创建入账凭证或代替 provider callback ACK。

## 迁移与本地验收

34 增加 `provider_route_assignments`、`provider_route_counters`、活动规则作用域唯一索引、时间统计索引和 attempt guard；不自动创建或启用规则，也不修改历史付款 attempt。已有迁移按 SHA-256 校验，34 冻结后修复须增加新迁移，不能改写该 SQL。

按项目既有本地 Compose 流程先运行 migrate 服务，再启动 API/Worker，并检查 `/readyz`。这里只描述本地迁移验收，不指示连接生产、导入真实通道或打开真实资金写开关。上线后的逻辑回退应停用规则/服务并保留不可变路由及 attempt 证据，不能通过删表、重置计数或更换订单号处理 unknown。

使用单独的本地测试 DSN、随机 schema 可重跑：

```powershell
$env:GOEPAY_TEST_DATABASE_URL='<本地 PostgreSQL 测试 DSN>'
go test -count=1 ./internal/backoffice ./internal/server ./tests/contract -run 'TestPayRoll|Test.*Recharge|Test.*Collection|Test.*Mini|TestProviderHTTP|TestScanHTTP'
go vet ./internal/backoffice ./internal/server ./tests/contract
```

验证包括四策略的选择边界、严格 JSON 保存与重复启用规则拒绝、真实本地上游轮询、两个独立 Server 共用游标、12 并发同订单单分配/单提交、12 个不同订单限额只准入 3 个、金额/时段/滚动与日累计边界、旧资料不执行、撤权和 unknown 不换通道，以及聚合收款/充值不绕过策略且未付款不入账。证据位于 `internal/backoffice/pay_rolls_test.go`、`internal/server/provider_policy_routing*_test.go` 和 `tests/contract/provider_policy*_test.go`。独立只读复审未发现当前范围内 P1/P2；这不扩大到生产权限或未实现的其它插件产品。
