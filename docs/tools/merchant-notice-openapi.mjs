export function installMerchantNoticeAPI({ paths, schemas, object, str, id, ref, content }) {
  const scenes = ['order','settle','login','complain','mchrisk','balance','group','domain_audit','daily_report'];
  const targets = ['wx_uid','wx_appid','telegram_uid','msgrobot_url','qqbot_appid','qqbot_appsecret','qqbot_openid','voice_devid'];
  const money = str('人民币元十进制字符串；非负，最多两位小数。', { pattern: '^(0|[1-9][0-9]*)(?:\\.[0-9]{1,2})?$' });
  const config = object({ ...Object.fromEntries(scenes.map(s => [s, { type: 'integer', minimum: 0, maximum: 6 }])), order_money: money, balance_money: money }, [...scenes,'order_money','balance_money']);
  const zeroID = str(null, { pattern: '^(0|[1-9][0-9]*)$' });
  const booleanMap = { type: 'object', additionalProperties: { type: 'boolean' } };
  schemas.MerchantNoticeWrite = object({ expected_profile_id: zeroID, msgconfig: config, voice_order: { type: 'boolean' }, targets: object(Object.fromEntries(targets.map(t => [t, str('仅替换时提交；不得为null或空串。', { writeOnly: true, minLength: 1 })]))), clear_fields: { type: 'array', uniqueItems: true, items: str(null, { enum: targets }) } }, ['expected_profile_id','msgconfig','voice_order','targets','clear_fields']);
  schemas.MerchantNoticeSettings = object({ id: zeroID, merchant_id: id, revision: zeroID, msgconfig: config, voice_order: { type: 'boolean' }, email_masked: str(), configured: booleanMap, target_masks: { type: 'object', additionalProperties: { type: 'string' } } }, ['id','merchant_id','revision','msgconfig','voice_order','email_masked','configured','target_masks']);
  const state = str(null, { enum: ['not_scheduled','queued','blocked','accepted','unknown','not_sent'] });
  schemas.MerchantNoticeEvent = object({ id, scene: str(null, { enum: scenes }), trade_no: str(), at: str(null, { format: 'date-time' }), snapshot_state: str(null,{enum:['captured','absent','unavailable','excluded']}), state, job_state: str(), deliveries: { type: 'array', items: object({ transport: str(null,{enum:['wechat_template','smtp','sms','robot','telegram','qqbot','voice']}), state, attempted_at: str(null,{format:'date-time'}) }, ['transport','state']) } }, ['id','scene','at','snapshot_state','state','deliveries']);
  schemas.MerchantNoticeEventsPage = object({ items: { type: 'array', items: ref('MerchantNoticeEvent') }, next_cursor: str('末页为空；否则使用原值作为before_id。',{pattern:'^(|[1-9][0-9]*)$'}) }, ['items','next_cursor']);
  const envelope = name => object({ code: { type: 'integer', const: 0 }, message: str(), data: ref(name) }, ['code','message','data']);
  for (const [area, base] of [['merchant','/account/notice'],['admin','/merchants/{merchant_id}/notice']]) {
    const owner = area === 'admin' ? [{ name: 'merchant_id', in: 'path', required: true, schema: id }] : [];
    const common = { tags: ['merchant-notices'], security: [{ [area === 'admin' ? 'AdminSession' : 'MerchantSession']: [] }], parameters: owner, description: '本人持久会话；代登录拒绝。管理员需已登记的super/operator商户管理权限。接收资料不作为身份验证或资金授权。只返回掩码；不得使用query指定商户。', responses: { '200': { description: '原通知设置。', content: content(envelope('MerchantNoticeSettings')) }, '403': { description: '会话或权限不足。' }, '409': { description: '版本或来源证明冲突，重新读取。' }, '503': { description: '配置不可用。' } } };
    const route = `/console/v1/${area}${base}`;
    paths[route] = { get: { ...common, operationId: `${area}MerchantNoticeSettings`, summary: '读取商户消息提醒设置' }, put: { ...common, operationId: `${area}MerchantNoticeSave`, summary: '版本比较后保存消息提醒设置', description: `${common.description} 保存不会发送消息；空targets保留原值，清除必须显式clear_fields。保存结果未知时重新读取版本，不能自动重试。`, parameters: [...owner,{name:'X-CSRF-Token',in:'header',required:true,schema:{type:'string'}}], requestBody: { required: true, content: content(ref('MerchantNoticeWrite')) } } };
    paths[`${route}/events`] = { get: { ...common, operationId: `${area}MerchantNoticeEvents`, summary: '只读通知记录', description: `${common.description} 来源和发送凭证校验后读取；accepted仅表示上游受理，unknown包括已claim但丢失结果。读取不发送、补发或清理记录。`, parameters: [...owner,{name:'before_id',in:'query',schema:zeroID},{name:'limit',in:'query',schema:{type:'integer',minimum:1,maximum:100,default:20}}], responses: { ...common.responses, '200': { description: 'ID倒序、游标分页；不含收件目标、凭据或原消息正文。', content: content(envelope('MerchantNoticeEventsPage')) }, '400': { description: '未知、重复或非规范分页参数。' } } } };
  }
}

