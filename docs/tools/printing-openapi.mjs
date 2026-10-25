export function installPrintingAPI({add,paths,schemas,object,str,id,ref,content}) {
 const decimal=str('规范非负十进制字符串。',{pattern:'^(0|[1-9][0-9]*)$'});
 const boolean={type:'boolean'};
 const integer=(min,max)=>({type:'integer',minimum:min,maximum:max});
 const fields=(properties)=>object(properties,Object.keys(properties));
 schemas.PrintingPlatformInput=fields({expected_revision:decimal,enabled:boolean,provider:str(null,{enum:['local_fake','excelsecu']}),credential_action:str(null,{enum:['keep','replace','clear']}),app_id:str('仅replace时明文提交；keep/clear必须显式空串。',{writeOnly:true,maxLength:128}),secret:str('只写不回显；清除须先停用。',{writeOnly:true,maxLength:512})});
 schemas.PrintingMerchantInput=fields({expected_revision:decimal,mode:integer(0,2),copies:integer(1,5),voice:boolean,device_action:str(null,{enum:['keep','replace','clear']}),device_id:str('设备只写；keep/clear传空串；clear需mode0。',{writeOnly:true,maxLength:128})});
 schemas.PrintingManualInput=fields({external_no:str('原幂等号；丢响应必须同号同订单核对。',{pattern:'^[A-Za-z0-9_-]{1,64}$'}),previous_job_id:decimal,acknowledge_duplicate:boolean});
 schemas.PrintingRecoverInput=fields({expected_task_id:id});
 schemas.PrintingPlatform=fields({id:decimal,revision:decimal,enabled:boolean,provider:str(),app_id_masked:str(),secret_present:boolean,external_execution_available:{type:'boolean',description:'此二进制具备固定Excelsecu连接器。不是worker心跳、已启用外部执行或实体打印完成证明；外部worker须独立启用，默认关闭。'}});
 schemas.PrintingMerchant=fields({id:decimal,merchant_id:id,revision:decimal,mode:integer(0,2),copies:integer(1,5),voice:boolean,device_masked:str(),device_present:boolean});
 schemas.PrintReceipt=fields({trade_no:str(),merchant_name:str(),name:str(),amount:decimal,type:str(),paid_at:str(),remark:str()});
 schemas.PrintJob=object({id,merchant_id:id,order_id:id,previous_job_id:decimal,trade_no:str(),source:str(null,{enum:['auto','manual']}),external_no:str(),request_no:str(),state:str('accepted仅网关受理；不证明纸张打印完成。'),reason:str(),provider:str(),device_masked:str(),copies:integer(1,5),voice:boolean,created_at:str(null,{format:'date-time'}),receipt:ref('PrintReceipt'),task_id:decimal,task_state:str(),can_recover:boolean},['id','trade_no','request_no','state','provider','receipt']);
 const envelope=(schema)=>object({code:{type:'integer'},message:str(),data:schema},['code','message','data']);
 function endpoint(role,path,method,summary,options={}){
  add(path,method,summary,options);
  const original='/console/v1/{role}'+path;
  const op=paths[original][method];delete paths[original][method];if(!Object.keys(paths[original]).length)delete paths[original];
  (paths['/console/v1/'+role+path]??={})[method]=op;
  op.parameters=op.parameters.filter(p=>p.name!=='role');op.security=[{[role==='admin'?'AdminSession':'MerchantSession']:[]}];
  return op;
 }
 function result(op,schema){op.responses['200'].content=content(envelope(schema));}
 const scope='仅本人且非代登录会话；不接受merchant_id/query覆盖。打印配置/意图与支付资金分离，不触发外部付款。';
 for(const [role,path,input,output] of [['admin','/printing/settings','PrintingPlatformInput','PrintingPlatform'],['merchant','/account/printing','PrintingMerchantInput','PrintingMerchant']]){
  result(endpoint(role,path,'get','读取掩码打印配置',{description:scope+' 平台限super；默认未启用。'}),ref(output));
  result(endpoint(role,path,'put','CAS替换打印配置',{body:input,description:scope+' 全字段显式提交；密钥/设备AEAD保存，仅返回掩码，不写审计明文。保存不立即打印，不补打历史。excelsecu使用固定HTTPS连接器和独立默认关闭worker；不接受自定义地址，网关受理不表示纸张完成。'}),ref(output));
 }
 result(endpoint('merchant','/orders/{trade}/receipt','get','读取本人订单可信支付收据',{description:scope+' 必须存在原可信支付HMAC事实；未支付/历史缺事实不能凭status推断。金额为原支付毛额分字符串，不含买家标识或支付凭据。'}),ref('PrintReceipt'));
 const create=endpoint('merchant','/orders/{trade}/print-jobs','post','显式创建或返回原手工打印意图',{body:'PrintingManualInput',uncertain:true,description:scope+' 先原external_no幂等读取；未终态禁止隐式新号。unknown只在后端明确引用原job并acknowledge_duplicate时另建有限人工意图，普通页面不提供unknown重发。无HTTP dispatch端点。'});
 create.responses['202']={description:'原打印意图已记录；state仍须查询，queued尚未发送，accepted也不证明实体打印完成。',content:content(envelope(ref('PrintJob')))};
 for(const path of ['/print-jobs','/orders/{trade}/print-jobs']){
  const op=endpoint('merchant',path,'get','按本人范围游标读取打印记录',{description:scope+' 订单路径在数据库中按原orderID筛选，不从全商户页过滤。next_cursor空表示结束，满页可再读空页。'});
  op.parameters.push({name:'after_id',in:'query',schema:{...decimal,default:'0'}},{name:'limit',in:'query',schema:{...integer(1,100),default:20}});
  result(op,fields({items:{type:'array',items:ref('PrintJob')},next_cursor:str()}));
 }
 result(endpoint('merchant','/print-jobs/{id}','get','读取本人原打印请求状态',{description:scope+' sending/unknown不可自动重发。local_fake受理仅为本地模拟。'}),ref('PrintJob'));
 result(endpoint('merchant','/print-jobs/{id}/recover','post','人工恢复原未发送dead任务',{body:'PrintingRecoverInput',description:scope+' 仅can_recover=true、原expected_task_id、无任何业务claim；原dead任务不改写，恢复证明与审计同事务。原task唯一幂等，丢响应保留同expected_task_id；不换PJ/不新增打印意图，unknown/sending拒绝。'}),ref('PrintJob'));
}
