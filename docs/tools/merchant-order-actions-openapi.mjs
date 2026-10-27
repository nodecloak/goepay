export function installMerchantOrderActionsAPI({paths,schemas,object,str,ref,content}) {
 schemas.MerchantOrderCapabilities=object({query:{type:'boolean'},notify:{type:'boolean'},refund:{type:'boolean'},print:{type:'boolean'}},['query','notify','refund','print']);
 schemas.MerchantOrderListRow={type:'object',properties:{id:str('完整正int64字符串。'),trade_no:str('平台原订单号。'),amount:str('整数分字符串。'),refunded_amount:str('已成功退款整数分字符串。'),merchant_actions:ref('MerchantOrderCapabilities')},required:['id','trade_no','merchant_actions'],additionalProperties:true};
 const base=paths['/console/v1/{role}/orders']?.get;
 if(base)paths['/console/v1/merchant/orders']={get:{...base,operationId:'merchantOrdersWithCapabilities',security:[{MerchantSession:[]}],summary:'读取本商户订单及已验证条件动作',description:'一次本人范围的批量查询和原用途证明生成merchant_actions，GET不外呼、不写数据。query只在待付且原适配器可查询；notify只在已付普通payment且原通知URL有效；refund还受剩余金额、未知退款、冻结来源/资金和真实适配器能力限制；print受已付/原会话与打印服务限制。菜单选择后仍须读取新鲜详情和服务端权限，不能以列表布尔代替写入授权。未知能力false；商户不能调用管理员状态修改/手工退款/冻结动作。',parameters:(base.parameters||[]).filter(p=>!(p.in==='path'&&p.name==='role')),responses:{...base.responses,'200':{description:'原订单字段及四个布尔能力；所有权不由请求自报。',content:content(object({code:{type:'integer',const:0},message:str(),data:{type:'object',properties:{items:{type:'array',items:ref('MerchantOrderListRow')},total:{type:'integer'},page:{type:'integer'},limit:{type:'integer'}},required:['items','total','page','limit'],additionalProperties:true}},['code','message','data']))}}}};
 for(const kind of ['ledger','refunds']){
  const get=paths['/console/v1/{role}/'+kind]?.get;if(!get)continue;
  get.parameters||=[];if(!get.parameters.some(p=>p.name==='trade_no'&&p.in==='query'))get.parameters.push({name:'trade_no',in:'query',schema:{type:'string',minLength:1,maxLength:64},description:'原订单精确筛选；拒重复值、NUL/CR/LF及冲突q/search_field。仍受本账户/角色范围限制，不暴露他人订单存在性。CSV共用此谓词。'});
  get.description=(get.description||'')+' trade_no为真实关联精确筛选；ledger按不可变订单与真实journal FK/已知writer精确canonical引用匹配，原物理账户历史余额window先算后筛选，付款/退款/冻结/已实现分账和返现只显示本人可见分录。没有真实订单关联的会员奖励不猜。无本人分录的他人订单与不存在订单均为空。';
 }
}
