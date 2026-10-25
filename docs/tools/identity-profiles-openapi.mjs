export function installIdentityProfileAPI({ paths, schemas, object, str, ref, content }) {
 const positive = str('规范正十进制字符串；最大int64。', { pattern: '^[1-9][0-9]*$' });
 const opaque = str('一次性随机值，仅保留在当前页面内存；不得写日志、storage或分析事件。', { pattern: '^[0-9a-f]{64}$' });
 const wallet = str(null, { enum: ['wxpay', 'alipay'] });
 const kind = str(null, { enum: ['web', 'mini'] });
 const time = str(null, { format: 'date-time' });
 const code = str('由原应用SDK显式取得的临时授权码；禁止传入OpenID、access_token或session_key。', { minLength: 1, maxLength: 4096, pattern: '^[A-Za-z0-9_.~-]+$' });
 const binding = { merchant_id: positive, channel_id: positive, profile_id: positive, profile_revision: positive, provider: wallet, app_id: str(), kind, path: str() };
 schemas.IdentityProfile = object({ id: positive, revision: positive, channel_id: positive, provider: wallet, kind, app_id: str(), path: str(), url_link_available: {type:'boolean',default:false,description:'仅明确启用的直连微信小程序档案；配置可尝试不代表厂商权限或链接已生成。'} }, ['id', 'revision', 'channel_id', 'provider', 'kind', 'app_id', 'path']);
 schemas.ProfileIdentityLookup = object({ ...binding, request_id: opaque, status: str(null, { enum: ['pending', 'processing', 'verified', 'failed', 'revealed'] }), expires_at: time }, [...Object.keys(binding), 'request_id', 'status', 'expires_at']);
 schemas.IdentityProfileCapabilities = object({ merchant_id: positive, available: { type: 'boolean' }, reason: str(), profiles: { type: 'array', maxItems: 256, items: ref('IdentityProfile') }, url_link_available: { type: 'boolean',description:'所列档案中至少一个可尝试URL Link；此GET零厂商I/O。' } }, ['merchant_id', 'available', 'reason', 'profiles', 'url_link_available']);
 schemas.IdentityProfileCreate = object({ profile_id: positive, profile_revision: positive, merchant_id: positive }, ['profile_id', 'profile_revision']);
 schemas.IdentityProfileSDKLaunch = object({ kind: str(null, { const: 'sdk' }), provider: wallet, app_id: str(), path: str(), method: str(null, { const: 'POST' }), completion_url: str('固定HTTPS源 /identity/profile/mini/{provider}/{profile_id}/{request_id}/；不含主体或凭据。', { format: 'uri' }), body: object({ state: opaque }, ['state']), expires_at: time }, ['kind', 'provider', 'app_id', 'path', 'method', 'completion_url', 'body', 'expires_at']);
 schemas.IdentityProfileCreated = object({ request: ref('ProfileIdentityLookup'), url_link_available: { type: 'boolean',description:'仅当前原档案的URL Link尝试能力；创建本身不调用厂商。' }, authorization_url: str('仅web创建时返回；原控制台会话绑定、首次浏览器一次使用。', { format: 'uri' }), launch: ref('IdentityProfileSDKLaunch') }, ['request', 'url_link_available']);
 schemas.ProfileIdentityLinkCreate = object({state:opaque},['state']);
 schemas.ProfileIdentityLink = object({purpose:str(null,{const:'wechat_mini_identity_url_link'}),request_id:opaque,provider:str(null,{const:'wxpay'}),app_id:str(),status:str(null,{enum:['creating','ready','failed','unknown']}),created_at:time,expires_at:time,requested_expires_at:str('仅请求厂商使用的原期限向下取整秒；不是厂商实际期限证明。',{format:'date-time'}),url_link:str('仅ready且原会话/原档案/原请求pending有效时返回。只接受HTTPS wxaurl.cn或wxmpurl.cn；禁止端口/userinfo/fragment，唯一可选cq参数。',{format:'uri'})},['purpose','request_id','provider','app_id','status','created_at','expires_at','requested_expires_at']);
 schemas.IdentityProfileComplete = object({ state: opaque, code }, ['state', 'code']);
 schemas.IdentityProfileCompleted = object({ request_id: opaque, status: str(null, { const: 'verified' }) }, ['request_id', 'status']);
 schemas.IdentityProfileResult = object({ subject_id: str('仅一次返回。标识只属于所列provider/AppID，不保证跨应用相同。'), label: str(null, { enum: ['微信OpenID', '支付宝用户标识'] }), provider: wallet, app_id: str() }, ['subject_id', 'label', 'provider', 'app_id']);
 const response = (schema, description = '成功时code=0。所有响应no-store/no-referrer。') => ({ description, content: content(object({ code: { type: 'integer' }, message: str(), data: schema }, ['code', 'message', 'data'])) });
 const errors = { 400: response({}, '拒绝未知/重复/null字段、非法格式或query。'), 401: response({}, '需要原控制台会话。'), 403: response({}, '跨主体/代登录/CSRF/失效会话拒绝。'), 404: response({}, '原请求不可读取。'), 409: response({}, '原版本、原会话、原state、code或一次领取已失效；禁止自动重试exchange。'), 429: response({}, '创建速率或同会话活动请求数受限。'), 503: response({}, '独立开关未启用，或当前可信应用/渠道不可用。') };
 const pathParam = (name, schema) => ({ name, in: 'path', required: true, schema });
 for (const area of ['admin', 'merchant']) {
  const base = `/console/v1/${area}/identity-profile-lookups`;
  const security = [{ [area === 'admin' ? 'AdminSession' : 'MerchantSession']: [] }];
  const add = (route, method, summary, body, result, parameters = [], status = 200) => {
   const op = { operationId: `${area}IdentityProfile${method}${route.replace(/[^A-Za-z]/g, '') || 'Create'}`, summary, description: '仅原会话；拒绝代登录。独立查询不创建订单、不发起支付、不改账。默认关闭，启用不依赖支付AllowLive。除capabilities外禁止query。每步重检当前应用版本/凭据绑定、商户权限与原会话。', security, parameters: [...parameters], responses: { [status]: response(result), ...errors } };
   if (method === 'post') op.parameters.push({ name: 'X-CSRF-Token', in: 'header', required: true, schema: str() });
   if (body) op.requestBody = { required: true, content: content(body) };
   (paths[base + route] ??= {})[method] = op;
  };
  add('/capabilities', 'get', '读取当前商户可用的独立OAuth档案', null, ref('IdentityProfileCapabilities'), [{ name: 'merchant_id', in: 'query', required: area === 'admin', schema: positive, description: area === 'admin' ? '原管理员显式选择的商户。' : '省略为本人；传值也必须为本人。' }]);
  add('', 'post', '创建独立网页或小程序标识查询', ref('IdentityProfileCreate'), ref('IdentityProfileCreated'), [], 201);
  add('/{request_id}', 'get', '原会话读取查询状态（无主体、state、authorization或凭据）', null, ref('ProfileIdentityLookup'), [pathParam('request_id', opaque)]);
  add('/{request_id}/complete', 'post', '原会话显式提交小程序SDK临时code', ref('IdentityProfileComplete'), ref('IdentityProfileCompleted'), [pathParam('request_id', opaque)]);
  add('/{request_id}/reveal', 'post', '原会话一次领取已验证标识', object({}, []), ref('IdentityProfileResult'), [pathParam('request_id', opaque)]);
  add('/{request_id}/url-link','post','显式为原微信小程序标识请求生成一次URL Link',ref('ProfileIdentityLinkCreate'),ref('ProfileIdentityLink'),[pathParam('request_id',opaque)]);
  paths[base+'/{request_id}/url-link'].post.description+=' 必须提供原SDK state，原期限至少余70秒。先提交独立意图与发送claim，再固定官方stable_token及generate_urllink两次POST。creating/ready/failed/unknown均不允许重新生成；网络未知只允许本地读，不重试。失败不会消费原SDK code/state，不产生支付。';
  add('/{request_id}/url-link','get','原会话只读本地URL Link意图',null,ref('ProfileIdentityLink'),[pathParam('request_id',opaque)]);
  paths[base+'/{request_id}/url-link'].get.description+=' 仅本地数据库读取，不查询或重新生成厂商链接；原身份请求完成、过期或撤销后不再返回链接。';
 }
 const publicBase = '/identity/profile';
 paths[`${publicBase}/authorize/{authorization}`] = { get: { operationId: 'profileIdentityAuthorize', summary: '独立网页授权的一次浏览器入口', description: '固定HTTPS官方授权地址；原durable会话仍须有效。首次绑定独立Secure/HttpOnly/Lax浏览器cookie；禁止query。原authorization不是查询主体或支付票据。', security: [], parameters: [pathParam('authorization', opaque)], responses: { 302: { description: '固定官方OAuth重定向，无主体返回。' }, 400: { description: '非法路径/query。' }, 409: { description: '原请求或浏览器绑定已失效。' }, 503: { description: '原可信应用不可用。' } } } };
 paths[`${publicBase}/callback/{provider}/{profile_id}/{request_id}/`] = { get: { operationId: 'profileIdentityWebCallback', summary: '独立网页授权回调', description: '固定provider/profile/request命名空间；原浏览器cookie和state必须匹配。先原子消耗state/同应用真实code realm再交换；结果加密入库，页面不返回主体。微信code、支付宝auth_code；app_id和scope如传入须匹配原应用/最小scope。拒绝重复/未知query。', security: [], parameters: [pathParam('provider', wallet), pathParam('profile_id', positive), pathParam('request_id', opaque), ...['state', 'code', 'auth_code', 'app_id', 'scope'].map(name => ({ name, in: 'query', required: name === 'state', schema: name === 'state' ? opaque : str() }))], responses: { 200: { description: '固定完成文本，请回原控制台领取；无主体。', content: { 'text/plain': { schema: str() } } }, 400: { description: '非法回调或交换失败，state不能重用。' }, 409: { description: '原绑定/原会话/state/code不可用。' } } } };
 paths[`${publicBase}/mini/{provider}/{profile_id}/{request_id}/`] = { post: { operationId: 'profileIdentityMiniComplete', summary: '独立小程序SDK提交临时code', description: '仅创建时launch.body返回的原state可用；禁止query、客户端openid、支付ticket和业务参数。请求不具有资金权限。先消耗原state和真实code realm；响应未确定时只查询原request，不重放exchange。当前仅SDK对接，不声称已生成URL Link/Scheme。', security: [], parameters: [pathParam('provider', wallet), pathParam('profile_id', positive), pathParam('request_id', opaque)], requestBody: { required: true, content: content(ref('IdentityProfileComplete')) }, responses: { 200: response(ref('IdentityProfileCompleted')), ...errors } } };
}
