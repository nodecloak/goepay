# 双后台全页面UX复核与整改记录

用户给出的订单截图仅是示例，范围为管理后台和商户后台全部页面。已经实际核对82个原菜单→组件→JS来源，并在当前固定98部署逐页读取全部82页DOM及截图。原始本机证据：`E:\GOEPAY\.tools\all-ux-live-b4a190cbaa5947109037346b579bf481`。没有触发业务写入、真实资金或外部请求。

最新独立模拟预览为127（8187/API8186、schema88/202表），见[127验收记录](local-preview-ux127-2026-10-25.md)。全部82页已重新读取DOM/截图，0业务写入、外部请求、页面/HTTP错误和页面溢出。下表保留98观察基线并另列127实机；观察通过不代表全部条件动作已经对齐。

最新独立模拟预览为122（8187/API8186、schema88/202表），见[122验收记录](local-preview-ux122-2026-10-25.md)。已重新读取全部82页DOM/截图，0业务写入、外部请求、页面/HTTP错误和页面溢出。下表发现保留98观察基线，进度和页面索引另列122实机，不能把旧按钮误作现状；观察通过仍不代表全部条件动作已经对齐。

**结论：当前界面与原Pro仍有实质差异，不能称全部功能或UX一致。** 0页面错误、0HTTP错误只能说明页面能运行，不能说明按钮、状态和业务已齐全。本记录区分已观察的页面职责错配、入口缺口与尚需按状态验证的功能，不用统一partial或菜单总数代替验收。

## 已确认问题与后续工作

| 编号 | 页面 | 实际发现 | 进度与要求 |
|---|---|---|
| UX-001 | admin/trade/order | 已支付/未支付行操作缺少原菜单分支，已有冻结与通知被藏入详情。 | row menus, batch controls, explicit auto refresh and original-source blacklist deployed in preview114; actual local workflow proof passed。已恢复paid/unpaid行菜单、确认、状态标记/归档恢复、独立手动退款、批量标记/归档/恢复和至少3秒显式刷新；IP/账号来源比对、天数期限、同号重试及到期政策有合同测试，原IP实际拦截后停用。批量资金动作、子订单、接口订单号和同步通知仍继续，不能称整个订单页已齐全。 |
| UX-002 | admin/merchant/group-buy | 固定98把管理员销售配置路由映射为购买页面。 | deployed in preview109; full frontend and affected Go gates passed; full page equivalence not asserted。原五类会员奖励字段、邀请码和只读来源/预算的专项整改已部署独立预览；其余收费、完整组配置与原条件行为仍逐项核对，不把已有卡片当整项完成。 |
| UX-003 | admin/system/settings | 固定98把原17类设置分组表单替换为通用键值CRUD。 | 17 groups and supported typed controls remediated in working source; targeted tests passed; remaining original settings still need implementation。已恢复17分组、中文控件、元/分/基点精确转换、只保存明确编辑项、分页、只读及失败保护，并保留管理员消息历史。未接入的原配置与PHP/Swoole运行差异仍明确说明，不能称所有设置已齐全。 |
| UX-004 | admin/other/onecode<br>admin/other/onecode/edit<br>merchant/onecode | 固定98把原专用样式编辑页映射为列表；商户原直接生成入口仍被记录列表替代。 | admin style editor and merchant direct generator deployed in preview109 with actual browser proof。已恢复原样式卡片、直接编辑、背景/尺寸/QR/文字及拖动缩放；商户双栏直接生成、名称/受控域名/风格/固定金额、分享链接和下载已实现。实际提交后丢响应跨SPA同号找回只生成一码/一次审计，900×1200海报、摘要篡改403、停用404及归档保留来源通过。当前只列部署支付origin，额外域名需显式部署支持；原全局名称保存/开关细节仍需按剩余设置统一复核。 |
| UX-005 | admin/merchant/merchant-list<br>admin/payapi/pay-channel<br>admin/merchant/group-list | 当前主要为通用详情/编辑/删除，原专用筛选、状态切换、通道绑定/测试及配置分组未逐项对齐。 | original group-name picker and administrator merchant login-log deployed through preview126; dedicated group/channel review continues。phone/qq/url真实字段、基本/密钥分区、124原组名选择/分页/搜索和大ID保留已实机验证；126恢复更多→登录日志，operator/super按核实目标主体只读，未知方式/地域不猜，桌面/375、分页关闭回焦与跨商户迟到响应保护通过。联系方式不自动变成登录别名；完整组配置、专属通道/子通道与其余原动作继续。 |
| UX-006 | admin/merchant/invite-code | 固定98原批量生成、使用状态、使用时间/使用者和清空分支被通用表单替代。 | deployed in preview109; actual preview105 commit-then-lost-response replay and archive proof passed。已部署随机码批量生成/复制、状态筛选、使用时间/使用者、删除与清理入口；原请求号和归档来源保护已验证。仍需结合全页条件行为继续验收，不能用专项通过宣称全部UX相同。 |
| UX-007 | admin/dashboard<br>merchant/dashboard | 当前仪表盘仅少量指标和刷新按钮，原概览模块和图表交互未对齐。 | main original overview modules deployed in preview114; actual admin/merchant/mobile proof passed; remaining details pending。恢复原四卡今日/昨日、七日趋势/明细/支付方式筛选、占比图、待办、公告、最近结算和当前统一组费率；Go/PostgreSQL显示实际运行版本。只用真实付款时间/成功出款凭证，未知成本保留未知，过期管理员响应不能覆盖商户数据。联系人/开关、额外原快捷筛选等仍需继续，不用新图表出现代表完整等同。 |
| UX-008 | admin/trade/stat/user<br>admin/transfer/user-stat<br>admin/complain/charts | 固定98通用报表错误地给投诉显示支付净额和手续费，商户统计查看方式仍缺失。 | original merchant matrix controls and runtime-configured zero dimensions deployed through preview126; detailed statistics review continues。支付/代付商户统计按方式或通道、金额指标、日期、排序、总计、分页及同口径CSV已恢复；126加入真实运行配置中的零交易通道/方式，历史列和未知成本保留，不把无交易配置捏造为商户行，不把未绑定的通道资料ID冒充真实通道。实际空日期1/2通道与CSV、375布局通过，其余统计字段/图表继续。 |
| UX-009 | admin/transfer/list<br>merchant/transfer | 原付款列表缺少已实现批次功能入口。 | batch entry deployed in preview109 in both areas; remaining product filters/actions pending。两个原列表均已链接真实持久批次页，继续复核原产品专属筛选、余额及行操作；不以入口恢复代替全部付款UX一致。 |
| UX-010 | merchant/settle/apply<br>merchant/applyments/create<br>admin/applyments/form<br>merchant/applyments/form | 固定98把专用申请路由映射为列表。 | direct forms deployed in preview109; component and desktop/mobile checks passed; real onboarding plugin deferred。提现直接呈现资金/收款/费用表单；三个本地进件资料入口直接新增或按query.id读取编辑，规范ID/上下文切换有保护。保存后不自动重复新增。真实进件插件仍按用户范围暂缓，不能把本地草稿宣称为上游进件。 |
| UX-011 | merchant/account/settings<br>admin/system/cron<br>admin/system/runtime-log<br>admin/system/update | 账户设置的绑定/通知、任务和日志等原交互与当前页面职责/布局仍有差异。 | original account same-page cards and verified own login-log deployed in preview122; remaining bindings and administrator tools pending。账户/密码/TOTP、消息推送、小票打印在原设置页组合现有真实组件；本人日志原四列、只读快照分页、核实主体、真实时间排序、未知方式/地域、代登录拒绝、375布局/关闭回焦通过。原手机/邮箱验证、第三方绑定、云设备专属连接与管理员任务/日志/更新动作仍需逐项核对，不显示无后端的空页签。 |
| UX-012 | admin/profitsharing/receiver<br>admin/profitsharing/order<br>merchant/profitsharing/receiver<br>merchant/profitsharing/order<br>admin/settle/list<br>merchant/settle/list | 原规则/记录列表和操作布局，与当前专用资金流程存在入口和状态表达差异。 | original settlement1/2/3 semantics deployed in preview122 with new-intent and historical-proof checks; other per-state flows pending。原组1自动/2手动/3两者、继承和普通全局0关闭均被新报价/预留和普通计划消费；阈值分支按原独立规则，会员到期重查，旧意图/历史HMAC保留。继续核对原费用、周期、组覆盖及分账列表/规则状态，不能把本项代表全部结算或渠道分账UX。 |
| UX-013 | admin/trade/refund<br>merchant/trade/refund | 原退款列表的六项退款统计弹窗缺失。 | original six-field filtered refund statistics deployed in preview127; scoped DB and actual dual-console checks passed。恢复退款总笔数/金额、成功笔数/金额、处理中笔数/金额，当前筛选全部记录，不受分页限制；金额/计数精确字符串，处理中含待核实，不把失败算作成功。双角色租户、日期/方式/状态/搜索、失败清空、迟到账号响应、375布局/关闭回焦通过。实机预览为空退款，非零混合状态由隔离真实PG合同验证；完整原退款搜索字段/状态交互继续核对。 |

## 全部页面复核索引

原字段/动作与98基线及127实机的按钮/表头/表单均在同名JSON中保存；原静态字段含表单和筛选，不等同全部表格列。空表不能证明没有行操作，条件动作另按源码/专项/实机核对。其它业务插件按用户指定暂缓。

| 页面 | 原模块 | 98页面类型 | 98基线入口 | 127实机入口 |
|---|---|---|---|---|
| admin/dashboard · 仪表盘 | index-BayLJ6d8.js | dashboard | 刷新数据 | 刷新数据、查看趋势明细 |
| admin/trade/order · 订单管理 | index-lAIAtQd1.js | orders | 刷新数据、查询、清空筛选、导出筛选结果 CSV、详情、申请退款、上一页、下一页 | 刷新数据、查询、清空筛选、自动刷新、导出筛选结果 CSV、执行批量操作、拉黑IP、详情、···、上一页、下一页 |
| admin/trade/refund · 退款记录 | index-CaPsjcRn.js | refunds | 刷新数据、查询、清空筛选、导出筛选结果 CSV、上一页、下一页、查询历史记录 | 刷新数据、查询、清空筛选、导出筛选结果 CSV、退款统计、上一页、下一页、查询历史记录 |
| admin/trade/blacklist · 黑名单管理 | index-Dd_3yCyx.js | resource | 查询、刷新、新增黑名单项、上一页、下一页 | 查询、刷新、新增黑名单项、详情、编辑、删除、上一页、下一页 |
| admin/trade/stat/daily · 每日交易统计 | index-C2O4Lmyd.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| admin/trade/stat/user · 商户交易统计 | index-CylHtmhC.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、重置、导出本页 CSV、导出筛选结果 CSV、商户ID ↕、总计 ↓、支付宝 ↕、微信支付 ↕、上一页、下一页 |
| admin/trade/stat/channel · 通道数据统计 | index-hT-pj3fY.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| admin/trade/stat/province · 地区数据统计 | index-DF-qDpRw.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| admin/trade/stat/buyer · 支付用户统计 | index-_M2b8ir_.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| admin/profitsharing/receiver · 分账规则 | index-ydfe_neb.js | channel-sharing | 读取分账数据、加密保存草稿、读取当前规则、添加已绑定接收方、发布新版本、禁用当前规则 | 读取分账数据、加密保存草稿、读取当前规则、添加已绑定接收方、发布新版本、禁用当前规则 |
| admin/profitsharing/order · 分账记录 | index-BcN7mYkh.js | channel-sharing | 读取分账数据、核对原支付声明 | 读取分账数据、核对原支付声明 |
| admin/profitsharing/internal · 站内分账规则 | index-CxvCqzdZ.js | resource | 查询、刷新、新增站内分账规则、上一页、下一页 | 查询、刷新、新增站内分账规则、上一页、下一页 |
| admin/transfer/list · 付款记录 | index-CYTHVGS4.js | transfers | 查询、申请付款、导出筛选结果 CSV、付款详情、上一页、下一页 | 查询、申请付款、导出筛选结果 CSV、上一页、下一页 |
| admin/transfer/stat · 付款账号统计 | index-D0BdOeFT.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| admin/transfer/channel-stat · 通道数据统计 | index-WNu3YHg3.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| admin/transfer/user-stat · 商户代付统计 | index-CDCxaHWS.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、重置、导出本页 CSV、导出筛选结果 CSV、商户ID ↕、总计 ↓、支付宝 ↕、微信支付 ↕、上一页、下一页 |
| admin/settle/list · 结算管理 | index-Cpj26fBW.js | workflow | 刷新、申请结算、核准所选 0 条申请、核对结算出款、上一页、下一页 | 刷新、申请结算、核准所选 0 条申请、上一页、下一页 |
| admin/settle/batch · 批量结算 | index-5nUERs6a.js | settlement-batches | 刷新数据、查询、刷新、生成结算批次、查看、第一页、下一页 | 刷新数据、查询、刷新、生成结算批次、第一页、下一页 |
| admin/merchant/merchant-list · 商户列表 | index-DzbjAsrt.js | resource | 查询、刷新、新增商户、代登录商户、详情、编辑、上一页、下一页 | 查询、清空商户筛选、刷新、新增商户、更多、代登录商户、详情、编辑、上一页、下一页 |
| admin/merchant/group-list · 用户组设置 | index-CCjDRx0B.js | resource | 查询、刷新、新增用户组、上一页、下一页 | 查询、刷新、新增用户组、详情、编辑、删除、上一页、下一页 |
| admin/merchant/group-buy · 用户组购买 | index-B2FwSy03.js | commerce | 刷新、购买会员、上一页、下一页 | 刷新配置、已开启、编辑简介、下架、保存全部 |
| admin/merchant/record · 资金明细 | index-WFOFLbSD.js | ledger | 刷新数据、查询、清空筛选、导出筛选结果 CSV、上一页、下一页 | 刷新数据、查询、清空筛选、导出筛选结果 CSV、上一页、下一页 |
| admin/merchant/internal-transfer · 站内转账 | index-DgvJvE3U.js | workflow | 刷新、发起站内转账、上一页、下一页 | 刷新、发起站内转账、上一页、下一页 |
| admin/merchant/domain · 授权域名 | index-BltpJOJD.js | resource | 查询、刷新、新增授权域名、上一页、下一页 | 查询、刷新、新增授权域名、上一页、下一页 |
| admin/merchant/invite-code · 邀请码管理 | index-ChX3aBWp.js | resource | 查询、刷新、新增邀请码、上一页、下一页 | 查询、清空筛选、生成邀请码、清空全部、清空已使用、刷新、上一页、下一页 |
| admin/payapi/pay-channel · 支付通道 | index-DNfqr2Hp.js | resource | 刷新状态、查询、刷新、新增支付通道、上一页、下一页 | 刷新状态、查询、刷新、新增支付通道、详情、编辑、删除、上一页、下一页 |
| admin/payapi/pay-type · 支付方式 | index-NXVshjHq.js | resource | 查询、刷新、新增支付方式、上一页、下一页 | 查询、刷新、新增支付方式、上一页、下一页 |
| admin/payapi/pay-roll · 支付通道轮询 | index-Cr4lev2n.js | resource | 查询、刷新、新增轮询配置、上一页、下一页 | 查询、刷新、新增轮询配置、上一页、下一页 |
| admin/payapi/pay-weixin · 公众号小程序 | index-DGzFU5v8.js | resource | 查询、刷新、新增公众号小程序、上一页、下一页 | 查询、刷新、新增公众号小程序、上一页、下一页 |
| admin/applyments/channel · 进件渠道配置 | index-DVcJ9Zy9.js | resource | 查询、刷新、新增进件渠道、上一页、下一页 | 查询、刷新、新增进件渠道、上一页、下一页 |
| admin/applyments/merchant · 进件商户管理 | index-DocsuwmU.js | resource | 查询、刷新、新增本地进件资料、上一页、下一页 | 查询、刷新、上一页、下一页 |
| admin/applyments/subchannel · 子通道管理 | index-C5U5W_96.js | resource | 查询、刷新、新增子通道资料、上一页、下一页 | 查询、刷新、新增子通道资料、上一页、下一页 |
| admin/applyments/mchrisk · 商户管控记录 | index-9OC_cO5i.js | resource | 查询、刷新、新增商户管控记录、上一页、下一页 | 查询、刷新、新增商户管控记录、上一页、下一页 |
| admin/applyments/form · 商户进件表单 | index-DhN7Ku4q.js | resource | 查询、刷新、新增本地进件资料、上一页、下一页 | 保存 |
| admin/complain/list · 投诉记录 | index-Cl6xa9dJ.js | resource | 查询、刷新、新增本地投诉工单、上一页、下一页 | 查询、刷新、新增本地投诉工单、上一页、下一页 |
| admin/complain/charts · 投诉统计 | index-CHYeaio0.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 刷新统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| admin/complain/task · 同步任务 | index-Ce2eNQYC.js | resource | 查询、刷新、新增同步任务配置、上一页、下一页 | 查询、刷新、新增同步任务配置、上一页、下一页 |
| admin/other/risk · 风控记录 | index-CfnvDEoO.js | resource | 查询、刷新、新增风控记录、上一页、下一页 | 查询、刷新、新增风控记录、详情、编辑、删除、上一页、下一页 |
| admin/other/get-token · 获取用户标识 | index-BySTvbcv.js | identity-lookup | 原收款应用网页授权、独立应用与小程序、支付宝应用授权、读取授权通道、创建授权请求 | 原收款应用网页授权、独立应用与小程序、支付宝应用授权、读取授权通道、创建授权请求 |
| admin/other/onecode · 聚合收款码样式 | index-Dc9Owjji.js | resource | 查询、刷新、新增收款码样式、详情、编辑、删除、上一页、下一页 | 刷新、已启用、删除、上一页、下一页 |
| admin/other/onecode/edit · 编辑收款码样式 | index-C_AO16KG.js | resource | 查询、刷新、新增收款码样式、详情、编辑、删除、上一页、下一页 | 重新读取、同步图片尺寸、预览、保存样式 |
| admin/app/app-store · 应用市场 | index-CNLq1l1m.js | unavailable（指定暂缓） | 无可见按钮 | 无可见按钮；结合页面职责核对 |
| admin/app/plugin-list · 本地插件 | index-zEWI8e72.js | operation | 查询、刷新、新增本地扩展资料、上一页、下一页 | 查询、刷新、新增本地扩展资料、上一页、下一页 |
| admin/app/template · 模板配置 | index-CSBB45Vm.js | template | 首页、支付收银台、聚合收款页、重新读取模板、标准支付页本地内置标准布局、紧凑支付页本地内置紧凑布局、检查并保存模板设置、检查并设为当前模板、预览已保存配置 | 首页、支付收银台、聚合收款页、重新读取模板、标准支付页本地内置标准布局、紧凑支付页本地内置紧凑布局、检查并保存模板设置、检查并设为当前模板、预览已保存配置 |
| admin/app/addon · 扩展配置 | index-DS4XI152.js | resource | 查询、刷新、新增扩展参数、上一页、下一页 | 查询、刷新、新增扩展参数、上一页、下一页 |
| admin/system/settings · 系统设置 | index-C6la0DtP.js | resource | 查询、刷新、新增系统设置项、上一页、下一页、刷新提醒记录 | 刷新设置、网站信息、商户相关、支付相关、交易投诉、风控检测、结算规则、转账付款、快捷登录、消息提醒、实名认证、OCR配置、公告排版、邮箱配置、短信配置、IP地址、中转代理、高级设置、保存本页设置 |
| admin/system/cron · 计划任务 | index-CQrUK7ri.js | operation | 查询、刷新、新增计划任务、上一页、下一页 | 查询、刷新、新增计划任务、上一页、下一页 |
| admin/system/anounce · 公告配置 | index-DdUbXxev.js | resource | 查询、刷新、新增公告、上一页、下一页 | 查询、刷新、新增公告、上一页、下一页 |
| admin/system/admin-user · 管理员列表 | index-XE51Re1u.js | resource | 查询、刷新、新增管理员、详情、编辑、上一页、下一页 | 查询、刷新、新增管理员、详情、编辑、上一页、下一页 |
| admin/system/log · 登录日志 | index-CABO95hu.js | operation | 刷新、上一页、下一页 | 刷新、上一页、下一页 |
| admin/system/runtime-log · 运行日志 | index-EblIiRjx.js | operation | 刷新、上一页、下一页 | 刷新、上一页、下一页 |
| admin/system/clean · 数据清理 | index-DAJcazKr.js | operation | 预览清理范围 | 预览清理范围 |
| admin/system/update · 系统更新 | index-3p1ouazc.js | operation | 刷新、上一页、下一页 | 刷新、上一页、下一页 |
| admin/system/change-password · 修改密码 | index-7OFMkBOP.js | account | 修改密码、绑定身份验证器、设置资金密码、刷新资金安全状态 | 修改密码、绑定身份验证器、设置资金密码、刷新资金安全状态 |
| merchant/dashboard · 仪表盘 | index-l9QPFu6k.js | dashboard | 刷新数据 | 刷新数据、查看趋势明细 |
| merchant/account/api-info · API 信息 | index-CgFnG600.js | account | 重置 API 密钥、生成 RSA 密钥对 | 重置 API 密钥、生成 RSA 密钥对 |
| merchant/account/settings · 账户设置 | index-fMgyEUZ4.js | account | 保存账户资料、修改密码、绑定身份验证器 | 登录日志、保存账户资料、修改密码、绑定身份验证器、重新读取设置、保存设置、刷新通知记录、检查并保存打印设置 |
| merchant/account/certificate · 实名认证 | index-D_Mjs5yy.js | unavailable（指定暂缓） | 无可见按钮 | 无可见按钮；结合页面职责核对 |
| merchant/account/recharge · 余额充值 | index-DTbArJ8N.js | commerce | 刷新、余额充值、上一页、下一页 | 刷新、余额充值、上一页、下一页 |
| merchant/account/groupbuy · 购买会员 | index-Dx_Ne5Vo.js | commerce | 刷新、购买会员、上一页、下一页 | 购买此用户组、刷新、购买会员、上一页、下一页 |
| merchant/account/deposit · 保证金 | index-BRVOjsJa.js | commerce | 刷新、保证金操作、上一页、下一页 | 刷新、保证金操作、上一页、下一页 |
| merchant/account/internal-transfer · 站内转账 | index-dO26ItYn.js | workflow | 刷新、发起站内转账、上一页、下一页 | 刷新、发起站内转账、上一页、下一页 |
| merchant/trade/order · 订单管理 | index-Dk-66xD5.js | orders | 刷新数据、查询、清空筛选、导出筛选结果 CSV、详情、申请退款、上一页、下一页 | 刷新数据、查询、清空筛选、自动刷新、导出筛选结果 CSV、详情、申请退款、上一页、下一页 |
| merchant/trade/refund · 退款记录 | index-KNIUL4x0.js | refunds | 刷新数据、查询、清空筛选、导出筛选结果 CSV、上一页、下一页、查询历史记录 | 刷新数据、查询、清空筛选、导出筛选结果 CSV、退款统计、上一页、下一页、查询历史记录 |
| merchant/trade/daily-stat · 每日交易统计 | index-B5ldFjlS.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| merchant/trade/province-stat · 地区数据统计 | index-DhcBuQBV.js | report | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 | 查询统计、导出本页 CSV、导出筛选结果 CSV、上一页、下一页 |
| merchant/settle/list · 结算记录 | index-DLnTxwvH.js | workflow | 刷新、申请结算、核对结算出款、上一页、下一页 | 刷新、申请结算、上一页、下一页 |
| merchant/settle/settings · 结算设置 | index-DHh5rhKO.js | resource | 修改收款资料、刷新收款版本 | 配置收款资料、刷新收款版本 |
| merchant/settle/apply · 申请提现 | index-DsthpoT6.js | workflow | 刷新、申请结算、核对结算出款、上一页、下一页 | 刷新可出账金额、配置收款资料、刷新收款版本、确认提交 |
| merchant/transfer · 代付管理 | index-Cn-r1JA8.js | transfers | 查询、申请付款、导出筛选结果 CSV、付款详情、上一页、下一页 | 查询、申请付款、导出筛选结果 CSV、上一页、下一页 |
| merchant/record · 资金明细 | index-ChSQvhrR.js | ledger | 刷新数据、查询、清空筛选、导出筛选结果 CSV、上一页、下一页 | 刷新数据、查询、清空筛选、导出筛选结果 CSV、上一页、下一页 |
| merchant/domain · 授权域名 | index-llNNw-18.js | resource | 查询、刷新、新增授权域名、上一页、下一页 | 查询、刷新、新增授权域名、上一页、下一页 |
| merchant/complain · 交易投诉 | index-BSWogy41.js | resource | 查询、刷新、新增本地投诉工单、上一页、下一页 | 查询、刷新、新增本地投诉工单、上一页、下一页 |
| merchant/applyments/merchant · 进件商户管理 | index-iwaK_B1G.js | resource | 查询、刷新、新增本地进件资料、上一页、下一页 | 查询、刷新、上一页、下一页 |
| merchant/applyments/create · 新增商户进件 | index-DuKpkIHz.js | resource | 查询、刷新、新增本地进件资料、上一页、下一页 | 保存 |
| merchant/applyments/mchrisk · 商户管控记录 | index-Bh5aco2B.js | resource | 查询、刷新、上一页、下一页 | 查询、刷新、上一页、下一页 |
| merchant/applyments/form · 商户进件表单 | index-VU6rHS2G.js | resource | 查询、刷新、新增本地进件资料、上一页、下一页 | 保存 |
| merchant/profitsharing/receiver · 分账规则 | index-CCRqtej7.js | channel-sharing | 读取分账数据、加密保存草稿、读取当前规则、添加已绑定接收方、发布新版本、禁用当前规则 | 读取分账数据、加密保存草稿、读取当前规则、添加已绑定接收方、发布新版本、禁用当前规则 |
| merchant/profitsharing/order · 分账记录 | index-B3PsZvos.js | channel-sharing | 读取分账数据、核对原支付声明、核对原订单完结状态、刷新原完结记录 | 读取分账数据、核对原支付声明、核对原订单完结状态、刷新原完结记录 |
| merchant/subchannel · 支付通道 | index-WhNAEuqO.js | resource | 查询、刷新、新增子通道资料、上一页、下一页 | 查询、刷新、新增子通道资料、上一页、下一页 |
| merchant/onecode · 聚合收款 | index-DVR_b5pg.js | resource | 查询、刷新、新增收款码资料、详情、编辑、删除、上一页、下一页 | 重新读取、历史收款码、生成收款码 |
| merchant/invite · 邀请返现 | index-Ch8Og0zy.js | referral | 重新读取邀请资料、发行或轮换推广令牌、刷新待发奖励首页、待发奖励首页、下一页待发奖励、关系首页、下一页关系、奖励首页、下一页奖励 | 重新读取邀请资料、发行或轮换推广令牌、刷新待发奖励首页、待发奖励首页、下一页待发奖励、关系首页、下一页关系、奖励首页、下一页奖励 |

## 验证口径

- 保留固定98观察和127最新实机两套入口；后续增量按109/114/116/117/122/124/126/127验收记录区分。不得将专项或82页观察通过写成全部条件功能/UX对齐。
- 新操作保留角色/本人权限、CSRF、稳定原请求号、资金授权、审计与未知结果保护。原直接改状态/物理删流水与当前账务保护的差异，须在对应确认交互中明确。
- 原服务端控制器大量加密，前端存在按钮不证明其全部业务规则已恢复；同一支付插件的未实现分支不能归为其它插件暂缓。
- 后续按本表逐项修复并记录实际测试及部署范围；未完成项保留，不以“全部本地测试通过”替代功能验收。
