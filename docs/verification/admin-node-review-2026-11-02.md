# 管理后台逐页复核（2026-11-02）

本轮在已登录的 `go.ma3fu.com` 上逐一打开实际侧栏的 60 个管理节点，并打开系统设置的 17 个分类。对照参考站与此前字段核对记录，检查加载、筛选、字段、操作入口和打开/取消交互。线上不提交支付、退款、补单、结算、转账、分账或其他资金请求；写操作由本地合成数据测试覆盖。

## 修复

1. 管理员结算列表的状态筛选移回默认可见区域，与商户端保持一致；展开和收起只影响日期，不会生成重复状态框。重置同时清空已应用条件。
2. 授权域名、管理员、风控、扩展、进件渠道等共用资源抽屉，打开后聚焦首个输入项，Tab/Shift+Tab 留在抽屉内，取消后返回原按钮；原按钮被刷新移除时返回可用的新增/刷新按钮。临时全部禁用时保留面板焦点。
3. 申请结算和站内转账抽屉补齐初始焦点、键盘循环、安全 Escape 和关闭后返回。处理资金请求期间 Escape 不关闭；原请求未知结果的保留、查询和重试规则保持原逻辑。内嵌提现申请不抢占或限制页面焦点。
4. 分账审批成功刷新列表后，原行按钮消失时回退到刷新按钮。

上述改动没有修改 Go、数据库迁移、权限、支付签名、资金处理或通道配置。

## 线上节点覆盖

以下每项均已实际访问并等待加载完成，未见页面加载错误。此处“已查看”不表示在生产执行过所有写操作，也不表示存在数据的每个业务分支都已在线触发。

| # | 节点 | 路径 | 结果 |
|---|---|---|---|
| 1 | 仪表盘 | `/admin/dashboard` | 已查看 |
| 2 | 订单管理 | `/admin/trade/order` | 已查看 |
| 3 | 退款记录 | `/admin/trade/refund` | 已查看 |
| 4 | 黑名单管理 | `/admin/trade/blacklist` | 已查看 |
| 5 | 每日交易统计 | `/admin/trade/stat/daily` | 已查看 |
| 6 | 商户交易统计 | `/admin/trade/stat/user` | 已查看 |
| 7 | 通道数据统计 | `/admin/trade/stat/channel` | 已查看 |
| 8 | 地区数据统计 | `/admin/trade/stat/province` | 已查看 |
| 9 | 支付用户统计 | `/admin/trade/stat/buyer` | 已查看 |
| 10 | 分账规则 | `/admin/profitsharing/receiver` | 已查看 |
| 11 | 分账记录 | `/admin/profitsharing/order` | 已查看 |
| 12 | 站内分账规则 | `/admin/profitsharing/internal` | 已查看 |
| 13 | 站内分账审批 | `/admin/profitsharing/internal-orders` | 已查看 |
| 14 | 付款记录 | `/admin/transfer/list` | 已查看 |
| 15 | 付款账号统计 | `/admin/transfer/stat` | 已查看 |
| 16 | 付款通道统计 | `/admin/transfer/channel-stat` | 已查看 |
| 17 | 商户代付统计 | `/admin/transfer/user-stat` | 已查看 |
| 18 | 创建红包 | `/admin/transfer/red` | 已查看 |
| 19 | 批量付款 | `/admin/transfer/batches` | 已查看 |
| 20 | 结算管理 | `/admin/settle/list` | 已查看 |
| 21 | 批量结算 | `/admin/settle/batch` | 已查看 |
| 22 | 结算计划 | `/admin/settle/plans` | 已查看 |
| 23 | 商户列表 | `/admin/merchant/merchant-list` | 已查看 |
| 24 | 用户组设置 | `/admin/merchant/group-list` | 已查看 |
| 25 | 用户组购买 | `/admin/merchant/group-buy` | 已查看 |
| 26 | 资金明细 | `/admin/merchant/record` | 已查看 |
| 27 | 站内转账 | `/admin/merchant/internal-transfer` | 已查看 |
| 28 | 授权域名 | `/admin/merchant/domain` | 已查看 |
| 29 | 邀请码管理 | `/admin/merchant/invite-code` | 已查看 |
| 30 | 商户消息提醒 | `/admin/merchant/notice` | 已查看 |
| 31 | 保证金审批 | `/admin/merchant/deposit-review` | 已查看 |
| 32 | 支付通道 | `/admin/payapi/pay-channel` | 已查看 |
| 33 | 支付方式 | `/admin/payapi/pay-type` | 已查看 |
| 34 | 支付通道轮询 | `/admin/payapi/pay-roll` | 已查看 |
| 35 | 公众号小程序 | `/admin/payapi/pay-weixin` | 已查看 |
| 36 | 进件渠道配置 | `/admin/applyments/channel` | 已查看 |
| 37 | 进件商户管理 | `/admin/applyments/merchant` | 已查看 |
| 38 | 子通道管理 | `/admin/applyments/subchannel` | 已查看 |
| 39 | 商户管控记录 | `/admin/applyments/mchrisk` | 已查看 |
| 40 | 投诉记录 | `/admin/complain/list` | 已查看 |
| 41 | 投诉统计 | `/admin/complain/charts` | 已查看 |
| 42 | 投诉同步任务 | `/admin/complain/task` | 已查看 |
| 43 | 风控记录 | `/admin/other/risk` | 已查看 |
| 44 | 获取用户标识 | `/admin/other/get-token` | 已查看 |
| 45 | 聚合收款码样式 | `/admin/other/onecode` | 已查看 |
| 46 | 应用市场 | `/admin/app/app-store` | 已查看 |
| 47 | 本地插件 | `/admin/app/plugin-list` | 已查看 |
| 48 | 模板配置 | `/admin/app/template` | 已查看 |
| 49 | 扩展配置 | `/admin/app/addon` | 已查看 |
| 50 | 系统设置 | `/admin/system/settings` | 已查看 |
| 51 | 计划任务 | `/admin/system/cron` | 已查看 |
| 52 | 公告配置 | `/admin/system/anounce` | 已查看 |
| 53 | 管理员列表 | `/admin/system/admin-user` | 已查看 |
| 54 | 登录日志 | `/admin/system/log` | 已查看 |
| 55 | 运行日志 | `/admin/system/runtime-log` | 已查看 |
| 56 | 数据清理 | `/admin/system/clean` | 已查看 |
| 57 | 系统更新 | `/admin/system/update` | 已查看 |
| 58 | 订单打印配置 | `/admin/system/printing` | 已查看 |
| 59 | 邀请返现政策 | `/admin/system/referral-policies` | 已查看 |
| 60 | 订单名称限制策略 | `/admin/system/order-subject-policies` | 已查看 |

系统分类：网站信息、商户相关、支付相关、交易投诉、风控检测、结算规则、转账付款、快捷登录、消息提醒、实名认证、OCR配置、公告排版、邮箱配置、短信配置、IP地址、中转代理、高级设置。按当前可见面板核对字段，排除缓存但隐藏的面板；邮箱的账号和发送频率在独立窗口中检查。

## 重点交互与边界

- 未支付订单显示“改已完成、手动补单、删除订单”；已支付订单显示“改未完成、API退款、手动退款、冻结订单、重新通知、删除订单”。核对菜单时曾误打开补单确认框，已立即取消，未输入原因或提交操作；后续每次切换订单前均确认菜单关闭。
- 新增商户、用户组的字段和分栏已再次对照参考站。密码保持空白且使用新密码自动填充提示；输入草稿后取消、重开不保留旧草稿。Go 的密码长度要求及 API 专用账户选项是明确的安全/业务扩展。
- 黑名单、授权域名、管理员、支付方式、通道轮询、公众号小程序、风控、扩展、进件渠道等表单只打开和取消。付款/结算/站内转账表单未查询生产资金或提交请求。
- 计划任务仅查看配置和执行记录，未点击运行；模板及应用节点未保存配置、安装、上传或切换模板；未执行数据清理、系统升级、密钥重置或权限变更。
- 用户此前暂缓的外部插件仍按原范围暂缓。实名认证、OCR、短信等无适配器时显示能力限制，不能视为外部服务已验收。
- 已知字段/资金模型边界继续以 `production-form-parity-2026-11-01.md` 为准，不能将本次可访问检查解释为所有原版资金规则已完全一致。

## 验证记录

- 最终前端单元测试：315 个文件、1,765 项通过。
- TypeScript 类型检查和生产构建通过。
- 生产构建浏览器回归：642 项通过；15 项需后端的用例单独在本地合成环境运行，不能将第一轮跳过计为通过。
- 本地后端集成：9 个测试文件、15 项通过；与前一轮跳过的 15 项逐文件匹配，合计浏览器验证 657 项，无未执行的条件用例。测试复用现有 PostgreSQL，每个自建数据库均在连接数为零后清理。
- Go 源码及嵌入数据共 2,452 项与上一轮完整验收快照完全一致；沿用该快照 3,037 项测试/示例通过的证据，本轮未重复全量 Go 测试。
- 测试日志保存在本机 `.tools/admin-ux223-20261102`。失败尝试保留；首轮本地集成因夹具数据库前缀不一致被拒绝，修正后以独立运行验收。没有新建容器，没有发起真实资金交易。
- 本次采用独立静态前端发布，后端保持 `2026.11.01-222`，不重启服务、不执行迁移或修改配置。发布后的线上复核与静态文件摘要保存在本机 `.tools/deploy223`。
