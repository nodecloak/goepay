// Install only with the daily70 core/HTTP increment. This module does not enable
// scheduling; the separate scheduler remains disabled by default.
export function installReferralDailyAPI({ paths, schemas, object, str, ref, content }) {
  const minor = str('原订单整数分字符串；禁止转浮点数。', { pattern: '^(0|[1-9][0-9]*)$' });
  const id = str('原不可变十进制ID字符串。', { pattern: '^[1-9][0-9]*$' });
  const counter = str('非负十进制整数字符串。', { pattern: '^(0|[1-9][0-9]*)$' });
  const envelope = name => object({ code: { type: 'integer', const: 0 }, message: str(), data: ref(name) }, ['code', 'message', 'data']);
  schemas.ReferralDailyQueueItem = object({
    order_id: id, merchant_id: id, promoter_id: id,
    trade_no: str('原付款订单号；merchant_id为原付款商户，不是当前推广人。'),
    amount: minor, platform_fee: minor, reward_amount: minor, pending_amount: minor,
    state: str('pending等待专用调度；awarded已发；blocked退款结果待核实；voided首次成功退款已撤销整笔待发奖励。', { enum: ['pending', 'awarded', 'blocked', 'voided'] }),
    activated_at: str('原激活时间；界面仅转换北京时间，不改API原值。', { format: 'date-time' }),
    business_day: str('原激活北京时间业务日。', { format: 'date' }),
    eligible_day: str('原业务日次日，按北京时间；不承诺调度器已运行或该日已到账。', { format: 'date' }),
    source_version: { type: 'integer', const: 2 },
    source_verified: { type: 'boolean', const: true, description: '服务端已核对原条款、账务来源及结果HMAC；不返回proof或原收款资料。' },
  }, ['order_id', 'merchant_id', 'promoter_id', 'trade_no', 'amount', 'platform_fee', 'reward_amount', 'pending_amount', 'state', 'activated_at', 'business_day', 'eligible_day', 'source_version', 'source_verified']);
  schemas.ReferralDailyQueuePage = object({ items: { type: 'array', maxItems: 100, items: ref('ReferralDailyQueueItem') }, next_cursor: str('原order_id升序游标；空串表示本次无后继游标。') }, ['items', 'next_cursor']);
  paths['/console/v1/merchant/referral/daily-queue'] = { get: {
    operationId: 'merchantReferralDailyQueue', summary: '本人读取原单每日奖励队列',
    description: '仅真实merchant本人且非delegated。promoter取原durable session，不接受merchant_id/promoter_id/owner选择器。只读本地，不发奖、不查上游；严格核验来源后一次返回整页。每单已固定fee|gross基数、比例、奖励及原手续费上限，不用迟到SUM或当前用户组重算。unknown退款暂存；failed恢复原待发额；first succeeded整笔撤销。pending_amount是当前原单待发账户金额，blocked可为0，不能解释为已发。调度器独立默认关闭。',
    security: [{ MerchantSession: [] }],
    parameters: [
      { name: 'after_id', in: 'query', schema: { type: 'string', pattern: '^(0|[1-9][0-9]*)$', default: '0' } },
      { name: 'limit', in: 'query', schema: { type: 'integer', minimum: 1, maximum: 100, default: 20 } },
    ], responses: {
      '200': { description: '原来源已验证；所有金额是分字符串，无原收款字段。', content: content(envelope('ReferralDailyQueuePage')) },
      '400': { description: '未知或非规范游标/limit/query拒绝。' }, '401': { description: '原登录会话已失效。' }, '403': { description: '非本人merchant或委托会话拒绝。' }, '409': { description: '原条款、账本或来源证据不一致；不返回部分可信页面。' },
    },
  } };
  const overview = paths['/console/v1/{role}/referral/overview']?.get;
  const policy = paths['/console/v1/{role}/referral-policies/{scope}/{scope_id}'];
  if (!overview || !policy?.get || !policy?.post || !schemas.ReferralPolicy?.properties?.mode) throw Error('Original referral API module missing');
  overview.description += '\n\ndaily_queue_available=true表示已部署本人队列只读能力；旧服务缺失/false时前端不请求队列。不是调度器运行状态。';
  schemas.ReferralPolicy.properties.mode = str('必须显式选择并绑定原CAS与本人资金授权。缺省immediate；daily为原单固定奖励，次日北京时间起等待专用调度器。', { enum: ['immediate', 'daily'] });
  schemas.ReferralDailyOverview = object({ invited_count: counter, issued_amount: minor, voided_amount: minor, recovery_pending_amount: minor, today_issued: minor, yesterday_issued: minor, daily_queue_available: { type: 'boolean', const: true } }, ['invited_count', 'issued_amount', 'voided_amount', 'recovery_pending_amount', 'today_issued', 'yesterday_issued', 'daily_queue_available']);
  schemas.ReferralDailyPolicyView = object({ id: counter, scope: str(null, { enum: ['global', 'group'] }), scope_id: counter, revision: counter, ...Object.fromEntries(['enabled', 'rate_bps', 'mode', 'basis', 'refund_policy'].map(key => [key, schemas.ReferralPolicy.properties[key]])), created_at: str(null, { format: 'date-time' }), daily_mode_available: { type: 'boolean', const: true } }, ['id', 'scope', 'scope_id', 'revision', 'enabled', 'rate_bps', 'mode', 'basis', 'refund_policy', 'created_at', 'daily_mode_available']);
  overview.responses['200'].content = content(envelope('ReferralDailyOverview'));
  for (const operation of [policy.get, policy.post]) operation.responses['200'].content = content(envelope('ReferralDailyPolicyView'));
  policy.get.description += '\n\ndaily_mode_available=true时才允许选择daily；字段缺失/false视为未开放。原view若mode=daily但能力不可用，前端阻止编辑并提示刷新，不可静默改成immediate。';
  policy.post.description = '仅admin/super本人。expected/enabled/rate_bps/mode/basis/refund_policy全部必填，拒绝未知或重复字段。原CAS与本人资金grant绑定全部六字段，包括daily模式。启用与比例null按全局→付款商户组→推广商户组继承；存在政策的基数/模式明确覆盖。仅影响新订单原快照。fee|gross均受原手续费封顶，daily次日北京时间起待独立调度发放，默认scheduler关闭；不能当作已到账。任意成功退款整笔撤销，unknown先暂存，failed恢复原待发额。响应丢失先GET核对，不自动重试。';
} 
