import fs from 'node:fs';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
import vm from 'node:vm';

const root=path.resolve(new URL('../../',import.meta.url).pathname.replace(/^\/([A-Za-z]:)/,'$1'));
const read=p=>fs.readFileSync(path.join(root,p),'utf8').replace(/^\uFEFF/,'');
const hash=b=>createHash('sha256').update(b).digest('hex');
const inventory=JSON.parse(read('docs/design/pro-console-field-inventory.json'));
const indexed=new Map(inventory.files.map(f=>[f.file.replaceAll('\\','/'),f]));
const bindings={admin:new Map(),merchant:new Map()};
for(const [area,entry] of [['admin','index-IIq1IliX.js'],['merchant','index-_K93c9Ev.js']]){
 const body=read('epay_pro/public/static/assets/'+entry);
 const regex=/"\.\.\/\.\.\/views\/([^"\n]+)":\(\)=>[A-Za-z_$][\w$]*\(\(\)=>import\("\.\/([^"\n]+)"\)/g;
 for(const match of body.matchAll(regex))bindings[area].set(match[1], 'epay_pro/public/static/assets/'+match[2]);
 if(!bindings[area].size)throw Error('Original entry module map not found: '+area);
}
const require=createRequire(path.join(root,'web/package.json'));
const esbuild=require('esbuild');
const own=esbuild.buildSync({stdin:{contents:'export {viewKind} from "./web/src/menu.ts"; export {resourceFor} from "./web/src/resources.ts"; export {reportFor} from "./web/src/reports.ts"; export {workflowFor} from "./web/src/workflows.ts";',resolveDir:root,loader:'ts'},bundle:true,platform:'node',format:'cjs',write:false}).outputFiles[0].text;
const module={exports:{}};vm.runInNewContext(own,{module,exports:module.exports,require});
const current=module.exports,capabilities=JSON.parse(read('web/capabilities.json'));
const action=/^(?:新增|添加|编辑|修改|删除|详情|查看|查询|搜索|批量|导入|导出|审核|通过|拒绝|启用|禁用|冻结|解冻|退款|补单|手动|API退款|改已|改未|重新|确认|结算|代付|转账|充值|扣款|重置|生成|复制|下载|上传|测试|保存|提交|取消|关闭|开通|绑定|解绑|登录|设置|更改|清空|刷新|发起|同步|恢复|撤销|终止|拉黑|移除|清理)/;
const componentFiles=fs.readdirSync(path.join(root,'web/src/components')).filter(f=>f.endsWith('.vue'));
const norm=s=>s.replace(/[-_]/g,'').toLowerCase().replace(/(?:page|panel)\.vue$/,'');
const pages=[];
function walk(area,nodes,parent=''){
 for(const node of nodes){
  const route=node.path.startsWith('/')?node.path:parent+'/'+node.path;
  if(node.children?.length){walk(area,node.children,route);continue;}
  const component=String(node.component||'').replace(/^\//,'').replace(/\.vue$/,'');
  const candidates=[component+'.vue',component+'/index.vue'];
  const source=candidates.map(c=>bindings[area].get(c)).find(Boolean)||null;
  let original=null;
  if(source){const bytes=fs.readFileSync(path.join(root,source)),known=indexed.get(source);if(known&&known.sha256!==hash(bytes))throw Error('Original module changed: '+source);
   const labels=known?.labels||[];
   original={module:source,sha256:hash(bytes),columns_and_form_labels:known?.columns||[],literal_labels:labels,action_label_candidates:[...new Set(labels.filter(s=>s.length<=24&&action.test(s)&&!/(?:成功|失败|错误|正在|不存在|不能为空|请先|不能|不支持|尚未)/.test(s)))],local_endpoint_literals:known?.endpoints||[],literal_route_links:known?.routes||[],extraction:known?'existing Babel static inventory':'module mapped; not yet included in prior label inventory'};
  }
  const kind=current.viewKind(area,route),resource=current.resourceFor(area,route)||null;
  const dedicated=componentFiles.find(f=>norm(f)===norm(kind));
  const currentSources=kind==='resource'?['web/src/components/ResourcePage.vue','web/src/components/ResourceFields.vue']:kind==='report'?['web/src/components/ReportPage.vue']:kind==='workflow'?['web/src/components/WorkflowPage.vue']:dedicated?['web/src/components/'+dedicated]:['web/src/App.vue','web/src/components/DataTable.vue'];
  if(area==='admin'&&route==='/trade/order')currentSources.push('web/src/components/OrderActionMenu.vue','web/src/components/AdminOrderActionDialog.vue','web/src/order-actions.ts');
  const declared=capabilities.find(c=>c.area===area&&c.path===route);
  pages.push({area,path:route,title:node.meta?.title||node.name,original_component:node.component,original,current:{kind,resource,report:current.reportFor(area,route)||null,workflow:current.workflowFor(area,route)||null,sources:currentSources.map(file=>({file,sha256:hash(fs.readFileSync(path.join(root,file)))})),declared_capabilities:declared?.capabilities||[],deferred:declared?.status==='deferred'},review_scope:'original menu/component/module and current source binding; candidates need per-action/DOM validation',behavior_parity:'not established by static inventory'});
 }
}
walk('admin',JSON.parse(read('epay_pro/app/data/menu_admin.json')));
walk('merchant',JSON.parse(read('epay_pro/app/data/menu_user.json')));
if(pages.length!==82)throw Error('Expected all82 original leaf pages, got '+pages.length);
const result={date:'2026-10-25',scope:'Both complete original backoffices; order screenshots are examples only',production_access:false,real_funds_operations:false,original_entry_maps:{admin:bindings.admin.size,merchant:bindings.merchant.size},pages:pages.length,mapped_original_modules:pages.filter(p=>p.original).length,remaining_source_bindings:pages.filter(p=>!p.original).map(p=>({area:p.area,path:p.path,component:p.original_component})),deferred:pages.filter(p=>p.current.deferred).length,static_inventory_is_not_ux_or_business_acceptance:true,entries:pages};
const dest='docs/verification/pro-ux-all-pages-2026-10-25';
fs.writeFileSync(path.join(root,dest+'.json'),JSON.stringify(result,null,2)+'\n');
const esc=s=>String(s??'').replaceAll('|','\\|').replaceAll('\n',' ');
const lines=['# 双后台全部页面UX复核来源清单','','用户澄清：订单只是例子，两个后台全部页面必须review。本表逐页绑定原菜单、原视图/编译模块和当前真实路由组件；静态来源核对不等于按钮已实现或交互验收通过。完整候选动作、字段、状态文本和来源SHA保存在同名JSON。','','原源码全部在本地读取，未访问生产或发起资金交易。原控制器加密的行为不能从按钮文案推定。','','| 后台/页面 | 原组件模块 | 当前实际页面 | 原动作标签候选（完整见JSON） |','|---|---|---|---|'];
for(const p of pages)lines.push(`| ${p.area}${p.path} · ${esc(p.title)} | ${p.original?path.basename(p.original.module):'来源未绑定：'+esc(p.original_component)} | ${esc(p.current.kind)}${p.current.resource?' / '+esc(p.current.resource):''}${p.current.deferred?'（用户指定暂缓）':''} | ${esc(p.original?.action_label_candidates.join('、')||'需检查导入组件；不推断无操作')} |`);
lines.push('','## 当前复核约束','','- 表内动作是静态候选，必须再核对按钮的状态/权限条件和真实处理方法；同义文案不自动判断缺失。','- 通用资源页的详情/编辑/删除不代表原商户/通道/设置的所有专用动作已实现。','- 本表绑定当前工作源码；实际部署、测试与剩余差异以对应验收记录和全页面整改表为准，静态来源不冒充部署后验收。','- 其它业务插件按用户指定暂缓，其余页面不能因为缺少插件而自动排除。','');
fs.writeFileSync(path.join(root,dest+'.md'),lines.join('\n'));
console.log(JSON.stringify({...result,entries:undefined}));
