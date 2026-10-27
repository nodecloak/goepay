export function installNotificationReturn200API({paths,schemas,object,str,ref,content}) {
 schemas.OrderSynchronousReturn200=object({url:{...str(),format:'uri',readOnly:true,maxLength:16384,description:'以原订单和当前签名材料生成的商户同步返回地址。仅当次no-store响应返回；不得渲染到日志、URL参数或浏览器持久存储。'}},['url']);
 for(const role of ['admin','merchant'])paths[`/console/v1/${role}/orders/{trade}/return-notification`]={post:{
  operationId:`${role}SynchronousOrderReturn200`,tags:['orders'],summary:'订单详情重新通知（同步）',
  description:'保留原异步resend-notification，此动作仅生成并在新窗口打开已保存的return_url，不请求通道或商户地址，不创建异步任务或更改订单/通知/账本。仅原始持久商户本人或原始super/operator管理员，CSRF、锁内归属/状态/密钥及审计后会话时限再次核验；要求可信已支付状态和有效存储地址。严格空JSON，不接受查询参数/自报URL。详情新增can_return_notification布尔观察，动作仍重新校验，不以该布尔代替权限。新窗口必须由用户点击预开、切上下文时关闭，成功导航前清opener，失败或未知不自动重试。',
  security:[{[role==='admin'?'AdminSession':'MerchantSession']:[]}],
  parameters:[{name:'trade',in:'path',required:true,schema:str(null,{minLength:1,maxLength:64})},{name:'X-CSRF-Token',in:'header',required:true,schema:str(null,{writeOnly:true})}],
  requestBody:{required:true,content:content(ref('Empty'))},responses:{
   '200':{description:'no-store。审计已提交，仅生成同步返回地址；不表示商户已收到或处理成功。',content:content(object({code:{type:'integer',const:0},message:str(),data:ref('OrderSynchronousReturn200')},['code','message','data']))},
   '400':{description:'不是严格空JSON或含查询参数。'},'401':{description:'入口会话无效。'},'403':{description:'缺少原始持久会话权限、CSRF无效或等待后已过期。'},'404':{description:'订单不存在或不属于本人。'},'405':{description:'必须POST。'},'409':{description:'未支付或没有有效的原同步地址。'},'503':{description:'签名来源、审计或提交无法确认，未返回地址。'}
  }
 }};
}
