export function installSettlementAutoAPI({paths,schemas,object,str,ref,content}) {
 const positive=str('规范正int64十进制字符串；金额为人民币分。',{pattern:'^[1-9][0-9]{0,18}$'});
 const counter=str('规范非负int64十进制字符串。',{pattern:'^(0|[1-9][0-9]{0,18})$'});
 const timestamp=str('带时区RFC3339；最长31天，须在原生成授权有效期内。',{format:'date-time'});
 const channel=object({merchant_id:positive,channel_id:positive},['merchant_id','channel_id']);
 const fields={expected_authorization_id:counter,generation_authorization_id:positive,valid_from:timestamp,expires_at:timestamp,max_item_gross:positive,max_run_gross:positive,max_day_gross:positive,max_day_items:str('每日最多100条。',{pattern:'^[1-9][0-9]?$|^100$'}),cost_policy:str('上限仅覆盖原结算毛额=净额+平台手续费；不包含支付机构账户另收费，也不保证机构成本封顶。',{const:'provider_managed_excluded'}),channels:{type:'array',minItems:1,maxItems:100,items:channel}};
 schemas.SettlementAutoAuthorizationWrite=object({expected_revision:positive,...fields},['expected_revision',...Object.keys(fields)]);
 const properties={id:counter,plan_id:positive,plan_revision:counter,...fields,revoked:{type:'boolean'},created_at:timestamp};
 // GET's absent-authority shape carries the Go zero-valued embedded input.
 for(const key of ['generation_authorization_id','max_item_gross','max_run_gross','max_day_gross','max_day_items'])properties[key]=counter;
 properties.cost_policy=str(null,{enum:['','provider_managed_excluded']});
 properties.channels={type:['array','null'],items:channel};
 schemas.SettlementAutoAuthorization=object(properties,['id','plan_id','plan_revision',...Object.keys(fields),'revoked','created_at']);
 schemas.SettlementAutoWalletChannel=object({id:positive,provider:str(null,{enum:['alipay','wxpay']}),product:str(null,{enum:['alipay.fund.trans.toaccount.transfer','alipay.fund.trans.uni.transfer','wechat.v2.promotion.transfers']}),app_id:str('明确付款AppID。'),payer_masked:str('付款账户掩码；不返回私钥或完整账户。'),merchant_ids:{type:'array',minItems:1,uniqueItems:true,items:positive}},['id','provider','product','app_id','payer_masked','merchant_ids']);
 schemas.SettlementAutoAuthorizationRead=object({...properties,available:{type:'boolean',const:true},wallet_channels:{type:'array',items:ref('SettlementAutoWalletChannel')}},[...schemas.SettlementAutoAuthorization.required,'available','wallet_channels']);
 const envelope=name=>object({code:{type:'integer',const:0},message:str(),data:ref(name)},['code','message','data']);
 const parameter={name:'plan',in:'path',required:true,schema:positive};
 const security=[{AdminSession:[]}];
 const errors={'400':{description:'重复、未知或非规范字段、query、ID、时间、费用合同或限额。'},'401':{description:'原会话失效。'},'403':{description:'须本人super管理员，拒绝委托；修改须合法CSRF。'},'404':{description:'原计划或该计划的原授权不存在。'},'409':{description:'原修订、授权头、商户参与/收款、付款账户、产品或政策已变；不重试原确认。'},'422':{description:'无效有限授权。'},'428':{description:'签发始终要求本人独立资金确认，即使全局确认关闭。'},'503':{description:'未配置明确钱包产品或服务不可用。'}};
 const common='独立于原生成授权（generate_only或balance_threshold）；只覆盖其原run生成的原申请。每商户明确通道，微信AppID+MchID、支付宝AppID+SellerID及具体产品；拒绝弱身份和银行产品。单项/每次/每日毛额及每日条数有界，新授权不重置计划已消费额度。授权签发不启动执行器。balance_threshold通道目录只提供原生成授权固定的全局支付宝通道。';
 paths['/console/v1/admin/settlement-plans/{plan}/auto-authorization']={get:{operationId:'settlementAutoAuthorizationRead',summary:'本人super读取独立自动付款授权及已准入钱包通道',description:common+' 未签发时id为0，wallet_channels逐通道列出明确准入的商户ID。available表示该接口可用，不表示执行器运行。',security,parameters:[parameter],responses:{'200':{description:'原独立授权及通道掩码。',content:content(envelope('SettlementAutoAuthorizationRead'))},...errors}}};
 const csrf={name:'X-CSRF-Token',in:'header',required:true,schema:{type:'string',writeOnly:true}};
 paths['/console/v1/admin/settlement-plans/{plan}/auto-authorizations']={post:{operationId:'settlementAutoAuthorizationCreate',summary:'以独立资金确认签发有限自动付款授权',description:common+' 准确11个字段。expected_revision为当前计划修订，expected_authorization_id为原自动授权头（首次0），generation_authorization_id是当前原生成授权。资金摘要明确机构费用范围；绑定全部原收款/政策/产品/付款账户，任何变更使旧确认失效。',security,parameters:[parameter,csrf,{name:'X-Payment-Authorization',in:'header',required:true,schema:{type:'string',writeOnly:true},description:'独立settlement-auto-authorize用途，不能复用生成或人工出款授权。'}],requestBody:{required:true,content:content(ref('SettlementAutoAuthorizationWrite'))},responses:{'201':{description:'签发已提交；尚未发送付款或启动执行器。',content:content(envelope('SettlementAutoAuthorization'))},...errors}}};
 paths['/console/v1/admin/settlement-plans/{plan}/auto-authorizations/{authorization}/revoke']={post:{operationId:'settlementAutoAuthorizationRevoke',summary:'本人super撤销该原计划的独立自动付款授权',description:'严格空对象，不要求新资金确认。撤销仅阻止后续首次发送；已有claim/unknown不重发、不释放未知资金，按原TF查询恢复。锁等待后按实际数据库时间记录撤销。',security,parameters:[parameter,{name:'authorization',in:'path',required:true,schema:positive},csrf],requestBody:{required:true,content:content(object({},[]))},responses:{'200':{description:'已撤销或原来已撤销。'},...errors}}};
}
