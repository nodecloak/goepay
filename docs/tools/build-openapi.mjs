// Documents the implemented console contract, independently of legacy PHP URLs.
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { installSettlementPlanAPI } from './settlement-plans-openapi.mjs';
import { installPrintingAPI } from './printing-openapi.mjs';
import { installLegacyRefundHistoryAPI } from './legacy-refund-history-openapi.mjs';
import { installPageTemplateAPI } from './page-templates-openapi.mjs';
import { installIdentityProfileAPI } from './identity-profiles-openapi.mjs';
import { installApplicationAuthorizationAPI } from './application-authorization-openapi.mjs';
import { installProConsoleProjectionsAPI } from './pro-console-projections-openapi.mjs';
import { installPreauthObservationsAPI } from './preauth-observations-openapi.mjs';
import { installPreauthRemainingAPI } from './preauth-remaining-openapi.mjs';
import { installReferralDailyAPI } from './referral-daily-openapi.mjs';
import { installProOrderSearchAPI } from './pro-order-search-openapi.mjs';
import { installMerchantRSAAPI } from './merchant-rsa-openapi.mjs';
import { installReferralFundingAPI } from './referral-funding-openapi.mjs';
import { installSettlementAutoAPI } from './settlement-auto-openapi.mjs';
import { installChannelStopAPI } from './channel-stop-openapi.mjs';
import { installScanRecoveryAPI } from './scan-recovery-openapi.mjs';
import { installMerchantNoticeAPI } from './merchant-notice-openapi.mjs';
import { installMerchantPaymentReviewAPI } from './merchant-payment-review-openapi.mjs';
import { installBusinessNoticeAPI } from './business-notice-openapi.mjs';
const root = fileURLToPath(new URL('../../', import.meta.url));
const ref = name => ({ $ref: `#/components/schemas/${name}` });
const str = (description, extra = {}) => ({ type: 'string', ...(description ? { description } : {}), ...extra });
const object = (properties, required = [], additionalProperties = false) => ({ type: 'object', properties, required, additionalProperties });
const money = str('人民币元十进制字符串；不得使用二进制浮点。', { pattern: '^[0-9]+(?:\\.[0-9]{1,2})?$' });
const id = str('十进制 ID 字符串。', { pattern: '^[1-9][0-9]*$' });
const minor = str('人民币分的非负十进制整数字符串；本渠道分账API不接受元数值。',{pattern:'^(0|[1-9][0-9]*)$'});
const sharingNo = str('商户内稳定原请求号；未知结果禁止换号重发。',{pattern:'^[A-Za-z0-9_-]{1,128}$'});
const schemas = {
  Envelope: object({ code: { type: 'integer', description: '后台成功为 0，错误通常为 -1；仍需检查 HTTP 状态和业务 state。' }, message: str(), data: {} }, ['code', 'message', 'data']),
  Empty: object({}),
  IdentityLookupCreate: object({channel_id:id,merchant_id:id},['channel_id']),
  Login: object({ username: str(null, { maxLength: 128 }), password: str(null, { format: 'password', writeOnly: true }), code: str('已启用 TOTP 时必填。') }, ['username', 'password']),
  Refund: object({ amount: money, external_no: str('商户内幂等号；重试必须保持金额和原订单不变。', { minLength: 1, maxLength: 128 }) }, ['amount', 'external_no']),
  NotificationResend: object({ external_no: str('订单内幂等号；响应丢失后保留同号重试。活跃投递合并，完成后同号仍查询原任务，不再次发送。', { minLength: 1, maxLength: 128 }) }, ['external_no']),
  IncomeFreeze: object({ external_no: str('冻结请求幂等号；不接受客户端金额。', {minLength:1,maxLength:128}) }, ['external_no']),
  IncomeUnfreeze: object({ hold_id: id }, ['hold_id']),
  IncomeHoldAdoption: object({ hold_id: id, external_no: str('历史冻结款迁移原请求号；同号重放只核对原采用，不重新消费授权。', {minLength:1,maxLength:128}) }, ['hold_id','external_no']),
  SSOGrant: object({ merchant_id: id }, ['merchant_id']),
  SSOConsume: object({ grant: str('60秒一次性代登录授权，仅在请求体使用，绑定原管理员会话。', {minLength:43,maxLength:43,writeOnly:true}) }, ['grant']),
  Verification: object({ purpose: str(null,{enum:['register','recover']}), email: str(null,{format:'email',maxLength:254}) }, ['purpose','email']),
  Registration: object({challenge_id:str(),code:str(null,{writeOnly:true}),email:str(null,{format:'email'}),username:str(),name:str(),password:str(null,{format:'password',writeOnly:true,minLength:12,maxLength:1024}),invite_code:str(null,{writeOnly:true})},['challenge_id','code','email','username','name','password']),
  Recovery: object({challenge_id:str(),code:str(null,{writeOnly:true}),email:str(null,{format:'email'}),new_password:str(null,{format:'password',writeOnly:true,minLength:12,maxLength:1024})},['challenge_id','code','email','new_password']),
  Transfer: object({ merchant_id: id, channel_id: id, type: str(null, { enum: ['alipay', 'wxpay', 'bank'] }), account: str('敏感收款账号；不写动作审计。'), name: str('收款人姓名；银行转账必填。'), amount: money, remark: str(), external_no: str(null, { minLength: 1, maxLength: 128 }), bank_code: str('仅 bank；必须来自对应通道支持的银行映射。') }, ['type', 'account', 'amount', 'external_no']),
  TransferBatchItem: object({channel_id:id,type:str(null,{enum:['alipay','wxpay','bank']}),account:str(null,{writeOnly:true}),name:str(null,{writeOnly:true}),amount:money,remark:str(),bank_code:str()},['channel_id','type','account','amount']),
  TransferBatchPrepare: object({merchant_id:id,external_no:str('整批稳定幂等号；顺序和每项收款身份均绑定。',{minLength:1,maxLength:128}),items:{type:'array',minItems:1,maxItems:100,items:ref('TransferBatchItem')}},['external_no','items']),
  TransferBatchExecute: object({limit:{type:'integer',minimum:1,maximum:100}},['limit']),
  TransferBatchImport: object({merchant_id:id,format:str('仅CSV或XLSX；XLS明确不支持。',{enum:['csv','xlsx']}),file_base64:str('原文件标准带padding base64；文件1..524288字节。仅当前会话内存，不记录原文件/原始受款资料。',{writeOnly:true,maxLength:699052})},['format','file_base64']),
  ReferralTokenChange: object({expected:str('当前token version，首次0；丢响应先读取当前掩码版本，不自动重试。',{pattern:'^(0|[1-9][0-9]*)$'})},['expected']),
  ReferralPolicy:object({expected:str('当前政策revision的CAS，非记录ID；首次0。',{pattern:'^(0|[1-9][0-9]*)$'}),enabled:{type:['boolean','null'],description:'group null继承；global必须bool。'},rate_bps:{type:['integer','null'],minimum:0,maximum:10000,description:'group null继承，0明确覆盖；global必须整数。'},mode:str(null,{enum:['immediate']}),basis:str('fee=原平台手续费基数；gross=原订单金额基数，但奖励始终不超过该原订单platform_fee。基数随最后存在政策覆盖，不是null继承。仅新订单快照生效，旧proof及退款来源不变。',{enum:['fee','gross']}),refund_policy:str(null,{enum:['any_successful_refund_voids_reward']})},['expected','enabled','rate_bps','mode','basis','refund_policy']),
  ReferralAliasCursor:object({after_id:str('关系ID游标，非商户ID。',{pattern:'^(0|[1-9][0-9]*)$'}),limit:{type:'integer',minimum:1,maximum:100}},['after_id','limit']),
  FundsRequest: object({ merchant_id: id, recipient_id: id, amount: money, external_no: str(null, { minLength: 1, maxLength: 128 }),expected_fee_cents:str('仅settlements，需与expected_net_cents同时提交，整数分字符串；实际事务费用不一致返回409。',{pattern:'^(0|[1-9][0-9]*)$'}),expected_net_cents:str('仅settlements，与手续费一起核对的正整数分字符串。',{pattern:'^[1-9][0-9]*$'}) }, ['amount', 'external_no']),
  Profile: object({ name: str(null, { minLength: 1, maxLength: 128 }), email: str(null, { maxLength: 254 }) }, ['name', 'email']),
  PasswordChange: object({ current_password: str(null, { format: 'password', writeOnly: true }), new_password: str(null, { format: 'password', writeOnly: true, minLength: 12, maxLength: 1024 }) }, ['current_password', 'new_password']),
  PasswordProof: object({ current_password: str(null, { format: 'password', writeOnly: true }) }, ['current_password']),
  ResourceFields: { type: 'object', description: '字段和写权限由同身份 GET /resources/{resource}/schema 返回。只提交允许的字段；read_only 不可写，create_only 更新时省略，write_only 不回显。密钥及密码不得从列表补全。' },
};
const parameters = {
  role: { name: 'role', in: 'path', required: true, schema: { type: 'string', enum: ['admin', 'merchant'] } },
  trade: { name: 'trade', in: 'path', required: true, schema: { type: 'string', minLength: 1 } },
  id: { name: 'id', in: 'path', required: true, schema: id },
  resource: { name: 'resource', in: 'path', required: true, schema: { type: 'string' }, description: '必须是服务端已注册资源；管理员/商户允许资源、字段和动作不同。不存在的资源返回404，越权返回403。' },
  kind: { name: 'kind', in: 'path', required: true, schema: { type: 'string', enum: ['settlements', 'internal-transfers', 'profitsharing'] } },
  request_id: {name:'request_id',in:'path',required:true,schema:{type:'string',pattern:'^[a-f0-9]{64}$'}},
  sharing_no: {name:'sharing_no',in:'path',required:true,schema:sharingNo},
  scope:{name:'scope',in:'path',required:true,schema:{type:'string',enum:['global','group']}},
  scope_id:{name:'scope_id',in:'path',required:true,schema:{type:'string',pattern:'^(0|[1-9][0-9]*)$'},description:'global必须0；group必须已存在的正用户组ID。'},
};
const csrf = { name: 'X-CSRF-Token', in: 'header', required: true, schema: { type: 'string' }, description: '登录/session 返回的 csrf_token，与当前身份会话绑定。' };
const paging = [
  { name: 'page', in: 'query', schema: { type: 'integer', minimum: 1, default: 1 } },
  { name: 'limit', in: 'query', schema: { type: 'integer', minimum: 1, maximum: 100, default: 20 } },
  { name: 'q', in: 'query', schema: { type: 'string' }, description: '当前资源支持的文本筛选；不代表旧版全部筛选项。' },
];
const content = schema => ({ 'application/json': { schema } });
schemas.Registration.properties.promotion_token=str('与注册准入invite_code独立，仅注册显式JSON提交；URL参数不建立关系。新链接使用fragment，前端在router和API请求前strip，再仅当前表单内存prefill。',{writeOnly:true,pattern:'^rp_[A-Za-z0-9_-]{43}$'});
schemas.FundsRequest.properties.recipient_config_id = str('申请时核对的不可变加密收款版本；仅settlements。缺历史版本不自动承接。',{pattern:'^[1-9][0-9]*$'});
schemas.SettlementRecipient = object({merchant_id:id,type:str(null,{enum:['alipay','wxpay','bank']}),account:str('仅写入AEAD配置，不从读取接口回显。',{writeOnly:true,maxLength:128}),name:str('仅写入，审计只记变更标记。',{writeOnly:true,maxLength:128}),bank_code:str(),app_id:str('微信钱包必须绑定真实应用；最终出款需匹配通道。'),expected_config_id:str('CAS版本，首次为0；发生冲突需重新核对。',{pattern:'^(0|[1-9][0-9]*)$'})},['type','account','name']);
schemas.PaymentPassword = object({current_password:str(null,{writeOnly:true,format:'password'}),current_payment_password:str('已设置时必填。',{writeOnly:true,format:'password'}),new_payment_password:str('独立管理员资金密码，禁止与登录密码相同。',{writeOnly:true,format:'password',minLength:12,maxLength:1024}),code:str('已绑定TOTP时必填。',{writeOnly:true})},['current_password','new_payment_password']);
schemas.FundsChallenge = object({target:str('规范的完整/console/v1/admin/金融POST路径，服务端明确白名单。'),body:{type:'object',additionalProperties:true,description:'原业务请求；服务端严格拒重复键并从实际数据库重建金额、身份、通道和政策指纹。'}},['target','body']);
schemas.FundsConfirmation = object({challenge_id:str(null,{writeOnly:true,minLength:43,maxLength:43}),payment_password:str(null,{writeOnly:true,format:'password',maxLength:1024})},['challenge_id','payment_password']);
schemas.SettlementPayoutPrepare = object({channel_id:id},['channel_id']);
schemas.ChannelSharingOwner=object({merchant_id:id});
schemas.ChannelSharingIntent=object({merchant_id:id,external_no:sharingNo},['external_no']);
schemas.ChannelSharingReceiverCreate=object({merchant_id:id,channel_id:id,external_no:sharingNo,type:str(null,{enum:['MERCHANT_ID','PERSONAL_OPENID','userId']}),account:str('完整账号只写入AEAD；支付宝仅规范2088用户ID，不支持猜测loginName/openId别名。',{writeOnly:true,maxLength:128}),name:str('仅写入完整名称，读取仅返回掩码。',{writeOnly:true,maxLength:256}),relation_type:str('微信关系类型。'),custom_relation:str('微信CUSTOM时必填。',{maxLength:30})},['channel_id','external_no','type','account']);
schemas.ChannelSharingReceiverCreate.properties.account.description += ' wxpay_v2_single_direct按UTF-8最多64字节，其他产品最多128字节；以服务端当前产品校验为准。';
Object.assign(schemas.ChannelSharingReceiverCreate.properties.name,{maxLength:1024,description:'完整名称仅写入AEAD，读取只返回掩码。V2单次产品最多1024个UTF-8字节，V3/支付宝最多256字节；微信商户接收方必填。'});
schemas.ChannelSharingReceiverCreate.properties.custom_relation.description = '微信CUSTOM时必填。V2单次产品最多10个Unicode字符且40个UTF-8字节；V3最多30个UTF-8字节。';
schemas.ChannelSharingRuleItem=object({receiver_id:id,mode:str(null,{enum:['rate','fixed']}),rate_bps:minor,fixed_amount:minor},['receiver_id','mode','rate_bps','fixed_amount']);
schemas.ChannelSharingRuleWrite=object({merchant_id:id,expected:str('当前规则ID的CAS版本，首次0；不是revision序号。',{pattern:'^(0|[1-9][0-9]*)$'}),enabled:{type:'boolean'},minimum_amount:minor,methods:{type:'array',minItems:1,maxItems:10,items:str()},items:{type:'array',minItems:1,maxItems:50,items:ref('ChannelSharingRuleItem')}},['expected','enabled','minimum_amount','methods','items']);
schemas.ChannelSharingRuleDisable=object({merchant_id:id,expected:str('原规则ID，必须与当前版本一致。',{pattern:'^[1-9][0-9]*$'})},['expected']);
schemas.ChannelSharingSubmit=object({merchant_id:id,trade_no:sharingNo,external_no:sharingNo},['trade_no','external_no']);
schemas.ChannelSharingReturnSubmit=object({merchant_id:id,sharing_external_no:sharingNo,receiver_id:id,external_no:sharingNo,amount:str('本次回退金额，正整数分字符串；0.10元必须传10。',{pattern:'^[1-9][0-9]*$'})},['sharing_external_no','receiver_id','external_no','amount']);
schemas.ChannelSharingReceiverView=object({id,merchant_id:id,channel_id:id,provider:str(),app_id:str(),product:str(),type:str(),account_masked:str(),name_masked:str(),state:str(),operation:{type:'object',description:'原绑定/解绑操作；source=reference是复用原可信关系的本地引用，不代表再次远端绑定或姓名核验。'},created_at:str(null,{format:'date-time'})},['id','merchant_id','channel_id','provider','type','account_masked','name_masked','state']);
schemas.ChannelSharingOrderView=object({id,order_id:id,merchant_id:id,external_no:sharingNo,out_no:str(),provider_order_no:str(),state:str(null,{enum:['unsent','claimed','pending','unknown','succeeded','failed','partial']}),completed:{type:'boolean'},funds_released:{type:'boolean'},released_amount:minor,items:{type:'array',items:object({receiver_id:id,amount:minor,state:str(null,{enum:['pending','succeeded','failed']}),provider_detail_no:str(),failure_code:str()},['receiver_id','amount','state'])},created_at:str(null,{format:'date-time'})},['id','order_id','merchant_id','external_no','state','completed','funds_released','released_amount','items']);
schemas.ChannelSharingReturnView=object({id,sharing_id:id,order_id:id,merchant_id:id,receiver_id:id,external_no:sharingNo,out_no:str(),amount:minor,state:str(null,{enum:['unsent','claimed','unknown','pending','succeeded','failed']}),provider_return_no:str(),created_at:str(null,{format:'date-time'})},['id','sharing_id','order_id','merchant_id','receiver_id','external_no','out_no','amount','state']);
schemas.ChannelSharingFinishView=object({id:minor,order_id:id,merchant_id:id,trade_no:sharingNo,external_no:str(),out_no:str(),provider_order_no:str(),provider_trade_no:str(),state:str(null,{enum:['eligible','unsent','claimed','pending','unknown','failed','succeeded']}),source:str(null,{enum:['finish','sharing']}),gross_amount:minor,merchant_net:minor,shared_amount:minor,expected_unsplit:minor,release_amount:minor,funds_released:{type:'boolean'},sharing:ref('ChannelSharingOrderView'),created_at:str(null,{format:'date-time'})},['id','order_id','merchant_id','trade_no','state','source','gross_amount','merchant_net','shared_amount','expected_unsplit','release_amount','funds_released']);
schemas.ChannelSharingFinishView.properties.previous_failed_id=str('eligible预览大于0表示已核实全部原失败证据，可以显式新号、新授权申请；unknown/pending不能借此重发。首申请0。',{pattern:'^(0|[1-9][0-9]*)$'});
const responses = {
  200: { description: '结果已返回；资金结果须检查 data.status/state，不能仅以 HTTP200 推断成功。', content: content(ref('Envelope')) },
  400: { description: 'JSON、字段类型或请求参数错误。', content: content(ref('Envelope')) },
  401: { description: '未认证、会话已撤销、密码/TOTP错误。', content: content(ref('Envelope')) },
  403: { description: 'CSRF、角色或资源权限拒绝。', content: content(ref('Envelope')) },
  404: { description: '资源不存在或不属于当前商户。', content: content(ref('Envelope')) },
  409: { description: '业务幂等冲突、状态冲突或资金不足。', content: content(ref('Envelope')) },
  422: { description: '业务输入或当前状态不允许。', content: content(ref('Envelope')) },
  428: { description: '启用管理员资金授权后，缺少配置或当前具体动作授权；不创建新资金意图。原号已存在的幂等意图只返回原状态。', content: content(ref('Envelope')) },
  503: { description: '依赖不可用、外部写入关闭或通道未启用。', content: content(ref('Envelope')) },
};
const paths = {};
let operationCounter = 0;
function add(route, method, summary, { body, description, list = false, created = false, uncertain = false, public: unauthenticated = false, publicScope = false } = {}) {
  const full = `/console/v1/${publicScope ? 'public' : '{role}'}${route}`;
  const params = [...full.matchAll(/\{([^}]+)\}/g)].map(([, key]) => structuredClone(parameters[key]));
  const write = !['get', 'head'].includes(method);
  if (write && !unauthenticated) params.push(csrf);
  if (list) params.push(...paging);
  const rs = structuredClone(responses);
  if (created) rs['201'] = { description: '已创建相应本地资源；资金申请仅预留，身份challenge无资金变更，均不代表外部已出款。', content: content(ref('Envelope')) };
  if (uncertain) rs['202'] = { description: '意图已持久化但结果未确认；查询原业务号，禁止生成新号或重发资金请求。', content: content(ref('Envelope')) };
  if (unauthenticated) rs['429'] = { description: '登录频率限制。', content: content(ref('Envelope')) };
  (paths[full] ??= {})[method] = {
    operationId: `console_${++operationCounter}_${method}`,
    summary, ...(description ? { description } : {}), parameters: params,
    ...(unauthenticated ? { security: [] } : {}),
    ...(body ? { requestBody: { required: true, content: content(ref(body)) } } : {}), responses: rs,
  };
}
add('/login', 'post', '登录当前后台身份', { body: 'Login', public: true, description: '设置 HttpOnly、SameSite=Strict 的对应角色 Cookie，返回 user 和 csrf_token；本地 HTTP 不启用 Secure，正式 HTTPS 应开启。' });
add('/session', 'get', '读取当前用户和 CSRF token');
add('/referral/overview','get','商户本人读取邀请关系与奖励历史流水汇总',{description:'仅merchant本人且非delegated session；无merchant_id等query选择器。invited_count/issued_amount/voided_amount/recovery_pending_amount/today_issued/yesterday_issued均非负整数字符串，金额为分。今日/昨日按UTC+08。已发/已撤/退款暂存是历史流水，不代表可提现余额。逐行复验原HMAC；不接受admin假actor。'});
add('/referral/token','get','读取当前推广令牌掩码、版本及有效期',{description:'仅merchant本人；无版本时id/version0、enabled=false。返回masked而非既有bearer，expires_at存在时需同时检查过期。No-store，禁止raw token日志/storage。'});
for(const action of ['rotate','revoke'])add(`/referral/token/${action}`,'post',action==='rotate'?'显式发行/轮换本人推广令牌，仅一次返回完整值':'显式撤销本人推广令牌，保留已绑定关系',{body:'ReferralTokenChange',description:'CSRF+本人merchant+CAS；拒admin和delegated会话，禁止query/owner。每账号IP10次/15分钟。新token默认90天有效；旧token立即失效；不存在旧bearer reveal。丢响应不重试，先读取版本，必要时再次显式轮换。与一次性注册准入邀请码独立，不写日志/storage。'});
for(const kind of ['invitees','rewards']){
 add(`/referral/${kind}`,'get',kind==='invitees'?'商户本人游标读取已注册邀请关系':'商户本人游标读取原奖励及退款推导状态',{description:kind==='invitees'?'返回items及next_cursor；item为id/invited_id/promoter_id/token_id/token_version/created_at，均ID字符串；无OTP challenge/proof/email/bearer。next_cursor是关系ID，满页后可能再读到空页。':'返回items及next_cursor；item为id/order_id/invited_id/trade_no/amount（分字符串）/state(active|recovery_pending|voided)/created_at。状态由原奖励和退款回收证明推导，active不代表当前可提现。仅merchant本人；拒owner selector。'});
 paths[`/console/v1/{role}/referral/${kind}`].get.parameters.push({name:'after_id',in:'query',schema:{type:'string',pattern:'^(0|[1-9][0-9]*)$',default:'0'}},{name:'limit',in:'query',schema:{type:'integer',minimum:1,maximum:100,default:20}});
}
add('/referral-policies/{scope}/{scope_id}','get','super读取当前邀请政策版本',{description:'仅admin/super本人；scope为global/id0或group/正用户组ID；无query。缺政策返回revision/id0，nullable项未配置。覆盖顺序global→payer group→promoter group；null继承、0明确覆盖。'});
add('/referral-policies/{scope}/{scope_id}','post','super以CAS与资金授权保存不可变邀请政策',{body:'ReferralPolicy',description:'仅admin/super本人。六字段全必填且拒重复/未知。仅immediate/fee/any_successful_refund_voids_reward；不开放daily/profit/会员奖励。影响新订单快照，不追补旧单。资金guard key为scope:id:expected，旧revision或变化group快照拒绝。丢响应先GET核对，不自动重试。'});
paths['/console/v1/{role}/referral-policies/{scope}/{scope_id}'].post.parameters.push({name:'X-Payment-Authorization',in:'header',required:false,schema:{type:'string',minLength:43,maxLength:43,writeOnly:true},description:'admin_funds_authorization_enabled=true时必须是原政策CAS和六字段的短时资金授权；配置、授权消费与审计同事务。'});
for(const [legacy,source,method,body] of [['/user/invite/inviteInfo','/referral/overview','get',null],['/user/invite/inviteList','/referral/invitees','post','ReferralAliasCursor']]){
 const op=structuredClone(paths['/console/v1/{role}'+source].get);op.operationId='legacy_safe_'+legacy.split('/').pop();op.parameters=method==='get'?[]:[csrf];op.security=[{MerchantSession:[]}];op.summary='旧邀请入口的安全DTO别名';op.description='只复用新Go安全DTO、本人merchant会话/CSRF，不兼容原PHP session/envelope/current-size或offset-limit。Cookie Path保持/console/v1/merchant，兼容HTTP客户端需明确携Go cookie；不扩宽浏览器cookie权限。响应X-Goepay-Contract说明非PHP JSON等价。'+op.description;if(body)op.requestBody={required:true,content:content(ref(body))};paths[legacy]={[method]:op};
}
add('/logout', 'post', '撤销当前会话');
add('/dashboard', 'get', '账本余额与订单摘要');
add('/security/payment-password','get','原管理员会话读取资金密码状态及enforced开关',{description:'仅admin；不是商户登录密码。未设置时configured=false，没有默认密码。'});
add('/security/payment-password','put','设置或修改本人独立资金密码',{body:'PaymentPassword',description:'校验登录密码、原资金密码和已绑定TOTP；凭据版本递增，旧动作授权立即失效。错误资金密码403，不注销有效登录。'});
add('/security/funds-authorizations/challenges','post','预览具体管理员资金动作',{body:'FundsChallenge',created:true,description:'最多2分钟，只返回opaque challenge及服务端脱敏摘要；无资金预留和支付机构调用。原会话及实际DB指纹绑定，不接受客户端自报可信hash。'});
add('/security/funds-authorizations/confirm','post','核验资金密码签发短时逐动作授权',{body:'FundsConfirmation',description:'最多5分钟且不超过原session；令牌仅通过X-Payment-Authorization提交原动作，不能通过URL、storage、日志保存。5失败/15分钟及有界Argon2并发；不自动重试资金请求。'});
add('/workflows/settlement-recipient','get','读取本商户当前加密收款版本和掩码',{description:'admin必填merchant_id；商户仅本人。旧明文设置不当作已验证快照。'});
paths['/console/v1/{role}/workflows/settlement-recipient'].get.parameters.push({name:'merchant_id',in:'query',schema:id});
add('/workflows/settlement-recipient','post','以CAS创建不可变AEAD收款配置',{body:'SettlementRecipient',description:'配置资料不认证收款机构身份、不发起付款。申请时复制独立request AAD快照，之后改资料不改变原申请。'});
add('/settlement-payouts/{id}','get','所属商户或管理员读取结算审批及实际出款状态',{description:'data.request是原申请，data.payout可以为null。审批approved与付款succeeded分离；返回金额和ID字符串及收款掩码。'});
add('/settlement-payouts/{id}/capabilities','get','管理员读取原结算可绑定的明确产品通道',{description:'本地读取，不调用支付机构；原gross专属预留、费用/收款快照、AppID和白名单必须匹配。bank类型另返回bank_product=wxpay_v2或alipay_bankcard及fee_policy=hold-and-return-v1，不可将两类退票证据互用。'});
add('/settlement-payouts/{id}/prepare','post','管理员将已批准结算绑定唯一原TF',{body:'SettlementPayoutPrepare',description:'钱包或明确bank产品；仅本地准备，不再扣可用余额、不加普通代付费用、不调用上游。同申请唯一不可变渠道/TF/公开付款账户摘要，原收款快照不随新资料改变。历史真实付款缺账户摘要不按当前配置自动补造。'});
for(const action of ['execute','query','cancel-unsent'])add(`/settlement-payouts/{id}/${action}`,'post',`结算出款 ${action}`,{body:'Empty',uncertain:true,description:action==='execute'?'仅管理员首次领取；同事务消费具体资金授权、固定发送屏障、审计和核实任务后才发送net。unknown只查原TF。银行卡成功后结算fee留原申请账户，funding=paid_fee_held，不确认为收入；普通TF.fee_amount=0。':action==='query'?'所属商户或管理员核实原TF；停写仍可用、无新资金密码、不重发。银行卡成功仍可只读查询晚退票；证据不完整state=reversal_pending不释放；原产品可信退票state=reversed、funding=returned，本金和保留fee一次返还。':'仅管理员持锁证明unsent后取消，原gross返还available；claimed/unknown不能取消或伪造支付机构失败。'});
add('/sso/grants','post','super管理员创建短时代登录授权',{body:'SSOGrant',created:true,description:'仅admin/super，需当前CSRF；目标必须有已启用后台账号，grant有效60秒，仅返回当次使用。'});
add('/sso/consume','post','原管理员会话消费一次性授权',{body:'SSOConsume',description:'仅admin/super，绑定同一管理员Cookie及CSRF。提交同事务生成15分钟商户会话、发行者关联与审计；替换浏览器商户Cookie。管理员退出/撤权/版本变化后代登录失效，每次代登录写操作先审计真实管理员。'});
add('/capabilities','get','注册与邮件验证能力',{public:true,publicScope:true});
add('/verification-code','post','申请注册或恢复验证',{body:'Verification',public:true,publicScope:true,uncertain:true,description:'邮箱和来源持久限流；响应只有opaque挑战ID与到期时间，无验证码。未配置发送器时503。'});
add('/register','post','消费邮箱证明创建零余额商户',{body:'Registration',public:true,publicScope:true,created:true,description:'受registration_open及邀请码政策限制；不自动登录，不回显APIKey。'});
add('/recover-password','post','消费可信邮箱证明更新密码',{body:'Recovery',public:true,publicScope:true,description:'不信任可编辑资料邮箱，只认已验证绑定。撤销全部原会话，保留TOTP，不自动登录。'});
for (const resource of ['orders', 'refunds', 'ledger', 'merchants']) add(`/${resource}`, 'get', `读取 ${resource} 分页`, { list: true, description: resource === 'merchants' ? '商户列表由管理员身份读取；当前商户数据范围仍由后端裁决。' : undefined });
for (const resource of ['orders', 'refunds', 'ledger', 'merchants']) {
  const params = paths[`/console/v1/{role}/${resource}`].get.parameters;
  params.push({ name: 'merchant_id', in: 'query', schema: id, description: '管理员可筛选；商户省略或等于自己，其他商户ID返回403。' });
  params.push(...['from','to'].map(name => ({ name, in: 'query', schema: {type:'string',format:'date'}, description: '创建时间按UTC+8，包含起止当天；同时传入时最多367天。' })));
  if (resource === 'orders' || resource === 'refunds') params.push({name:'type',in:'query',schema:{type:'string',enum:['alipay','wxpay','sandbox']}},{name:'channel_id',in:'query',schema:id},{name:'status',in:'query',schema:{type:'string',enum:resource==='orders'?['created','submitting','pending','paid','closed','failed','unknown']:['pending','unknown','succeeded','failed']}});
  if (resource === 'orders') params.push({name:'income_status',in:'query',schema:{type:'string',enum:['none','held','released']},description:'商户收入冻结的独立状态，不改变已支付事实。'},{name:'buyer',in:'query',schema:str(null,{maxLength:256}),description:'已验证买家标识精确匹配。'},{name:'client_ip',in:'query',schema:str(),description:'原客户IP字面量，支持IPv4/IPv6精确匹配。'});
  if (resource === 'merchants') params.push({name:'enabled',in:'query',schema:{type:'boolean'}});
}
add('/orders/{trade}', 'get', '订单详情及支付/预授权/释放状态', { description: 'provider.can_close 和 can_release_authorization 是服务端许可；付款成功前，授权金额不进入商户余额。' });
for (const action of ['query', 'close', 'release-authorization', 'query-authorization-release', 'capture-authorization']) add(`/orders/{trade}/${action}`, 'post', `订单 ${action}`, { body: 'Empty', uncertain: true, description: '检查所属商户并在外部I/O前写入审计意图。close 不用于解除预授权。release-authorization 仅允许未消费完整授权；解冻不是退款，不贷记商户账本。capture-authorization 仅在双重可信查询确认完整未消费授权后首次扣取；未知/失败不重发，改为查询原订单。' });
add('/orders/{trade}/refund', 'post', '发起幂等退款', { body: 'Refund', uncertain: true });
add('/orders/{trade}/freeze-income','post','管理员冻结可核实的原订单剩余净收入',{body:'IncomeFreeze',description:'仅管理员；金额由原支付/已成功退款双边凭证确定。订单保持paid，冻结款进入原hold专用账户；该订单退款优先使用自身冻结本金，不借用其它hold。'});
add('/orders/{trade}/unfreeze-income','post','管理员按原hold ID解除剩余收入冻结',{body:'IncomeUnfreeze',description:'只释放原批次扣除已成功退款后的剩余本金；原批次存在任何pending/unknown退款时拒绝，即使该笔仅用手续费。旧ID重放不能释放新cycle；0余款有不可变释放证明，不制造0金额journal。资金与审计同事务，不调用支付机构。'});
add('/orders/{trade}/preview-refund','post','按原订单本地核对退款资金来源',{body:'Refund',description:'本人商户或管理员；CSRF、strict JSON、拒query。读取原订单冻结来源、历史退款和HMAC，返回全部金额/ID的整数分字符串：held_amount、available_amount、fee_amount、held_remaining、hold_id及funding_mode。内部HMAC不回显。不创建退款、预留、授权或provider请求。实际确认在原事务锁后重算，不将预览作为付款许可。'});
add('/orders/{trade}/adopt-income-hold','post','本人超级管理员核对并迁移历史冻结款专用来源',{body:'IncomeHoldAdoption',description:'仅admin/super且非代登录；即使全局资金确认关闭，也必须本人短时逐动作资金密码授权。只采用真实迁移前旧hold，经原双边journal/来源/审计及旧池总额守恒核对，将原本金迁入独立hold账户；不增加余额、不发起外部退款。原hold_id/external_no幂等。'});
{
 const original='/console/v1/{role}/orders/{trade}/adopt-income-hold';
 const op=paths[original].post; delete paths[original];
 op.parameters=op.parameters.filter(p=>p.name!=='role');
 op.parameters.push({name:'X-Payment-Authorization',in:'header',required:true,schema:{type:'string',minLength:43,maxLength:43,writeOnly:true},description:'首次迁移必须具备本人super的原hold/原证据/原请求号授权，消费与迁移及审计同事务。'});
 op.security=[{AdminSession:[]}];
 paths['/console/v1/admin/orders/{trade}/adopt-income-hold']={post:op};
}
add('/orders/{trade}/resend-notification', 'post', '持久排队或合并商户通知重发', { body: 'NotificationResend', uncertain: true, description: '仅已支付且保存通知地址的订单；服务端固定使用原URL。返回 job_id/state；202不代表商户已收到，succeeded代表一次合法success应答。新手动投递60秒冷却，同幂等号返回原任务。此操作不修改支付与账本。' });
add('/transfers/capabilities', 'get', '读取当前商户转账通道、开关和限额', { description: '管理员必须提供 merchant_id 查询参数；仅显式广告的 type 可使用，不能由通道平台名推断银行卡或凭证能力。' });
paths['/console/v1/{role}/transfers/capabilities'].get.parameters.push({ name: 'merchant_id', in: 'query', schema: id, description: '管理员必填；商户必须等于自己或省略。' });
add('/transfers', 'get', '转账分页', { list: true });
add('/transfers', 'post', '持久化转账意图并尝试一次发送', { body: 'Transfer', uncertain: true, description: '同商户 external_no 幂等，服务端生成全局 TF 编号。只允许显式平台及商户开关、通道白名单和产品能力。未知保留预占。银行卡受理不是到账；bank_status 与历史 ledger_status 分开。' });
add('/transfers/{id}', 'get', '读取转账有效状态与银行生命周期');
add('/transfer-batches','get','按所属商户读取持久批次或恢复原请求号',{description:'管理员必填merchant_id；商户不能选择他人。external_no为精确匹配；limit默认20最大100，before_id为游标。只读本地，不发送或查询上游。'});
add('/transfer-batch-imports/preview','post','校验原五列CSV/XLSX，仅返回当前页面私有草稿',{body:'TransferBatchImport',description:'独立认证/角色/CSRF/owner校验；管理员必填merchant_id，商户不能选择他人；禁止query参数。最多10次/15分钟/账号+IP，JSON上限704KiB，文件上限512KiB，最多100条。首行必须精确为序号/收款账号/收款人姓名/转账金额（元）/转账备注，序号唯一；只读第一工作表，拒公式/外链/宏/不安全ZIP/XML/数值账号。返回data.merchant_id/format/count/total_cents（整数分字符串）、rows（掩码账号/姓名、元金额、has_remark）和draft_items（完整受款资料，仅当前认证浏览器内存）。Cache-Control:no-store；不存文件/受款资料、不创建批次、不预留资金、不消费资金授权、无provider I/O。应用草稿后仍须独立批次创建/资金授权和执行。422 data.error/source_row仅诊断不包含原字段值；无效文件整份拒绝、不返部分草稿。'});
paths['/console/v1/{role}/transfer-batches'].get.parameters.push({name:'merchant_id',in:'query',schema:id},{name:'external_no',in:'query',schema:str(null,{maxLength:128})},{name:'before_id',in:'query',schema:str(null,{pattern:'^[0-9]+$'})},{name:'limit',in:'query',schema:{type:'integer',minimum:1,maximum:100,default:20}});
add('/transfer-batches','post','原子校验并预留整批资金，不发送付款',{body:'TransferBatchPrepare',description:'任一条无效或资金不足整批回滚。每项独立原TF编号；响应丢失后用原external_no查找，不创建替代批次。费用/总额响应为整数分字符串，隐藏原受款账号姓名。'});
add('/transfer-batches/{id}','get','读取原批次与逐项发送边界');
add('/transfer-batches/{id}/execute','post','单独确认领取并发送未发送项目',{body:'TransferBatchExecute',description:'15秒执行预算，仅unsent可领取一次，领取和审计在I/O前提交。已claimed/未知/失败项不再次发送，进程中断由原编号只读核对。200绝不代表全成功，检查summary/all_succeeded及每项。'});
for(const action of ['query','cancel-unsent'])add(`/transfer-batches/{id}/${action}`,'post',action==='query'?'只读核对已发送项目':'原子取消可证明未发送的项目',{body:'Empty',description:'按批次持久归属授权，停写后仍可使用。取消仅释放未领取项目的原预占，不伪造银行退票或失败回执。'});
for (const action of ['query', 'proof']) add(`/transfers/{id}/${action}`, 'post', `转账 ${action}`, { body: 'Empty', uncertain: true, description: action === 'proof' ? '仅支持凭证的支付宝账号通道；申请是外部写操作，后续查询原申请号。' : '只核对原TF编号，不重新发送；缺少退票手续费等证据时保留 reversal_pending。' });
add('/resources/{resource}/schema', 'get', '读取角色过滤后的资源表单与权限');
add('/resources/{resource}', 'get', '资源列表', { list: true });
add('/resources/{resource}', 'post', '创建资源', { body: 'ResourceFields', created: true });
add('/resources/{resource}/{id}', 'get', '读取资源');
add('/resources/{resource}/{id}', 'patch', '更新允许字段', { body: 'ResourceFields' });
add('/resources/{resource}/{id}', 'put', '更新允许字段（同 PATCH 语义）', { body: 'ResourceFields' });
add('/resources/{resource}/{id}', 'delete', '删除允许删除的资源');
add('/workflows/{kind}', 'get', '站内资金审批申请列表', { list: true });
add('/workflows/funds-availability', 'get', '读取当前资金出账政策与可出账金额', { description: 'available_money 为可出账金额；ledger_available_money 为账本可用余额；today_blocked_money 为 T+1 当日受限部分；settle_type 为全局政策数值0或1，与商户收款账户类别不同。只有该查询结果和实际预占事务的校验可用于出账判断。管理员必填 merchant_id，商户仅查询自己。' });
add('/workflows/settlement-policy','get','读取结算政策或按金额给出一致快照报价',{description:'管理员必填merchant_id；商户仅自己。amount为可选元字符串，省略仅查政策；terms中的gross/fee/net/retained为整数分字符串。can_apply不是授权，提交仍在事务重查留存/日次数/T+1。新界面提交同时传expected_fee_cents和expected_net_cents，政策变化409要求重新确认。报价/申请/审批均external_payout=false。'});
paths['/console/v1/{role}/workflows/settlement-policy'].get.parameters.push({name:'merchant_id',in:'query',schema:id},{name:'amount',in:'query',schema:money});
paths['/console/v1/{role}/workflows/funds-availability'].get.parameters.push({ name: 'merchant_id', in: 'query', schema: id, description: '管理员必填，商户省略或等于本身份。' });
add('/workflows/{kind}', 'post', '创建资金冻结申请', { body: 'FundsRequest', created: true, description: 'internal-transfers/profitsharing 必须提供 recipient_id；settlements 不执行外部付款。管理员 merchant_id 必填。' });
for (const action of ['approve', 'reject']) add(`/workflows/{kind}/{id}/${action}`, 'post', `管理员 ${action} 资金申请`, { body: 'Empty', description: '仅管理员。结算批准仍保留冻结等待独立可验证出款；站内转账/分账只在平台内部记账。' });
add('/account/profile', 'get', '商户个人资料');
add('/account/profile', 'patch', '修改商户个人资料', { body: 'Profile' });
add('/account/api-info', 'get', '商户 API 身份与平台公钥', { description: '不会返回完整现有 API key；显示掩码。' });
add('/account/password', 'post', '验证当前密码并修改密码', { body: 'PasswordChange', description: '管理员及商户可用；成功撤销旧会话，必须重新登录。' });
add('/account/api-key/rotate', 'post', '验证密码并轮换商户 API key', { body: 'PasswordProof', description: '仅商户；新 key 只在本次响应中返回，调用方必须妥善保存。' });
add('/collection-codes/{id}/link', 'get', '生成限时聚合收款链接', { description: '所属商户或管理员；停用/删除收款码会立即撤销旧链接。链接 token 不得写日志。' });
add('/identity-lookups/capabilities','get','读取独立网页授权能力',{description:'原始会话；管理员必须提供merchant_id，商户仅自己。返回available/reason/channels，无外呼；沙箱明确不可用。'});
paths['/console/v1/{role}/identity-lookups/capabilities'].get.parameters.push({name:'merchant_id',in:'query',schema:id});
add('/identity-lookups','post','创建独立用户标识授权请求',{body:'IdentityLookupCreate',created:true,description:'原始会话及CSRF，拒绝代登录。管理员merchant_id必填。限五分钟且不超过会话；返回一次授权URL，不创建订单。使用固定通道及应用，令牌仅供首浏览器发起。'});
add('/identity-lookups/{request_id}','get','查询原会话授权状态',{description:'request_id为64位小写hex；无查询参数。仅原会话，返回状态和应用元数据，不返回标识、令牌或授权URL。'});
add('/identity-lookups/{request_id}/reveal','post','显式读取一次用户标识',{body:'Empty',description:'仅原始发起会话，CSRF且状态verified。返回subject_id/label/provider/app_id；并发仅成功一次，重复409，响应丢失不自动重发。应用及商户身份改变或会话撤销均拒绝。仅内存展示，禁止日志、URL和浏览器存储。'});
const sharingScope='管理员必须显式提供merchant_id（GET在query、POST在JSON）；商户由当前会话决定身份，禁止提供merchant_id字段，即使为空或为自己。规范JSON拒绝重复键、无关字段和null。';
function sharingAdd(route,method,summary,options={}) {
  add(`/channel-sharing${route}`,method,summary,{...options,description:`${sharingScope} ${options.description||''}`});
  const op=paths[`/console/v1/{role}/channel-sharing${route}`][method];
  if(method==='get')op.parameters.push({name:'merchant_id',in:'query',schema:id,description:'仅管理员必填；商户不能提供。'});
  return op;
}
sharingAdd('/channels','get','读取可用渠道分账能力',{description:'无上游外呼；当前白名单内产品返回channel_id/provider/product/methods/receiver_types/return_supported/finish_supported。明确产品为wxpay_v3_direct、wxpay_v2_single_direct、alipay_direct；V2使用原单次分账语义并支持付款码初次声明，未声明历史支付不回填。模拟或未启用返回available=false和reason。支付宝持久接收方仅userId，不直接开放wire别名。'});
for(const [kind,view]of [['receivers','ChannelSharingReceiverView'],['orders','ChannelSharingOrderView'],['returns','ChannelSharingReturnView']]) {
 const op=sharingAdd(`/${kind}`,'get',`读取所属商户渠道分账${kind}列表`,{description:'cursor为规范非负ID字符串，默认0；limit规范整数1..100默认20。返回items与next_cursor（字符串0表示无后页）；只读本地，不自动向上游查询。'});
 op.parameters.push({name:'cursor',in:'query',schema:minor},{name:'limit',in:'query',schema:{type:'integer',minimum:1,maximum:100,default:20}});
 op.responses['200'].content=content(object({code:{type:'integer'},message:str(),data:object({items:{type:'array',items:ref(view)},next_cursor:minor},['items','next_cursor'])},['code','message','data']));
}
sharingAdd('/receivers','post','加密保存不可变接收方草稿',{body:'ChannelSharingReceiverCreate',description:'只创建本地AEAD草稿，不自动绑定。完整账号/姓名仅本次写入，读取只返回掩码；不可用通道拒绝，不能用资料CRUD伪造远端成功。'});
sharingAdd('/receivers/{id}','get','读取原接收方脱敏身份与操作状态');
for(const action of ['bind','unbind'])sharingAdd(`/receivers/{id}/${action}`,'post',`单独确认渠道接收方${action}`,{body:'ChannelSharingIntent',description:'先持久化原PB/PU号、发送屏障和审计，未知结果409且禁止换号重发。启用资金政策时仅管理员首次操作要求对应具体授权。复用已可信关系显示source=reference；解绑本地引用不得删除其他所有者的活跃关系。'});
sharingAdd('/rules/{id}','get','读取当前渠道分账规则版本',{description:'id为通道ID；无规则返回404。读取包含真实规则ID/revision、方法/最低分金额、接收方比例或固定分金额；只读原版本，不解密接收方完整账号。'});
sharingAdd('/rules/{id}','post','CAS发布渠道分账规则新版本',{body:'ChannelSharingRuleWrite',description:'id为通道ID；expected使用原规则ID，首次0。每项mode=rate时rate_bps为万分比且fixed_amount为0；mode=fixed时fixed_amount为分且rate_bps为0；接收方必须已绑定。只影响随后新建支付的不可变声明，不能给历史订单补声明。启用资金政策时管理员需规则动作授权。'});
sharingAdd('/rules/{id}/disable','post','CAS禁用原规则',{body:'ChannelSharingRuleDisable',description:'只停止未来声明，不改变原支付快照；即使旧通道已移除仍可停止规则。管理员首次禁用需具体授权。'});
sharingAdd('/payment/{trade}','get','核对原支付分账声明',{description:'返回declared、gross_amount、merchant_net及逐接收方amount，全部金额为分字符串。声明为false的订单不能事后承接分账规则。充值用途不声明分账。'});
sharingAdd('/orders','post','按原支付声明提交唯一渠道分账',{body:'ChannelSharingSubmit',description:'只使用原接收方/金额、已验证支付回执及原订单专属净额预留；请求不接受客户端分账金额。未知409；同external_no重放只读原记录，不隐式查询或再发，新号也不能重发原订单。completed与全部成功不同，只有可信原解冻证明才funds_released。'});
sharingAdd('/orders/{sharing_no}','get','读取原渠道分账记录',{description:'sharing_no为原商户external_no；本地读取且校验不可变意图与结果证据，不调用上游。'});
sharingAdd('/orders/{sharing_no}/query','post','只读核对原渠道分账请求并处理可信结果',{body:'ChannelSharingOwner',description:'使用已持久化的原PS号及交易号；不消费新资金授权、不发送新的资金请求。微信显式剩余0等原证明后才释放原剩余净额；FINISHED本身不等于所有接收方成功。'});
sharingAdd('/returns','post','从原成功接收方独立回退资金',{body:'ChannelSharingReturnSubmit',description:'要求原PS已经完整完成且原余款已可信释放。指定原成功receiver，累计非failed回退不得超过原成功额；不接受新账号，不是买家退款。支持明确微信商户接收方与显式启用的支付宝原分账回退产品；未知409保留原PR号。启用政策时管理员需回退动作授权。'});
sharingAdd('/returns/{sharing_no}','get','读取原分账回退记录',{description:'sharing_no为回退的原external_no；原单/接收方/HMAC归属校验，只返回安全状态。'});
sharingAdd('/returns/{sharing_no}/query','post','只读查询原PR并一次回到原商户',{body:'ChannelSharingOwner',description:'不发新回退，不消费新资金授权；必须核对原金额、原接收方和可信回退凭证后一次记账。pending/unknown不计到账，succeeded表示回到原商户而不是买家退款。'});
sharingAdd('/finish-preview/{trade}','get','读取原订单可验证完结预览',{description:'只读本地与当前产品能力；不调用上游。Source=sharing且funds_released时返回原PS可信结果，不创建PF；Source=finish且state=eligible表示独立完结候选，id=0且external_no/out_no为空。仅微信直连V3独立PF，支付宝缺可信终态证明保持关闭。金额均为分字符串；上游expected_unsplit与本地release_amount按原gross/net分别核对，不能混用。'});
const finishList=sharingAdd('/finishes','get','读取所属商户独立完结记录',{description:'limit规范整数1..100默认20；返回items，按最新ID倒序。没有cursor参数；旧记录可按原external_no读取或按原订单预览恢复。'});
finishList.parameters.push({name:'limit',in:'query',schema:{type:'integer',minimum:1,maximum:100,default:20}});
finishList.responses['200'].content=content(object({code:{type:'integer'},message:str(),data:object({items:{type:'array',items:ref('ChannelSharingFinishView')}},['items'])},['code','message','data']));
sharingAdd('/finishes','post','按原支付金额申请独立完结',{body:'ChannelSharingSubmit',description:'只允许未PS或已证明未受理的原微信分账支付；已成功PS的自动解冻只返回原结果，不另建PF。不接受客户端金额。先固定原PF号/审计/任务并消费管理员具体完结授权，再发送；受理回执即使FINISHED也只记pending，不释放。未知409只保留原号核实。'});
sharingAdd('/finishes/{sharing_no}','get','读取原PF记录及可信资金状态',{description:'sharing_no为原external_no；归属、原金额与结果HMAC校验。本地读取无上游外呼。'});
sharingAdd('/finishes/{sharing_no}/query','post','只读查询原PF并核对原收款商户解冻结果',{body:'ChannelSharingOwner',description:'不消费新资金授权、不重发。核对原交易、原PF号、原收款商户和原gross解冻金额；可信终态前funds_released=false。succeeded时一次将原商户净额从专属预留释放；空明细或签名失败不释放。'});
for (const report of ['daily', 'merchants', 'channels', 'provinces', 'buyers', 'transfer-accounts', 'transfer-channels', 'transfer-merchants', 'complaints']) {
  add(`/reports/${report}`, 'get', `读取 ${report} 报表`, { list: true, description: '日期按 UTC+8，包含 from/to 当天；不传时最近30天，最多367天。商户仅可读取 daily/provinces 且仅含自己数据；其他报表仅管理员。没有 merchant_id 任意切换身份的参数。金额保留十进制字符串。' });
  paths[`/console/v1/{role}/reports/${report}`].get.parameters.push(...['from', 'to'].map(name => ({ name, in: 'query', schema: { type: 'string', format: 'date' } })));
}
for (const op of ['jobs', 'audit-log', 'login-log', 'system-info']) add(`/operations/${op}`, 'get', `管理员读取 ${op}`, { list: op !== 'system-info' });
for (const route of Object.keys(paths)) {
  if (!/^\/console\/v1\/\{role\}\/(orders|refunds|transfers|merchants|ledger|reports\/[^/]+)$/.test(route)) continue;
  const op=paths[route].get;
  op.parameters.push({name:'format',in:'query',schema:{type:'string',enum:['csv']},description:'省略返回JSON分页；csv导出同筛选的完整结果，忽略已验证的page/limit。最多50000行或8MiB，超限422且不返回截断附件。每进程并发2个，忙时429。'});
  op.responses=structuredClone(op.responses);
  op.responses['200'].content['text/csv']={schema:{type:'string'},description:'UTF-8 BOM，固定字段顺序，公式文本加单引号。列表金额分、统计金额元；带下载审计且no-store。'};
}
add('/operations/jobs/{id}/retry', 'post', '管理员重试允许的 dead 任务', { body: 'Empty', description: '仅通知和明确白名单内的查询任务；不会重新执行支付、转账、授权或解冻的资金写操作。' });
for (const [route,methods] of Object.entries(paths)) {
  if (!methods.post || !/(?:\/(?:transfers|transfer-batches)$|\/transfer-batches\/\{id\}\/(?:execute|cancel-unsent)$|\/orders\/\{trade\}\/(?:refund|close|capture-authorization|release-authorization|freeze-income|unfreeze-income)$|\/workflows\/\{kind\}(?:\/\{id\}\/(?:approve|reject))?$|\/settlement-payouts\/\{id\}\/(?:execute|cancel-unsent)$|\/channel-sharing\/(?:orders|returns|finishes)$|\/channel-sharing\/rules\/\{id\}(?:\/disable)?$|\/channel-sharing\/receivers\/\{id\}\/(?:bind|unbind)$)/.test(route)) continue;
  methods.post.parameters.push({name:'X-Payment-Authorization',in:'header',required:false,schema:{type:'string',minLength:43,maxLength:43,writeOnly:true},description:'仅管理员且admin_funds_authorization_enabled=true时，首次具体资金意图必须提供。先预览并显式确认原请求；服务端同事务消费。已存在的相同业务意图不重新消费；202或渠道分账未知409后只查询原号。商户签名接口不增加此管理员凭据。'});
}
installSettlementPlanAPI({add,paths,schemas,object,str,id,ref,content});
installPrintingAPI({add,paths,schemas,object,str,id,ref,content});
installPageTemplateAPI({add,paths,schemas,object,str,id,ref,content});
installIdentityProfileAPI({paths,schemas,object,str,ref,content});
installLegacyRefundHistoryAPI({paths,schemas,object,str,id,ref,content});
installApplicationAuthorizationAPI({paths,schemas,object,str,id,ref,content});
installProConsoleProjectionsAPI({paths,schemas,object,str});
installPreauthObservationsAPI({paths,schemas,object,str,id,ref,content});
installPreauthRemainingAPI({paths,schemas,object,str,id,ref,content});
installReferralDailyAPI({paths,schemas,object,str,ref,content});
installProOrderSearchAPI({paths});
installMerchantRSAAPI({paths,schemas,object,str,ref,content});
installReferralFundingAPI({paths,schemas,object,str,ref,content});
installSettlementAutoAPI({paths,schemas,object,str,ref,content});
installChannelStopAPI({paths,schemas,object,str,id,ref,content});
installScanRecoveryAPI({paths,schemas,object,str,id,ref,content});
installMerchantNoticeAPI({paths,schemas,object,str,id,ref,content});
installMerchantPaymentReviewAPI({paths,schemas,object,str,id,ref,content});
installBusinessNoticeAPI({paths,schemas,object,str,id,ref,content});
const document = {
  openapi: '3.1.0', info: { title: 'GOEPAY 已实施后台 API', version: '0.1.0', description: '用于当前 Go 双后台，不是旧 PHP 管理接口兼容声明。各资源仍须读取服务端 schema/能力；本文件不把业务插件暂缓或原版缺口标记为已完成。金额单位按具体API：通用退款/转账请求amount为元字符串；/channel-sharing全部金额及回退amount为分字符串。部分历史领域响应仍为分或整数，不得全局猜测单位。' },
  servers: [{ url: 'http://127.0.0.1:8184', description: '独立本地 simulation API；以部署交接记录的版本为准' }],
  security: [{ AdminSession: [] }, { MerchantSession: [] }], paths,
  components: { schemas, securitySchemes: { AdminSession: { type: 'apiKey', in: 'cookie', name: 'goepay_admin' }, MerchantSession: { type: 'apiKey', in: 'cookie', name: 'goepay_merchant' } } },
};
// Check local references, parameter coverage and operation IDs without network.
const operations = new Set();
for (const [route, methods] of Object.entries(paths)) for (const op of Object.values(methods)) {
  assert.ok(!operations.has(op.operationId)); operations.add(op.operationId);
  for (const [, key] of route.matchAll(/\{([^}]+)\}/g)) assert.ok(op.parameters.some(p => p.in === 'path' && p.name === key && p.required));
}
function validateRefs(node) {
  if (!node || typeof node !== 'object') return;
  if (node.$ref) assert.ok(node.$ref.startsWith('#/components/schemas/') && schemas[node.$ref.split('/').at(-1)]);
  for (const child of Object.values(node)) validateRefs(child);
}
validateRefs(document);
fs.mkdirSync(path.join(root, 'api'), { recursive: true });
fs.writeFileSync(path.join(root, 'api/console.openapi.json'), JSON.stringify(document, null, 2) + '\n');
console.log(JSON.stringify({ paths: Object.keys(paths).length, operations: operations.size, output: 'api/console.openapi.json' }));
