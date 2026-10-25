// Local read models; no provider I/O and no new authority to change money.
export function installProConsoleProjectionsAPI({ paths, schemas, object, str }) {
  const minor = str('整数分字符串，前端用十进制整数精确转换为元。', { pattern: '^-?(0|[1-9][0-9]*)$' });
  const identifier = str('十进制ID字符串，不得转换成浮点数。', { pattern: '^[1-9][0-9]*$' });
  schemas.ConsoleOrderReporting = object({
    channel_id: str('原provider attempt通道ID；未分配为空字符串。'),
    api_trade_no: str('原已验证收据交易号优先，原已存attempt交易号回退；缺失为空。'),
    buyer: str('原order_reporting支付账号优先，原预授权buyer_id回退；同筛选口径。'),
    client_ip: str('已存订单来源IP；未记录为空，不做地理位置推断。'),
    notification_state: str('最新本地payment.notify任务状态；不是付款状态。', { enum: ['none','pending','running','succeeded','dead'] }),
    refunded_amount: minor,
  }, ['channel_id','api_trade_no','buyer','client_ip','notification_state','refunded_amount']);
  schemas.ConsoleLedgerBalanceProjection = object({
    account_id: identifier,
    balance_before: minor,
    balance_after: minor,
  }, ['account_id','balance_before','balance_after']);
  schemas.ConsoleMerchantBalanceProjection = object({
    available_minor: minor,
    frozen_minor: minor,
    income_held_minor: minor,
    created_at: str('原商户注册时间，禁止创建或修改时输入。', { format:'date-time' }),
    login_username: str('原merchant登录账户名；未创建为空。'),
    effective_group_id: str('有效会员组优先，其次已确认基础组；默认组0，未知为空。'),
    effective_group_name: str('当前有效组名称；未知为空，不从旧profile.group_id猜测。'),
    group_membership_active: {type:'boolean'},
    membership_expires_at: {anyOf:[{const:''},{type:'string',format:'date-time'}],description:'原会员记录到期时间；即使到期仍保留，未购买为空。'},
    approved_domains: {type:'array',items:{type:'string'},description:'仅该商户已审核且启用的域名，排序去重；不拼接外链。'},
    settlement_recipient: object({
      state: str('未配置missing；损坏/无法核验unavailable，不回退旧明文。',{enum:['configured','missing','unavailable']}),
      config_id: str('当前加密收款配置ID字符串；未配置或不可用为0。'),
      type: str(),app_id:str(),account_masked:str('最多末4位，其余隐藏。'),name_masked:str('仅脱敏名。'),
    },['state','config_id','type','app_id','account_masked','name_masked']),
  }, ['available_minor','frozen_minor','income_held_minor','created_at','login_username','effective_group_id','effective_group_name','group_membership_active','membership_expires_at','approved_domains','settlement_recipient']);
  const append = (route, method, text) => {
    const operation = paths[route]?.[method];
    if (!operation) throw new Error(`Missing projection contract route: ${route}`);
    operation.description = [operation.description,text].filter(Boolean).join('\n\n');
  };
  append('/console/v1/{role}/orders','get','列表行同时返回ConsoleOrderReporting的6个只读字段。金额amount/merchant_amount/platform_fee/refunded_amount均为分字符串；不查询上游。buyer筛选与展示回退来源相同。');
  append('/console/v1/{role}/orders/{trade}','get','data.reporting为ConsoleOrderReporting；原order对象金额和ID契约不变。缺少账号/接口单号不编造。');
  append('/console/v1/{role}/ledger','get','行新增ConsoleLedgerBalanceProjection。前后金额按同一account_id完整不可变流水的entry.id顺序累计，在搜索、日期与分页之前计算；不是合并商户总钱包，也不是当前余额。JSON与完整CSV使用同一查询快照口径。');
  append('/console/v1/{role}/resources/{resource}','get','resource=merchants时仅管理员可读；每个item.fields包含ConsoleMerchantBalanceProjection。frozen_minor仅reserved/settlement_reserve/channel_sharing_reserve，与原Balance.Reserved一致；income_held_minor单独表示order_income_reserve，不得重复相加。所有派生字段均read_only，不可创建/修改。');
}
