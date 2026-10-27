export function installMerchantPersonalSettingsAPI({paths,schemas,object,str,ref,content}){
 const fingerprint=str('绑定本人ID、启用、config_revision及原三个个人字段的HMAC；不是原profile或API凭据。',{pattern:'^[a-f0-9]{64}$'});
 const money=str('元金额；空串表示本人不额外限制，0.00同样不增加限制；仍服从平台、有效用户组和实际通道限制。金额最多int64分，禁止转浮点。',{maxLength:20,pattern:'^(?:|(?:0|[1-9][0-9]*)[.][0-9]{2})$'});
 const qq=str('联系QQ，空或5–20位十进制数字。',{maxLength:20,pattern:'^(?:|[1-9][0-9]{4,19})$'});
 schemas.MerchantPersonalSettings=object({merchant_id:str('商户本人完整正int64字符串。',{pattern:'^[1-9][0-9]{0,18}$'}),qq,pay_minmoney:money,pay_maxmoney:money,bounds_valid:{type:'boolean',description:'最大非0时应>=最小；旧冲突区间返回false，允许本人明确修正，而非假称不受限制。'},fingerprint,can_update:{type:'boolean',const:true}},['merchant_id','qq','pay_minmoney','pay_maxmoney','bounds_valid','fingerprint','can_update']);
 const inputMoney=str('规范非负元字符串，最多两位小数，空表示不额外限制；服务端用整数分检查溢出及区间。保存后规范为两位小数。',{maxLength:21,pattern:'^(?:|(?:0|[1-9][0-9]*)(?:[.][0-9]{1,2})?)$'});
 schemas.MerchantPersonalSettingsChange=object({qq,pay_minmoney:inputMoney,pay_maxmoney:inputMoney,expected_fingerprint:fingerprint},['qq','pay_minmoney','pay_maxmoney','expected_fingerprint']);
 const envelope=object({code:{type:'integer',const:0},message:str(),data:ref('MerchantPersonalSettings')},['code','message','data']);
 const common={tags:['account'],security:[{MerchantSession:[]}],description:'仅商户本人原持久会话，不接受query、merchant_id或委托身份。所有响应no-store；不返回email/phone/profile/密钥/支付账户。仅三个原个人字段，不包含未接入的个人API开关、密钥登录或买家加费。个人支付限制由原新订单/报价出生引擎实际消费；旧精确订单重放不重价，个人更宽限制不能放宽组/平台/通道规则。'};
 const errors={'400':{description:'query、未知/缺少/重复/错类型字段或不规范指纹。'},'401':{description:'原会话已失效。'},'403':{description:'非本人原持久会话、委托、撤销或CSRF错误。'},'409':{description:'原字段或revision已变；须显式重新读取核对，不自动刷新重试。'},'422':{description:'QQ、金额、精度、范围或上下限非法。'},'503':{description:'原个人资料/存储或提交无法确认，未返回部分可信状态。'}};
 paths['/console/v1/merchant/account/personal-settings']={get:{...common,operationId:'merchantPersonalSettingsRead',summary:'读取本人联系QQ与个人单笔上下限',responses:{'200':{description:'完整原目标和规范金额；旧矛盾区间允许本人修正。',content:content(envelope)},...errors}},patch:{...common,operationId:'merchantPersonalSettingsChange',summary:'保存本人原联系QQ和单笔上下限',description:common.description+' merchant→用户→原session持久锁、原指纹CAS；仅合并三个字段并增revision，不覆盖无关profile或凭据；非秘密字段名审计失败全回滚，最后写后按数据库clock重新核验会话。成功返回新指纹，不接受旧指纹自动重试。',parameters:[{name:'X-CSRF-Token',in:'header',required:true,schema:{type:'string',minLength:43,maxLength:43}}],requestBody:{required:true,content:content(ref('MerchantPersonalSettingsChange'))},responses:{'200':{description:'实际保存并提交的新状态。',content:content(envelope)},...errors}}};
 // Console transport only. Do not alter the internal identity.User JSON tags
 // or any durable authority/proof encoding while fixing JavaScript precision.
 const ownerID=str('完整正int64编号字符串；不得先转JavaScript Number。',{pattern:'^[1-9][0-9]{0,18}$'});
 schemas.ConsoleUser=object({id:ownerID,name:str(),role:{type:'string',enum:['admin','merchant']},admin_class:{type:'string',enum:['super','operator']}},['id','name','role']);
 const csrf=str('当前角色会话CSRF，保持内存，不写本地存储。',{minLength:43,maxLength:43});
 schemas.ConsoleLoginData=object({user:ref('ConsoleUser'),csrf_token:csrf},['user','csrf_token']);
 schemas.ConsoleSessionData=object({user:ref('ConsoleUser'),csrf_token:csrf,delegated_by:{oneOf:[{type:'null'},object({id:ownerID,name:str()},['id','name'])]}},['user','csrf_token','delegated_by']);
 for(const [route,method,schema]of [['/console/v1/{role}/login','post','ConsoleLoginData'],['/console/v1/{role}/session','get','ConsoleSessionData']]){
  const operation=paths[route]?.[method];if(!operation)throw Error('Original console identity operation missing');
  operation.responses['200']={description:'原角色Cookie/CSRF与身份语义保持；user.id始终为完整字符串，不返回username/auth_version或凭据，no-store。',content:content(object({code:{type:'integer',const:0},message:str(),data:ref(schema)},['code','message','data']))};
 }
}
