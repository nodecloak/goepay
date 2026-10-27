export function installMerchantProfile198API({paths,schemas,object,str,ref,content}){
 schemas.MerchantOriginalProfile198=object({
  uid:{...str(),pattern:'^[1-9][0-9]{0,18}$',readOnly:true,description:'原商户ID，始终使用完整十进制字符串，取当前本人会话主体。'},
  addtime:{...str(),readOnly:true,description:'原注册时间展示字段，UTC+8 YYYY-MM-DD HH:mm:ss。没有原时间的旧导入账号为空，不返回导入时钟冒充。'},
  registration_time_known:{type:'boolean',readOnly:true},
  has_pwd:{type:'boolean',readOnly:true,description:'当前本人控制台账号是否已有登录密码，仅状态元数据；不包含hash、盐或恢复授权。未设置时不提供无验证的初始化能力。'},
  name:str(),email:{...str(),description:'优先返回已验证绑定；历史未验证展示资料不能授权找回。更换绑定使用account/email流程。'},pay:{type:'string',enum:['0','1','2'],readOnly:true}
 },['uid','addtime','registration_time_known','has_pwd','name','email','pay']);
 paths['/console/v1/merchant/account/profile']={get:{operationId:'merchantOriginalProfile198Read',tags:['account'],summary:'商户资料原UID和注册时间只读字段',description:'具体商户读取契约，旧通用role路径保留。uid只取认证主体，请求参数不能选择他人。新增只读字段不属于Profile PATCH输入。原生账号取不可变merchant出生时间；历史导入缺少原时间时明确未知。提供原始时间的新脱敏快照支持可选Merchant.created_at，要求明确时区RFC3339、非零且不晚于exported_at，不改写旧导入摘要或历史账本。',security:[{MerchantSession:[]}],parameters:[],responses:{'200':{description:'no-store，本人资料。',content:content(object({code:{type:'integer',const:0},message:str(),data:ref('MerchantOriginalProfile198')},['code','message','data']))},'401':{description:'登录会话无效。'},'403':{description:'商户身份不可用。'},'503':{description:'资料来源暂不可用，不使用他人或上一会话缓存代替。'}}}};
}
