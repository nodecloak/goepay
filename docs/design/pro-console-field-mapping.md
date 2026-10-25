# Pro控制台字段、列顺序和交互对照

范围是本地Pro的订单、商户、商户资金记录和统计页面，不是对加密控制器或真实上游行为的推断。静态标签只证明原界面存在该字段/操作，不证明当前Go后端支持或允许同样操作。

实际扫描了 `epay_pro/public/static/assets` 的204个JS文件，用Babel AST提取114个含字段、接口或路由引用的chunk。完整结果见 `pro-console-field-inventory.json`；每个字段保存源文件SHA256和原始UTF-16字符偏移（Babel node.start，不是UTF-8字节偏移）。人工语义对照、缺口和API对应见 `pro-console-field-mapping.json`，可用 `.tools/extract-pro-console-fields.mjs` 和 `.tools/map-pro-console-fields.mjs` 重建。未读取真实配置、密钥、生产数据，也未解密控制器。

## 直接来源与原列顺序

| 页面 | 本地Pro chunk | 主表顺序（不是弹窗子表） |
|---|---|---|
| 管理员订单 | `index-lAIAtQd1.js`，主表offset17689起 | 系统订单号、商户号、商品名称、实际支付、支付方式、支付IP、扩展参数、创建时间、支付状态、通知状态、操作 |
| 商户订单 | `index-Dk-66xD5.js`，主表offset12816起 | 系统订单号、商品名称、订单金额、实际支付、支付方式、创建时间、支付状态、通知状态、操作 |
| 商户管理 | `index-DzbjAsrt.js`，主表offset39084起 | 商户号/用户组、余额、结算账号/姓名、联系方式、注册时间/域名、状态、操作 |
| 管理员资金明细 | `index-WFOFLbSD.js`，主表offset2525起 | 商户号、操作类型、变更金额、变更前金额、变更后金额、时间、关联订单号、操作 |
| 商户资金明细 | `index-ChSQvhrR.js`，主表offset2114起 | 操作类型、变更金额、变更前金额、变更后金额、时间、关联订单号 |
| 通道统计 | `index-hT-pj3fY.js` | 支付通道、支付金额、退款金额、手续费利润、总订单数、成功订单数、支付成功率 |
| 商户统计 | `index-CylHtmhC.js` | 商户ID、按支付方式/通道和统计类型动态展开、总计 |
| 买家统计 | `index-_M2b8ir_.js` | 支付账号/IP/手机号、付款笔数、付款金额、最新订单号、是否拉黑、操作 |
| 日统计复用组件 | `index-DNfqr2Hp.js` 内统计组件 | 日期、支付金额、退款金额、手续费利润、总订单数、成功订单数、支付成功率 |

管理员入口引用 `index-IIq1IliX.js`，商户入口引用 `index-_K93c9Ev.js`。inventory中的routes是能直接读出的静态路由引用，未声称动态/后端菜单分发已全部恢复。全部204个文件均扫描，不把未出现静态字段的库文件误称业务页面。

## 第一批实际实现

- 订单表按Pro主要列组织。系统单号下展示商户单号与已有接口单号；金额下明确展示商户入账、平台手续费和成功累计退款；支付方式下展示实际通道ID，避免把每个API字段都铺成新列。商户视角不显示管理员商户筛选列。字段缺失时不编造值。
- DataTable与订单详情用BigInt将整数分转换为元，保留int64精度，长ID不转Number。`paid_money/net_money/fee_money`已经是元字符串，保持原值；schema的money输入也保持原元字符串。
- 支付状态、收入冻结状态和通知任务状态分开显示。通知只能由 `none/pending/running/succeeded/dead` 映射，不能从订单paid推断已通知。未支付、未知、已全额退款或仍冻结的订单不能从表格按钮发起退款；这只是UI约束，后端仍是授权来源。
- 商户ResourcePage去掉重复ID，商户号下按专用投影展示有效用户组/成员期限；列表按schema展示名称、可用余额、脱敏结算资料、邮箱、注册时间/已审核域名、启用、登录账号、签名算法。基础组账务快照等低频字段仍在获准详情内。余额单元格分别展示可用余额、冻结预留、待解冻收入，绝不混合计数。API密钥/密码/token/私钥不会因动态列枚举而进入列表或详情，编辑时继续遵循write-only规则。
- 报表按维度、金额、数量、成功率组织。成功率仅从当前API的成功笔数/总笔数精确派生；没有分母时显示未知，不伪造0%。提供北京时间今天/昨天/最近7天/最近30天，日期区间匹配现API最多367天。
- 保留现有详情、查单、关单、退款、通知、收入冻结和授权操作组件中的服务端权限门；没有复制Pro的强制补单、任意改状态或编辑接口订单号等按钮。

App.vue归root所有，集成点是：

```ts
import { proListColumns } from './pro-console-fields';
const columns = computed(() =>
  proListColumns(kind.value, area.value, items.value, columnSets[kind.value] || []),
);
```

这样不会继续自动追加API任意未知key。该helper已实现，本任务不编辑App.vue、style.css或MenuTree。

## 订单字段与语义边界

| Pro字段 | 当前Go数据 | 显示/限制 |
|---|---|---|
| trade_no / out_trade_no / uid | 同名系统/商户单号、merchant_id | string；不做浮点转换 |
| money | amount（整数分） | 订单金额（元） |
| realmoney / 商户分成 | amount / merchant_amount | 不把merchant_amount冒充实际支付；明确区分订单金额与商户入账 |
| 手续费利润 | platform_fee | 显示“平台手续费”，不是扣除通道成本后的利润 |
| api_trade_no | root新增只读投影 | 已验证收据优先，已存attempt provider ID回退；不允许编辑 |
| 支付方式/支付通道 | type / channel_id | 支付方式中文名，通道ID string；未分配为空 |
| 支付账号/IP | buyer / client_ip | 原有会话权限和租户范围内的只读投影；不新获取用户信息 |
| notify | notification_state | 独立任务状态，不等于付款状态 |
| 退款金额 | refunded_amount | 只累计成功退款，分→元 |
| 创建/完成时间 | created_at / paid_at | 按服务端已有数据；无退款完成时间不伪造 |
| 已退款/已冻结/预授权 | 成功退款额、income_status、authorization对象 | 不把Pro数字status强行套到新订单status枚举 |

root协同新增列表只读投影和详情 `reporting`，前端同时支持投影在列表行以及详情reporting对象中，不通过provider查单填列表字段。现字段amount/merchant_amount/platform_fee单位不变。

Pro搜索字段包括系统/商户/接口单号、渠道商户/交易单号、商品、金额、实付、分成、域名、支付账号、IP、手机号、扩展参数。当前API明确支持q（系统/商户单号及商品的组合搜索）、type/channel/status/from/to/buyer/client_ip/income_status，管理员额外merchant_id。未实现精确search_field/金额/域名/手机号/通知或结算状态筛选，不发送会被后端忽略的假参数。

Pro订单弹窗还含结算、子订单、渠道子商户、地理位置、实名和退款时间；不存在的数据准确列为缺口，不能从IP猜省份、从订单paid推断结算、从冻结余额推断买家预授权。

## 商户字段与专用HTTP增量建议

原Pro `account` slot直接组合 `settle_id/account/username`；`contact` slot读取 `qq` 与 `phone||email`；`addtime` slot组合 `addtime/url`；`uid` slot组合uid/groupname，编辑gid/endtime。当前商户主路由实际走 `/resources/merchants`，不能拿通用 `/merchants` 的不同响应猜测资源字段。

| 缺口 | 当前事实 | 建议后续增量（本批不改Go） |
|---|---|---|
| 商户余额 | root协同新增只读available_minor、frozen_minor、income_held_minor，均为分字符串 | available来自可用账户；frozen与Store.Balance.Reserved同口径，仅reserved/settlement_reserve/channel_sharing_reserve；待解冻收入order_income_reserve单独展示，不重复计入冻结；创建/编辑禁止提交 |
| 结算账号/姓名 | settlement_recipient使用既有masked DTO，configured/missing/unavailable | 仅显示通过格式校验的mask/type；unavailable不显示旧mask，更不回旧profile明文account/name；不复制明文到通用列表 |
| 联系方式 | schema现有email；没有phone/qq专用契约 | 先定义权限与脱敏投影；不得因为Pro slot就新增明文PII存储/导出 |
| 注册时间/域名 | created_at为原merchants注册时间；approved_domains来自owner内approved+enabled记录，sorted去重 | 仅文本展示，不创建外链、不推断任意url；禁止在创建/编辑提交注册时间/域名聚合 |
| 用户组名称/有效期 | effective_group_id/name、membership_expires_at、group_membership_active来自专用membership投影 | 组未知时不回退profile.group_id；原到期成员时间独立标注；不把历史基础组未解决状态猜成当前组，全部只读 |
| 支付/结算/实名状态 | enabled只有账户开关 | 独立权限/实名来源，不把enabled当作全部权限开启 |
| 排序/高级搜索/导出 | 当前资源列表只有q和分页 | 提供字段白名单、稳定排序、filter共享和脱敏CSV；不支持密钥搜索或明文导出 |

原Pro有充值扣款、余额直接修改、删除商户、重置/展示密钥和私钥、手动提现等交互。只有当前后端已有的审计与资金授权专门动作才能显示；本批不复刻直接余额覆盖或明文密钥查看。

已知默认用户组的明确投影是effective_group_id=`"0"`和名称“默认用户组”，与未知的空字符串不同；前端支持真实0默认态，不把unknown猜为0。peer独立审查发现该差异后，实际RED/GREEN证据在 `.tools/pro-console-default-group-{red,green}.log`。ResourcePage还用上下文epoch和AbortController清理旧schema/list/detail，避免client/resource/权限变化后的旧响应回显；复现与修复见 `.tools/pro-console-resource-context-{red,green}.log`。

## 资金记录与报表剩余缺口

`/ledger`的amount是分，kind是账务账户类别，description才是业务说明，event_key是业务请求引用。它们分别明确展示，不把kind伪装成Pro操作类型，不把任意event_key伪装成订单号。root协同新增account_id、balance_before、balance_after：按同一physical account_id的原不可变entry.id完整历史累计，后端在过滤/分页前计算，CSV同口径。前端仅将已存投影精确转元，标题明确“账户变更前/账户变更后（元）”，保留账户类别与ID，不称合并商户总钱包、不在分页内滚算。专用operation_kind/trade_no仍是缺口。不可删除不可变分录。

报表现有dimension/orders/paid_orders/paid_money/net_money/fee_money；金额已经是元字符串。原退款金额、通道成本后利润、手机号/IP分组、买家最新订单和拉黑状态不是现API字段。后续统计增量应明确退款成功时间与原订单创建时间归属、退款累计去重和通道成本来源；不能用fee_money冒充净利润。商户统计的type/method和管理员merchant筛选也须有专用固定SQL契约后再展示。

本页CSV可包含本地派生成功率；服务端全量CSV仍保留既有字段契约，不谎称多出了服务端未实现字段。报表比例基于当前API订单状态定义，不新增“已核实上游”的证明含义。

## 验证证据

实际RED/GREEN日志在 `.tools/pro-console-*.log`。组件测试覆盖精确分→元、超长ID、credential列不可见、商户紧凑列/三类只读余额/注册时间、编辑请求不提交只读账务字段、详情reporting投影、通知与付款状态分离、北京时间日期快捷范围、已支付/未支付退款按钮及报表成功率。纯函数测试覆盖int64边界、负数、已舍入JSON数字拒绝、merchant owner筛选范围、缺失字段不编造。

隔离验证使用工作源码Vite `127.0.0.1:5183`，所有console API由Playwright拦截；没有数据库、Docker或真实支付上游调用。首轮字段/筛选/CRUD/CSV/订单交互13项通过；冻结/授权/通知/历史退款/打印关联流程17项通过；独立壳布局审查7项通过。商户余额增量后相关9项再通过。实际截图保存在对应 `web/test-results/runs/*/pro-console-fields-*/`。构建成功，原有主包大于500kB提示仍存在；本次未修改分包策略。

独立布局审查复现订单搜索框在1280px桌面仅24px，真实RED日志为 `.tools/pro-console-search-width-red.log`；样式owner修复desktop工具栏换行后，同一断言实际GREEN在 `.tools/pro-console-search-width-green.log`。未越权修改ConsoleShell/MenuTree/style.css。

2026-10-24最终字段批次回归：`npm test` 全部50文件/177测试通过（3.65s），日志 `.tools/pro-console-final-unit.log`；`npm run build` 含vue-tsc成功，日志 `.tools/pro-console-final-build.log`。商户投影RED为缺失余额/注册列，GREEN为组件/资源19测试和相关mock browser9测试，分别见 `.tools/pro-console-merchant-projection-{red,green,browser-green}.log`。所有状态由实际断言得出，不将这批前端mock回归声称为真实上游/全项目验收。

后续同轮追加了商户masked/effective-group/domains和账户前后金额投影，以及独立预授权只读观察组件。阶段完整单测52文件/204测试通过（`.tools/pro-console-expanded-final-unit.log`），追加0默认组修正单测8通过；候选74前最终当前工作源码回归52文件/214测试全部通过3.36s（`.tools/pro-console-final-integrated-unit.log`，含协作者新增返现UI测试）。`npm run build`（含vue-tsc）通过，日志 `.tools/pro-console-expanded-static-build.log`。当前截图与45项关联mock browser在只读构建快照 `.tools/pro-console-browser-7710b65c76d144d1bf10489f02dcbc64` 验证，全45通过13.6s（`.tools/pro-console-expanded-static-browser.log`），使用127.0.0.1:5184且没有API代理。此前Vite协作修改引起HMR的一轮43通过/2超时保留日志与trace，不当作成功证据，稳定快照已通过对应原测试。
