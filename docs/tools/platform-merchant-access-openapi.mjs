export function installPlatformMerchantAccessAPI({paths,schemas,object,str,ref,content}) {
  const flag={type:'string',enum:['0','1']}, publicFlag={type:'integer',enum:[0,1]}, fp=str('独立平台开关完整事实私有HMAC CAS；不是登录凭据。',{pattern:'^[a-f0-9]{64}$'});
  schemas.PlatformMerchantAccess=object({close_keylogin:{...flag,description:'原字段：0允许新的商户密钥登录，1关闭；缺省0。'},user_refund:{...flag,description:'原字段：1允许商户新的API及后台自助退款，0关闭；原SQL缺省1。'},fingerprint:fp,can_update:{type:'boolean',const:true}},['close_keylogin','user_refund','fingerprint','can_update']);
  schemas.PlatformMerchantAccessChange=object({close_keylogin:flag,user_refund:flag,expected_fingerprint:fp},['close_keylogin','user_refund','expected_fingerprint']);
  schemas.PublicMerchantAccess=object({close_keylogin:publicFlag,user_refund:publicFlag},['close_keylogin','user_refund']);
  const envelope=name=>object({code:{type:'integer',const:0},message:str(),data:ref(name)},['code','message','data']);
  const desc='全部响应no-store；拒query，包括空?。原网站信息tab密钥登录开关、Pro商户设置tab开启商户自助退款。两字段始终一起读取并用当前完整指纹保存，隐藏分类不得恢复默认。历史无配置沿用原默认；已提交配置的行/私有head丢失、篡改或恢复旧合法版本时拒绝降默认。通用settings仅可读原两字段，不允许创建、改名、修改、删除或访问私有head。';
  const errors={'400':{description:'query或严格JSON错误，包括重复/未知/缺失字段、非字符串0/1、非法指纹。'},'401':{description:'未登录。'},'403':{description:'非本人原super会话、代登录、撤销/降权/过期或CSRF不符。'},'409':{description:'CAS或完整性冲突；先刷新核对，不自动重试。'},'503':{description:'读取或提交无法确认；审计失败回滚。'}};
  paths['/console/v1/admin/settings/merchant-access']={
    get:{operationId:'platformMerchantAccessRead',tags:['settings'],summary:'读取平台原密钥登录与自助退款开关',description:desc,security:[{AdminSession:[]}],responses:{'200':{description:'严格四字段，字符串0/1。',content:content(envelope('PlatformMerchantAccess'))},...errors}},
    patch:{operationId:'platformMerchantAccessChange',tags:['settings'],summary:'原super CAS保存平台两开关',description:desc+' 沿用原全局settings事务advisory锁，持久super用户/会话、非秘密审计及最后数据库clock校验。',security:[{AdminSession:[]}],parameters:[{name:'X-CSRF-Token',in:'header',required:true,schema:{type:'string',minLength:43,maxLength:43}}],requestBody:{required:true,content:content(ref('PlatformMerchantAccessChange'))},responses:{'200':{description:'实际提交的两开关与新指纹。',content:content(envelope('PlatformMerchantAccess'))},...errors}},
  };
  paths['/console/v1/public/merchant-access']={get:{operationId:'publicMerchantAccessRead',tags:['account'],summary:'读取登录与个人页实际平台能力',description:desc+' 无凭据/用户ID/私有head；失败不能当已知开启或关闭。',security:[],responses:{'200':{description:'严格两字段数字0/1，供公开登录及商户个人控件显示。',content:content(envelope('PublicMerchantAccess'))},'400':errors['400'],'503':errors['503']}}};
  paths['/console/v1/merchant/key-login'].post.description+=' 平台close_keylogin必须为0，Prepare和Complete均检查当前平台完整性与同一授权快照。平台关闭不影响密码、已有会话或商户API签名支付。';
  paths['/console/v1/merchant/account/api-switches'].get.description+=' 个人九字段DTO不增加平台字段；公开merchant-access能力独立读取。个人refund=1/keylogin=1不能越过平台关闭。';
  schemas.MerchantAPISwitches.properties.refund.description+=' 实际新API退款还必须满足平台user_refund=1；个人此字段只控制API，不阻止平台允许的本人后台自助退款。';
}
