# 管理端与商户端逐节点浏览器测试（2026-10-30）

本轮使用 Chromium 实际访问线上 Go 站点及本地真实 Go 后端。线上 API 版本为 2026.10.30-209；本轮修改测试脚本与测试文档，没有部署新的业务代码。

补充说明：本报告验证页面和业务场景的可执行性，不代表交互设计已经验收通过。后续专项复核发现 2 项 P1 和 5 项 P2 问题，见 [交互合理性复核](2026-10-30-ux-review.md)。

## 验证结果

- 管理端 63 个、商户端 33 个路由，共 96 个节点，均检查桌面（1440×1000）与手机（375×812）布局。
- 84 个当前菜单节点通过页面、接口及可用的只读交互检查；4 个隐藏辅助页打开正常；7 个受开关限制的入口单独标记（管理端投诉 3 个返回明确的关闭提示及 HTTP 403，商户端 4 个未在当前菜单开放）；1 个实名认证入口按既有范围暂缓。最终巡检没有未解决的页面加载、脚本、非预期接口错误或横向溢出。
- 模拟接口的完整浏览器套件：524 通过、9 跳过、0 失败。跳过的本地集成场景随后单独启用真实后端执行，不能把这份模拟测试当作上游通道验收。
- 真实本地后端：核心套件 9/9 通过，补充写入套件 4/4 通过，均无跳过。两批独立执行；沿用正常登录频率限制，批次准备仅重置本轮专用测试数据库的登录计数。
- 本地 CLI 账务核对的 33 项结果均为 0。
- 新建容器 0 个。线上只提交账号登录，其他操作限定为读取、展开和取消；浏览器网络守卫未发生写入拦截事件，说明没有误点提交。线上巡检后仍为 2 笔原验收订单，合计 2 分，可用余额 2 分、预占 0 分，readyz 正常。

## 验证边界

线上每个节点按实际控件检查查询/刷新、筛选、选项、页签、详情、添加/编辑表单和操作菜单；表单打开后取消，不提交真实退款、付款、结算、删除或配置变更。空列表页只证明页面、筛选和空状态可用，不等于已用真实复杂业务数据验收。

本地写入使用专用数据库 goepay_ui210_20261030、模拟通道和随机测试凭据，未导入生产数据或通道密钥，未启动外部执行 worker。复用已有 PostgreSQL 测试池，保留原预览及其数据库。本轮测试结束后，已停止自有临时服务，确认专用库连接数为 0，再删除该测试库；没有删除容器或其他数据库。

实名认证以及用户已暂缓的进件、投诉等外部插件流程没有进行上游验收。生产关闭的出款/结算自动执行、外部通知、打印设备等也没有通过本轮线上巡检启用。本地投诉开关关闭时，集成测试明确断言关闭提示并附加 feature-disabled 注解；这三个页面没有被计为真实投诉通道验收。

## 订单操作核对

| 场景 | 浏览器中核实的操作 |
| --- | --- |
| 管理员，已支付 | 改未完成、API退款、手动退款、冻结订单、重新通知、删除订单 |
| 管理员，未支付（本地） | 改已完成、手动补单、删除订单 |
| 商户，已支付 | 查看详情、重新通知、退款、打印小票 |

本地还实际提交了订单标记、收入冻结/解冻、逻辑删除/恢复，并通过真实 API 核对状态。后台“改已完成”保持原支付事实不变，不凭标记增加余额。

## 真实后端写入与权限验证

| 场景 | 最终结果 |
| --- | --- |
| 聚合收款样式上传、收款码创建、二维码、公页展示、禁用撤销与删除 | 通过 |
| 授权域名增改查删、运营报表及账户读取 | 通过 |
| 商户申请结算、资金预占、管理员拒绝及状态核对 | 通过 |
| 本地全菜单读取；另行标记投诉关闭开关 | 通过 |
| 操作员菜单权限、直达页面及服务端 403 | 通过 |
| 资金密码确认、按原净额模拟出款、重复提交核对 | 通过 |
| 模板 iframe 预览与 CSP、平台及商户打印设置 | 通过 |
| 模拟单笔付款、费用、批量部分执行、取消剩余项目及停写后查询 | 通过 |
| 两端登录、工作台、订单列表、手机布局及头像菜单退出 | 通过 |
| 用户组新增、费率修改、重新加载核对及删除 | 通过 |
| 黑名单必填校验、新增、编辑、详情及删除 | 通过 |
| 公告新增、商户展示、隐藏、修改及删除 | 通过 |
| 未支付/已支付菜单、非资金标记、收入冻结/解冻、删除/恢复 | 通过 |

## 逐节点清单

“通过”表示本轮页面及所记录的只读交互检查通过，并非每个有副作用按钮均在线上提交。受开关或插件范围限制的入口保留独立状态。详情弹窗数量为桌面复测结果。

| 端 | 节点 | 路径 | 桌面 | 手机 | 已记录交互 / 弹窗 |
| --- | --- | --- | --- | --- | --- |
| 管理 | 仪表盘 | `/admin/dashboard` | 通过 | 通过 | 1 / 0 |
| 管理 | 订单管理 | `/admin/trade/order` | 通过 | 通过 | 5 / 3 |
| 管理 | 退款记录 | `/admin/trade/refund` | 通过 | 通过 | 3 / 0 |
| 管理 | 黑名单管理 | `/admin/trade/blacklist` | 通过 | 通过 | 2 / 1 |
| 管理 | 每日交易统计 | `/admin/trade/stat/daily` | 通过 | 通过 | 2 / 1 |
| 管理 | 商户交易统计 | `/admin/trade/stat/user` | 通过 | 通过 | 1 / 0 |
| 管理 | 通道数据统计 | `/admin/trade/stat/channel` | 通过 | 通过 | 2 / 1 |
| 管理 | 地区数据统计 | `/admin/trade/stat/province` | 通过 | 通过 | 2 / 0 |
| 管理 | 支付用户统计 | `/admin/trade/stat/buyer` | 通过 | 通过 | 2 / 1 |
| 管理 | 分账规则 | `/admin/profitsharing/receiver` | 通过 | 通过 | 2 / 0 |
| 管理 | 分账记录 | `/admin/profitsharing/order` | 通过 | 通过 | 3 / 0 |
| 管理 | 站内分账规则 | `/admin/profitsharing/internal` | 通过 | 通过 | 2 / 2 |
| 管理 | 站内分账审批 | `/admin/profitsharing/internal-orders` | 通过 | 通过 | 2 / 1 |
| 管理 | 付款记录 | `/admin/transfer/list` | 通过 | 通过 | 3 / 0 |
| 管理 | 付款账号统计 | `/admin/transfer/stat` | 通过 | 通过 | 1 / 1 |
| 管理 | 通道数据统计 | `/admin/transfer/channel-stat` | 通过 | 通过 | 1 / 1 |
| 管理 | 商户代付统计 | `/admin/transfer/user-stat` | 通过 | 通过 | 1 / 0 |
| 管理 | 创建红包 | `/admin/transfer/red` | 通过 | 通过 | 2 / 0 |
| 管理 | 批量付款 | `/admin/transfer/batches` | 通过 | 通过 | 2 / 0 |
| 管理 | 结算管理 | `/admin/settle/list` | 通过 | 通过 | 3 / 1 |
| 管理 | 批量结算 | `/admin/settle/batch` | 通过 | 通过 | 2 / 0 |
| 管理 | 结算计划 | `/admin/settle/plans` | 通过 | 通过 | 1 / 1 |
| 管理 | 商户列表 | `/admin/merchant/merchant-list` | 通过 | 通过 | 3 / 1 |
| 管理 | 用户组设置 | `/admin/merchant/group-list` | 通过 | 通过 | 2 / 2 |
| 管理 | 用户组购买 | `/admin/merchant/group-buy` | 通过 | 通过 | 1 / 0 |
| 管理 | 资金明细 | `/admin/merchant/record` | 通过 | 通过 | 3 / 0 |
| 管理 | 站内转账 | `/admin/merchant/internal-transfer` | 通过 | 通过 | 2 / 1 |
| 管理 | 授权域名 | `/admin/merchant/domain` | 通过 | 通过 | 2 / 1 |
| 管理 | 邀请码管理 | `/admin/merchant/invite-code` | 通过 | 通过 | 2 / 0 |
| 管理 | 商户消息提醒 | `/admin/merchant/notice` | 通过 | 通过 | 1 / 0 |
| 管理 | 保证金审批 | `/admin/merchant/deposit-review` | 通过 | 通过 | 2 / 0 |
| 管理 | 支付通道 | `/admin/payapi/pay-channel` | 通过 | 通过 | 3 / 2 |
| 管理 | 支付方式 | `/admin/payapi/pay-type` | 通过 | 通过 | 2 / 1 |
| 管理 | 支付通道轮询 | `/admin/payapi/pay-roll` | 通过 | 通过 | 2 / 2 |
| 管理 | 公众号小程序 | `/admin/payapi/pay-weixin` | 通过 | 通过 | 2 / 2 |
| 管理 | 进件渠道配置 | `/admin/applyments/channel` | 通过 | 通过 | 2 / 2 |
| 管理 | 进件商户管理 | `/admin/applyments/merchant` | 通过 | 通过 | 2 / 1 |
| 管理 | 子通道管理 | `/admin/applyments/subchannel` | 通过 | 通过 | 2 / 1 |
| 管理 | 商户管控记录 | `/admin/applyments/mchrisk` | 通过 | 通过 | 2 / 2 |
| 管理 | 商户进件表单 | `/admin/applyments/form` | 辅助页已检查 | 辅助页已检查 | 1 / 0 |
| 管理 | 投诉记录 | `/admin/complain/list` | 功能关闭（403） | 功能关闭（403） | 3 / 0 |
| 管理 | 投诉统计 | `/admin/complain/charts` | 功能关闭（403） | 功能关闭（403） | 2 / 0 |
| 管理 | 同步任务 | `/admin/complain/task` | 功能关闭（403） | 功能关闭（403） | 2 / 0 |
| 管理 | 风控记录 | `/admin/other/risk` | 通过 | 通过 | 2 / 2 |
| 管理 | 获取用户标识 | `/admin/other/get-token` | 通过 | 通过 | 1 / 0 |
| 管理 | 聚合收款码样式 | `/admin/other/onecode` | 通过 | 通过 | 2 / 0 |
| 管理 | 编辑收款码样式 | `/admin/other/onecode/edit` | 辅助页已检查 | 辅助页已检查 | 1 / 0 |
| 管理 | 应用市场 | `/admin/app/app-store` | 通过 | 通过 | 2 / 0 |
| 管理 | 本地插件 | `/admin/app/plugin-list` | 通过 | 通过 | 2 / 2 |
| 管理 | 模板配置 | `/admin/app/template` | 通过 | 通过 | 1 / 0 |
| 管理 | 扩展配置 | `/admin/app/addon` | 通过 | 通过 | 2 / 1 |
| 管理 | 系统设置 | `/admin/system/settings` | 通过 | 通过 | 1 / 0 |
| 管理 | 计划任务 | `/admin/system/cron` | 通过 | 通过 | 1 / 0 |
| 管理 | 公告配置 | `/admin/system/anounce` | 通过 | 通过 | 1 / 1 |
| 管理 | 管理员列表 | `/admin/system/admin-user` | 通过 | 通过 | 2 / 1 |
| 管理 | 登录日志 | `/admin/system/log` | 通过 | 通过 | 2 / 1 |
| 管理 | 运行日志 | `/admin/system/runtime-log` | 通过 | 通过 | 2 / 1 |
| 管理 | 数据清理 | `/admin/system/clean` | 通过 | 通过 | 1 / 0 |
| 管理 | 系统更新 | `/admin/system/update` | 通过 | 通过 | 1 / 0 |
| 管理 | 订单打印配置 | `/admin/system/printing` | 通过 | 通过 | 1 / 0 |
| 管理 | 邀请返现政策 | `/admin/system/referral-policies` | 通过 | 通过 | 1 / 0 |
| 管理 | 订单名称限制策略 | `/admin/system/order-subject-policies` | 通过 | 通过 | 2 / 2 |
| 管理 | 修改密码 | `/admin/system/change-password` | 辅助页已检查 | 辅助页已检查 | 1 / 0 |
| 商户 | 仪表盘 | `/merchant/dashboard` | 通过 | 通过 | 1 / 0 |
| 商户 | API 信息 | `/merchant/account/api-info` | 通过 | 通过 | 1 / 0 |
| 商户 | 账户设置 | `/merchant/account/settings` | 通过 | 通过 | 1 / 0 |
| 商户 | 实名认证 | `/merchant/account/certificate` | 暂缓：尚未接入 | 暂缓：尚未接入 | 1 / 0 |
| 商户 | 余额充值 | `/merchant/account/recharge` | 通过 | 通过 | 2 / 0 |
| 商户 | 购买会员 | `/merchant/account/groupbuy` | 通过 | 通过 | 2 / 0 |
| 商户 | 保证金 | `/merchant/account/deposit` | 通过 | 通过 | 2 / 0 |
| 商户 | 站内转账 | `/merchant/account/internal-transfer` | 通过 | 通过 | 2 / 1 |
| 商户 | 消息提醒设置 | `/merchant/account/notice` | 通过 | 通过 | 1 / 0 |
| 商户 | 订单打印设置 | `/merchant/account/printing` | 通过 | 通过 | 1 / 0 |
| 商户 | 自动结算参与设置 | `/merchant/account/settlement-schedule` | 通过 | 通过 | 1 / 0 |
| 商户 | 订单管理 | `/merchant/trade/order` | 通过 | 通过 | 5 / 2 |
| 商户 | 退款记录 | `/merchant/trade/refund` | 通过 | 通过 | 3 / 0 |
| 商户 | 每日交易统计 | `/merchant/trade/daily-stat` | 通过 | 通过 | 2 / 1 |
| 商户 | 地区数据统计 | `/merchant/trade/province-stat` | 通过 | 通过 | 1 / 0 |
| 商户 | 结算记录 | `/merchant/settle/list` | 通过 | 通过 | 3 / 1 |
| 商户 | 结算设置 | `/merchant/settle/settings` | 通过 | 通过 | 2 / 0 |
| 商户 | 申请提现 | `/merchant/settle/apply` | 通过 | 通过 | 2 / 0 |
| 商户 | 单笔付款 | `/merchant/transfer` | 通过 | 通过 | 3 / 0 |
| 商户 | 创建红包 | `/merchant/transfer/red` | 通过 | 通过 | 2 / 0 |
| 商户 | 批量付款 | `/merchant/transfer/batches` | 通过 | 通过 | 2 / 0 |
| 商户 | 资金明细 | `/merchant/record` | 通过 | 通过 | 3 / 0 |
| 商户 | 授权域名 | `/merchant/domain` | 通过 | 通过 | 2 / 1 |
| 商户 | 交易投诉 | `/merchant/complain` | 当前菜单未开放 | 当前菜单未开放 | 1 / 0 |
| 商户 | 进件商户管理 | `/merchant/applyments/merchant` | 当前菜单未开放 | 当前菜单未开放 | 2 / 1 |
| 商户 | 新增商户进件 | `/merchant/applyments/create` | 当前菜单未开放 | 当前菜单未开放 | 1 / 0 |
| 商户 | 商户管控记录 | `/merchant/applyments/mchrisk` | 当前菜单未开放 | 当前菜单未开放 | 1 / 0 |
| 商户 | 商户进件表单 | `/merchant/applyments/form` | 辅助页已检查 | 辅助页已检查 | 1 / 0 |
| 商户 | 分账规则 | `/merchant/profitsharing/receiver` | 通过 | 通过 | 2 / 0 |
| 商户 | 分账记录 | `/merchant/profitsharing/order` | 通过 | 通过 | 3 / 0 |
| 商户 | 支付通道 | `/merchant/subchannel` | 通过 | 通过 | 2 / 1 |
| 商户 | 聚合收款 | `/merchant/onecode` | 通过 | 通过 | 1 / 0 |
| 商户 | 邀请返现 | `/merchant/invite` | 通过 | 通过 | 1 / 0 |

## 失败复测与脚本维护

首轮真实后端套件为 1 通过、8 失败，原始结果保留。随后发现并修正旧脚本对头像菜单退出、已移除标题、计划任务卡片、结算字段、状态标签及 dashboard 资料提醒的过时断言；修复导航竞态，并按模板实际 CSP 检查同源嵌入与禁止脚本。批量付款的旧 data-state 断言改为核对可见状态行，继续验证“只执行一项、取消剩余项”的业务结果。

原始巡检的地区统计 SVG 文字读取和订单菜单名称匹配属于巡检脚本错误，已分别复测通过；原失败文件没有改写。连续测试触发的 429 也保留为失败记录，未通过修改运行时登录规则来规避。补充订单测试改为核对服务端保存的状态和最终控件，不依赖刷新时会消失的瞬时提示。

最终汇总重新检查了全部 HTTP 4xx/5xx。管理端 3 个投诉节点有明确关闭提示及 HTTP 403，列为“功能关闭”，没有计为功能通过；任何其他非预期 HTTP 错误都会阻止报告生成。最终用例结果来自两份完整成功回执，不把先前失败或未执行的收集错误计为通过。

## 复测方式与本机证据

测试副本、冻结 API209、随机测试配置及截图均放在 .tools 下；凭据文件不进入测试报告或 Git。以下路径相对 E:\GOEPAY：

- .tools/browser210/final-summary.json：逐节点、交互及最终用例结果。
- .tools/browser210/live-audit.json、live-deep.json、live-targeted-recheck.json、live-order-menus.json、live-mobile.json：线上原始巡检及复测。
- .tools/browser210/screenshots/：脱敏浏览器截图；API 密钥页不截图。
- .tools/browser210/local-e2e.json：524 项模拟接口测试通过的完整回执。
- .tools/browser210/real-backend-core-final.json：核心 9 项完整执行结果。
- .tools/browser210/real-backend-nodes-final.json：补充 4 项完整执行结果。
- .tools/browser210/real-backend-original.json、real-backend-recheck*.json：全部失败、收集错误及中间复测记录。
- .tools/browser210/production-post-audit.log：线上订单和余额只读复核。
- .tools/browser210/local-ledger-reconcile.log：本地账务核对。
- .tools/browser210/sandbox-cleanup.json：本轮专用数据库清理回执。

核心套件启用 GOEPAY_LIVE_E2E、GOEPAY_LIVE_FUNDS_E2E、GOEPAY_LIVE_TEMPLATE_PRINTING_E2E，执行 playwright test live --grep-invert 'isolated real backend node workflows' --workers=1。补充套件独立准备本地夹具后，再同时启用 GOEPAY_LIVE_NODE_WORKFLOWS_E2E，执行 playwright test live-node-workflows.spec.ts --workers=1。必须显式设置本地 GOEPAY_WEB_URL 和 GOEPAY_LIVE_ENV_FILE，不要以生产站点代替。
