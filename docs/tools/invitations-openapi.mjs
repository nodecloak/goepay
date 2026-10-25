export function installInvitationsAPI({add,paths,schemas,object,str}) {
 const request_id=str('UUIDv4稳定原请求号。超时后使用同一请求与数量/范围核对，不生成新请求号。',{pattern:'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'});
 schemas.InvitationGenerate=object({request_id,count:{type:'integer',minimum:1,maximum:1000}},['request_id','count']);
 schemas.InvitationClear=object({request_id,scope:{type:'string',enum:['all','used']},confirm:{type:'boolean',const:true}},['request_id','scope','confirm']);
 add('/invitation-codes','get','读取邀请码、使用者与真实注册使用记录',{list:true,description:'仅本人管理员；search仅按邀请码包含匹配；status为used/unused或空。返回items/id/code/status/enabled/max_uses/use_count/created_at/used_at/used_by/uses以及total/page/limit。使用状态源自不可变注册证明；多次使用旧码保留次数和全部使用记录。归档码不在列表中。'});
 add('/invitation-codes/generate','post','原子生成1–1000个一次性随机邀请码',{body:'InvitationGenerate',description:'仅本人管理员+CSRF。新码160位随机，每个max_uses=1。原请求号相同数量返回原items且replayed=true；改数量409。全部记录及批次审计同事务，失败无部分生成。'});
 add('/invitation-codes/clear','post','明确确认后分批归档邀请码',{body:'InvitationClear',description:'仅本人管理员+CSRF。每个原请求最多1000条，返回archived/remaining/replayed，不自动处理下一批。清空为enabled=false、archived=true；不删除注册使用证明。原码不可恢复或重新分配。同号同范围返回原结果；改范围409。通用资源DELETE也执行同样归档，通用列表排除归档，更新归档码冲突。'});
 for(const suffix of['/invitation-codes','/invitation-codes/generate','/invitation-codes/clear']){
  const from='/console/v1/{role}'+suffix,to='/console/v1/admin'+suffix;
  paths[to]=paths[from];delete paths[from];for(const op of Object.values(paths[to])){op.security=[{AdminSession:[]}];op.parameters=op.parameters.filter(p=>p.name!=='role');}
 }
 paths['/console/v1/admin/invitation-codes'].get.parameters.push({name:'status',in:'query',schema:{type:'string',enum:['','used','unused']}});
}
