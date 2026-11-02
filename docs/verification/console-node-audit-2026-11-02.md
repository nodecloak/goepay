# 管理端和商户端逐节点复查（2026-11-02）

本轮在已登录的 `go.ma3fu.com` 读取实际侧栏菜单，覆盖管理端 60 个、商户端 29 个叶节点，以及系统设置的 17 个分类。逐页确认加载后的字段、表头、筛选、操作入口、错误和能力限制提示；打开并取消新增商户、用户组、支付方式、域名、结算资料及结算计划，核对订单状态菜单、详情、默认组统计和键盘行为。

线上只读检查不等于逐个提交线上写操作。未支付订单的改已完成、补单、删除，以及已支付订单的改未完成、API退款、手动退款、冻结、重新通知、删除，均只展开检查。新增、登录、回调、退款、结算、分账及失败恢复由本地隔离模拟测试覆盖。没有真实付款、退款、出款、发送通知、重置密钥、保存生产设置或启动生产定时任务。

## 本轮修复

- 地图中一条没有地区名称的轮廓曾进入统计并作为第35个可选地区。保留完整地图几何，只让34个有名称地区进入明细和键盘操作；未匹配地区金额和笔数仍保留在未归属合计中。
- 默认用户组 GID 0 的删除按钮及勾选入口禁用，全选只选择可删除组；批量提交前再次排除受保护默认组。默认组编辑和统计仍可用。
- 系统设置、支付方式、通道、收款码样式、短信网关开关拥有稳定的功能名称；开关文字和状态仍按原逻辑显示，键盘切换后名称不会变成笼统的开启/关闭。
- 订单详情将计费策略、退款手续费承担方、支付场景、通道请求状态和材料类型显示为中文；未知枚举保留原值。请求待处理明确显示等待支付结果，不能当作已支付。
- 结算计划区分加载、确认无记录和读取错误；加载时禁用新建，空态说明新计划默认关闭且需独立授权。首次新建成功后不会再错误显示暂无计划；提供直接取消入口，清除草稿和核对状态，保留未知结果限制，打开时聚焦首输入，取消后恢复原按钮焦点。

## 核验边界

实名认证、OCR等外部业务插件按此前约定暂缓。页面应明确说明未接入或未启用，不计作已完成外部联调。当前商户红包未开启属于配置限制。任务运行、外部短信/邮件、实体打印及上游资金调用没有在生产触发。

初次本地测试复用脚本时，输出根目录未替换成功；该轮已中断，当时已开始的子进程正常结束并移除4个自有零连接数据库，不计作最终验收。输出隔离后重新执行，保留原失败和中断记录。最终验收只使用重新隔离执行的 `final-web-v4` 和 `final-live-v3` 回执。

## 实际菜单覆盖

以下“读取/检查”表示页面加载、字段和入口已在真实浏览器检查；资金及配置写入均转入本地测试。

| 端 | 节点 | 实际路径 | 本轮检查 |
| --- | --- | --- | --- |
| 管理端 | 仪表盘 | `/admin/dashboard` | 读取/检查 |
| 管理端 | 订单管理 | `/admin/trade/order` | 读取/检查 |
| 管理端 | 退款记录 | `/admin/trade/refund` | 读取/检查 |
| 管理端 | 黑名单管理 | `/admin/trade/blacklist` | 读取/检查 |
| 管理端 | 每日交易统计 | `/admin/trade/stat/daily` | 读取/检查 |
| 管理端 | 商户交易统计 | `/admin/trade/stat/user` | 读取/检查 |
| 管理端 | 通道数据统计 | `/admin/trade/stat/channel` | 读取/检查 |
| 管理端 | 地区数据统计 | `/admin/trade/stat/province` | 读取/检查 |
| 管理端 | 支付用户统计 | `/admin/trade/stat/buyer` | 读取/检查 |
| 管理端 | 分账规则 | `/admin/profitsharing/receiver` | 读取/检查 |
| 管理端 | 分账记录 | `/admin/profitsharing/order` | 读取/检查 |
| 管理端 | 站内分账规则 | `/admin/profitsharing/internal` | 读取/检查 |
| 管理端 | 站内分账审批 | `/admin/profitsharing/internal-orders` | 读取/检查 |
| 管理端 | 付款记录 | `/admin/transfer/list` | 读取/检查 |
| 管理端 | 付款账号统计 | `/admin/transfer/stat` | 读取/检查 |
| 管理端 | 通道数据统计 | `/admin/transfer/channel-stat` | 读取/检查 |
| 管理端 | 商户代付统计 | `/admin/transfer/user-stat` | 读取/检查 |
| 管理端 | 创建红包 | `/admin/transfer/red` | 读取/检查 |
| 管理端 | 批量付款 | `/admin/transfer/batches` | 读取/检查 |
| 管理端 | 结算管理 | `/admin/settle/list` | 读取/检查 |
| 管理端 | 批量结算 | `/admin/settle/batch` | 读取/检查 |
| 管理端 | 结算计划 | `/admin/settle/plans` | 读取/检查 |
| 管理端 | 商户列表 | `/admin/merchant/merchant-list` | 读取/检查 |
| 管理端 | 用户组设置 | `/admin/merchant/group-list` | 读取/检查 |
| 管理端 | 用户组购买 | `/admin/merchant/group-buy` | 读取/检查 |
| 管理端 | 资金明细 | `/admin/merchant/record` | 读取/检查 |
| 管理端 | 站内转账 | `/admin/merchant/internal-transfer` | 读取/检查 |
| 管理端 | 授权域名 | `/admin/merchant/domain` | 读取/检查 |
| 管理端 | 邀请码管理 | `/admin/merchant/invite-code` | 读取/检查 |
| 管理端 | 商户消息提醒 | `/admin/merchant/notice` | 读取/检查 |
| 管理端 | 保证金审批 | `/admin/merchant/deposit-review` | 读取/检查 |
| 管理端 | 支付通道 | `/admin/payapi/pay-channel` | 读取/检查 |
| 管理端 | 支付方式 | `/admin/payapi/pay-type` | 读取/检查 |
| 管理端 | 支付通道轮询 | `/admin/payapi/pay-roll` | 读取/检查 |
| 管理端 | 公众号小程序 | `/admin/payapi/pay-weixin` | 读取/检查 |
| 管理端 | 进件渠道配置 | `/admin/applyments/channel` | 读取/检查 |
| 管理端 | 进件商户管理 | `/admin/applyments/merchant` | 读取/检查 |
| 管理端 | 子通道管理 | `/admin/applyments/subchannel` | 读取/检查 |
| 管理端 | 商户管控记录 | `/admin/applyments/mchrisk` | 读取/检查 |
| 管理端 | 投诉记录 | `/admin/complain/list` | 读取/检查 |
| 管理端 | 投诉统计 | `/admin/complain/charts` | 读取/检查 |
| 管理端 | 同步任务 | `/admin/complain/task` | 读取/检查 |
| 管理端 | 风控记录 | `/admin/other/risk` | 读取/检查 |
| 管理端 | 获取用户标识 | `/admin/other/get-token` | 读取/检查 |
| 管理端 | 聚合收款码样式 | `/admin/other/onecode` | 读取/检查 |
| 管理端 | 应用市场 | `/admin/app/app-store` | 读取/检查 |
| 管理端 | 本地插件 | `/admin/app/plugin-list` | 读取/检查 |
| 管理端 | 模板配置 | `/admin/app/template` | 读取/检查 |
| 管理端 | 扩展配置 | `/admin/app/addon` | 读取/检查 |
| 管理端 | 系统设置 | `/admin/system/settings` | 读取/检查 |
| 管理端 | 计划任务 | `/admin/system/cron` | 读取/检查 |
| 管理端 | 公告配置 | `/admin/system/anounce` | 读取/检查 |
| 管理端 | 管理员列表 | `/admin/system/admin-user` | 读取/检查 |
| 管理端 | 登录日志 | `/admin/system/log` | 读取/检查 |
| 管理端 | 运行日志 | `/admin/system/runtime-log` | 读取/检查 |
| 管理端 | 数据清理 | `/admin/system/clean` | 读取/检查 |
| 管理端 | 系统更新 | `/admin/system/update` | 读取/检查 |
| 管理端 | 订单打印配置 | `/admin/system/printing` | 读取/检查 |
| 管理端 | 邀请返现政策 | `/admin/system/referral-policies` | 读取/检查 |
| 管理端 | 订单名称限制策略 | `/admin/system/order-subject-policies` | 读取/检查 |
| 商户端 | 仪表盘 | `/merchant/dashboard` | 读取/检查 |
| 商户端 | API 信息 | `/merchant/account/api-info` | 读取/检查 |
| 商户端 | 账户设置 | `/merchant/account/settings` | 读取/检查 |
| 商户端 | 实名认证 | `/merchant/account/certificate` | 读取/检查 |
| 商户端 | 余额充值 | `/merchant/account/recharge` | 读取/检查 |
| 商户端 | 购买会员 | `/merchant/account/groupbuy` | 读取/检查 |
| 商户端 | 保证金 | `/merchant/account/deposit` | 读取/检查 |
| 商户端 | 站内转账 | `/merchant/account/internal-transfer` | 读取/检查 |
| 商户端 | 消息提醒设置 | `/merchant/account/notice` | 读取/检查 |
| 商户端 | 订单打印设置 | `/merchant/account/printing` | 读取/检查 |
| 商户端 | 自动结算参与设置 | `/merchant/account/settlement-schedule` | 读取/检查 |
| 商户端 | 订单管理 | `/merchant/trade/order` | 读取/检查 |
| 商户端 | 退款记录 | `/merchant/trade/refund` | 读取/检查 |
| 商户端 | 每日交易统计 | `/merchant/trade/daily-stat` | 读取/检查 |
| 商户端 | 地区数据统计 | `/merchant/trade/province-stat` | 读取/检查 |
| 商户端 | 结算记录 | `/merchant/settle/list` | 读取/检查 |
| 商户端 | 结算设置 | `/merchant/settle/settings` | 读取/检查 |
| 商户端 | 申请提现 | `/merchant/settle/apply` | 读取/检查 |
| 商户端 | 单笔付款 | `/merchant/transfer` | 读取/检查 |
| 商户端 | 创建红包 | `/merchant/transfer/red` | 读取/检查 |
| 商户端 | 批量付款 | `/merchant/transfer/batches` | 读取/检查 |
| 商户端 | 资金明细 | `/merchant/record` | 读取/检查 |
| 商户端 | 授权域名 | `/merchant/domain` | 读取/检查 |
| 商户端 | 交易投诉 | `/merchant/complain` | 读取/检查 |
| 商户端 | 分账规则 | `/merchant/profitsharing/receiver` | 读取/检查 |
| 商户端 | 分账记录 | `/merchant/profitsharing/order` | 读取/检查 |
| 商户端 | 支付通道 | `/merchant/subchannel` | 读取/检查 |
| 商户端 | 聚合收款 | `/merchant/onecode` | 读取/检查 |
| 商户端 | 邀请返现 | `/merchant/invite` | 读取/检查 |

系统设置分类：网站信息、商户相关、支付相关、交易投诉、风控检测、结算规则、转账付款、快捷登录、消息提醒、实名认证、OCR配置、公告排版、邮箱配置、短信配置、IP地址、中转代理、高级设置。

## 最终验证

- 单元测试：319个文件，1817项通过。
- 浏览器：662项模拟接口测试通过；16项本地实际Go服务/隔离PostgreSQL测试通过，总计678项。模拟批次中的16项条件跳过与实际本地通过名称逐一对应，最终没有未覆盖的条件跳过。
- 实际本地测试覆盖10个文件，每个文件使用独立数据库；每轮33个账务核对指标全为零，数据库均在确认零连接后移除。
- 类型检查和生产构建通过。113个静态资源的摘要与浏览器测试构建一致；最终966个前端输入的快照摘要为 `5a1a9d9879bcdc4d958edbf4cf772b59bd3b62c0a5621767da8c1cfe75a4a95b`。
- Go、内嵌数据与迁移共2453个输入均未改变，复用同快照已通过的3040个Go测试/示例及vet/Linux构建证据；本轮另用新编译的Windows二进制执行上述实际本地浏览器测试。
- 14个Linux静态发布故障用例通过，全部替换真实网络与子进程调用；同时在目标机运行隔离自测，未触发生产业务。
- 独立静态复核提出的既有测试定位冲突、首次新建后的空态问题均修复并回归。
- 本轮只发布前端静态资源226；后端225、151条迁移、运行配置及服务进程均不需变更。生产发布后将再次核对公开资源摘要和浏览器节点。

本机详细回执位于 `.tools/ux226-20261102/acceptance.json`、`final-web-v4`、`final-live-v3`，发布回执位于 `.tools/deploy226`；这些本机证据目录不上传仓库，也不保存到产品页面。
