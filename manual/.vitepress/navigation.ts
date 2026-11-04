export const sidebar = [
  { text: '开始使用', items: [
    { text: '快速开始', link: '/' },
    { text: '登录与页面操作', link: '/start/navigation' },
    { text: '业务架构与系统分层', link: '/concepts/architecture' },
  ] },
  { text: '功能模块', items: [
    { text: '商户与用户组', link: '/modules/merchant-management', items: [
      { text: '管理员：开通与代登录', link: '/admin/merchants' },
      { text: '用户组、默认通道与费率', link: '/admin/groups' },
    ] },
    { text: '支付方式与通道', link: '/modules/payment-channels', items: [
      { text: '通道、方式与轮询配置', link: '/admin/channels' },
      { text: '商户通道与聚合收款', link: '/merchant/collection' },
    ] },
    { text: '收款与订单', link: '/modules/transactions', items: [
      { text: '管理员订单操作', link: '/admin/orders' },
      { text: '商户订单与通知', link: '/merchant/orders' },
      { text: '支付回调时序', link: '/concepts/payment-flow' },
    ] },
    { text: '退款与售后', link: '/modules/refunds', items: [
      { text: 'API退款、手动退款与核对', link: '/admin/refunds' },
    ] },
    { text: '结算与提现', link: '/modules/settlements', items: [
      { text: '结算审批、批次与计划', link: '/admin/settlements' },
      { text: '商户收款账户与提现', link: '/merchant/settlements' },
    ] },
    { text: '代付与转账', link: '/modules/transfers', items: [
      { text: '管理员付款、批次与红包', link: '/admin/transfers' },
      { text: '商户代付与站内转账', link: '/merchant/transfers' },
    ] },
    { text: '订单分账', link: '/modules/profitsharing', items: [
      { text: '跨商户分账与审批', link: '/admin/sharing' },
      { text: '商户分账规则与记录', link: '/merchant/sharing' },
    ] },
  ] },
  { text: '账户与运营', items: [
    { text: '账户与接入安全', link: '/modules/account-security', items: [
      { text: 'API信息、密钥与域名', link: '/merchant/api' },
      { text: '商户资料与二次验证', link: '/merchant/account' },
      { text: '管理员权限与资金确认', link: '/concepts/security' },
    ] },
    { text: '消息与订单打印', link: '/modules/notifications', items: [
      { text: '平台短信、机器人与硬件', link: '/admin/notifications' },
      { text: '商户提醒与设备设置', link: '/merchant/notifications' },
    ] },
    { text: '实名认证与 OCR', link: '/modules/certification', items: [
      { text: '服务商与费用配置', link: '/admin/certification' },
      { text: '商户认证与证照识别', link: '/merchant/certification' },
    ] },
    { text: '风控、投诉与进件', link: '/modules/risk-onboarding', items: [
      { text: '管理员风控与机构协作', link: '/admin/risk-and-onboarding' },
      { text: '商户投诉与进件', link: '/merchant/reports-and-support' },
    ] },
    { text: '统计与资金服务', link: '/modules/reports', items: [
      { text: '运营报表与资金明细', link: '/admin/reports' },
      { text: '余额、费用与账务口径', link: '/concepts/accounting' },
      { text: '充值、会员、保证金与返现', link: '/merchant/services' },
    ] },
  ] },
  { text: '系统与维护', items: [
    { text: '系统管理', link: '/modules/system', items: [
      { text: '系统设置各分类', link: '/admin/settings' },
      { text: '公告、权限、任务与日志', link: '/admin/system' },
      { text: '模板、样式与应用', link: '/admin/appearance' },
    ] },
    { text: '数据迁移', link: '/modules/migration', items: [
      { text: '易支付迁移向导', link: '/admin/migration' },
    ] },
  ] },
  { text: '帮助与参考', items: [
    { text: '管理员开通检查表', link: '/admin/quickstart' },
    { text: '商户首次使用', link: '/merchant/quickstart' },
    { text: '常见问题', link: '/troubleshooting' },
    { text: '管理员菜单索引', link: '/reference/admin-menu' },
    { text: '商户菜单索引', link: '/reference/merchant-menu' },
    { text: '术语表', link: '/reference/glossary' },
  ] },
]
