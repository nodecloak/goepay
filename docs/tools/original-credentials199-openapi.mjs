export function installOriginalCredentials199API({paths,schemas,object,str,ref,content}) {
 schemas.MerchantMD5Rotation199Result=object({api_key:{...str(),readOnly:true,description:'仅确认提交后的本次响应返回。敏感值不得写入URL、日志或浏览器持久存储。'}},['api_key']);
 for(const role of ['merchant','admin']) {
  const rotation=role==='merchant';
  const security=[{[rotation?'MerchantSession':'AdminSession']:[]}];
  const errors={
   '400':{description:'JSON格式或字段错误。'},
   '401':{description:'请求入口会话已失效。'},
   '403':{description:'不是当前原始本人持久会话，委派、撤销、禁用、认证代次变化或等待后过期。'},
   '422':{description:'当前密码不正确、未设置登录密码或输入不满足要求；有效会话不会因确认密码错误而注销。'},
   '429':{description:'共享来源/账号来源15分钟预算或密码验证并发预算耗尽。',headers:{'Retry-After':{schema:{type:'integer'}}}},
   '503':{description:'来源不可用或结果无法确认；不返回新密钥，不自动重复修改。'}
  };
  paths[`/console/v1/${role}/account/password`]={post:{
   operationId:`${role}OriginalPassword199Change`,tags:['account'],summary:'本人密码修改并原子撤销旧会话',security,
   description:'保留current_password/new_password字段。仅原始本人持久会话；检查CSRF、认证代次、启用状态和锁等待后的原始到期时间。密码哈希、现有触发器认证代次恰好+1、删除所有本人旧会话及审计同一事务提交；末次业务写后再次检查到期。失败全回滚。密码错误422可在同一有效会话重试；不提供无原密码的首次初始化。成功需重新登录。',
   requestBody:{required:true,content:content(ref('PasswordChange'))},responses:{'200':{description:'no-store。密码和旧会话撤销已确认提交，须使用新密码登录。',content:content(object({code:{type:'integer',const:0},message:str(),data:{type:'null'}},['code','message','data']))},...errors}
  }};
  if(rotation)paths['/console/v1/merchant/account/api-key/rotate']={post:{
   operationId:'merchantOriginalMD5Key199Rotate',tags:['account'],summary:'原始本人确认密码后轮换MD5接入密钥',security,
   description:'保留current_password字段。复用既有加密原子MD5写入；授权完成后才生成新密钥，实际api_key写入后复查原会话到期时间，再确认事务提交。仅提交成功返回一次新密钥。认证代次、密码、MFA、费率和账本不变。未知结果核对接入信息，不自动重试。',
   requestBody:{required:true,content:content(ref('PasswordProof'))},responses:{'200':{description:'no-store。本次新密钥已确认提交。',content:content(object({code:{type:'integer',const:0},message:str(),data:ref('MerchantMD5Rotation199Result')},['code','message','data']))},...errors}
  }};
 }
}
