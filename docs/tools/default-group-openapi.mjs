// Groups accept the canonical ID 0 for the real, editable default group
// (internal/backoffice/handler.go); other resources keep positive IDs.
export function installDefaultGroupAPI({paths}) {
let count=0;
for(const [path,item]of Object.entries(paths)){
 if(!path.includes('/resources/')||!path.endsWith('/{id}'))continue;
 for(const verb of['get','patch','put','delete']){
  const operation=item[verb];if(!operation)continue;
  const description='用户组 groups 的规范ID字符串 0 是真实、可编辑的默认组；仅 groups 接受0，其它资源保持正ID。默认组不允许删除、改编号或改资源，服务层及数据库双重保护；未知基础归属仍需明确分配确认。普通组的角色、CSRF和精确运行通道校验同样适用。';
  if(!(operation.description||'').includes(description))operation.description=(operation.description||'')+' '+description;
  for(const p of operation.parameters||[])if(p.in==='path'&&p.name==='id'){
   const note='groups资源允许规范字符串0；其它资源必须正ID。';
   if(!(p.description||'').includes(note))p.description=(p.description||'')+' '+note;
   p.schema={type:'string',pattern:'^(0|[1-9][0-9]{0,18})$'};
  }
  count++;
 }
}
if(!count)throw Error('No generic resource ID operations found; preserve API specification');
}
