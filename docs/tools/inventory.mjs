// Read-only source inventory. Run: node docs/tools/inventory.mjs
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const output = path.join(root, 'docs/baseline');
fs.mkdirSync(output, { recursive: true });
const read = p => fs.readFileSync(path.join(root, p), 'utf8');
const list = p => fs.readdirSync(path.join(root, p), {withFileTypes:true}).flatMap(e => e.isDirectory() ? (['vendor','.git','node_modules'].includes(e.name) ? [] : list(`${p}/${e.name}`)) : [`${p}/${e.name}`]);
const csv = (name, rows, columns) => fs.writeFileSync(path.join(output, name), '\ufeff' + [columns, ...rows.map(r => columns.map(c => r[c] ?? ''))].map(row => row.map(v => '"'+String(v).replaceAll('"','""')+'"').join(',')).join('\n')+'\n');
const menus = [];
for (const area of ['admin','user']) {
  const source = `epay_pro/app/data/menu_${area}.json`;
  const walk = (items, parent='', titles=[]) => items.forEach(item => {
    const full = item.path.startsWith('/') ? item.path : `${parent}/${item.path}`;
    const title = [...titles, item.meta?.title ?? item.name];
    menus.push({id:`${area.toUpperCase()}-${item.name}`,area,title:title.join(' / '),path:full,raw_path:item.path,component:item.component,roles:(item.meta?.roles??[]).join(';'),hidden:!!item.meta?.isHide,leaf:!item.children?.length,source,status:'已定位菜单；业务验收未执行'});
    if(item.children) walk(item.children,full,title);
  });
  walk(JSON.parse(read(source)));
}
csv('menus.csv',menus,['id','area','title','path','raw_path','component','roles','hidden','leaf','source','status']);
const routes=[];
for(const area of ['admin','user','api','app']) {
  const source=`epay_pro/route/${area}.php`;
  const lines=read(source).split(/\r?\n/); let depth=0; const groups=[];
  lines.forEach((line,index)=>{
    const g=line.match(/Route::group\((?:'([^']*)'|(\$adminPath))/);
    if(g) groups.push({prefix:g[1]??'{admin_path}',depth:depth+1});
    const r=line.match(/Route::(get|post|any|put|delete|patch)\('([^']*)'\s*,\s*(?:'([^']*)'|(function))/);
    if(r) {
      const full='/'+[...groups.map(g=>g.prefix),r[2]].filter(Boolean).join('/').replace(/^\/+/, '');
      const target=r[3]??'closure';
      let controller=''; let encrypted='';
      if(target!=='closure') { const [cls]=target.split('/'); controller='epay_pro/app/controller/'+cls.split('.').map((s,i,a)=>i===a.length-1?s[0].toUpperCase()+s.slice(1):s).join('/')+'.php'; if(fs.existsSync(path.join(root,controller))) encrypted=read(controller).slice(0,250).includes("extension_loaded('swoole_loader')"); }
      routes.push({id:`${area.toUpperCase()}-R${String(routes.filter(x=>x.area===area).length+1).padStart(3,'0')}`,area,method:r[1].toUpperCase(),path:full,target,controller,encrypted,source,line:index+1,status:'路由已定位；请求响应及权限待契约验证'});
    }
    const stripped=line.replace(/'(?:\\.|[^'\\])*'|"(?:\\.|[^"\\])*"/g,'').replace(/\/\/.*$/,'');
    depth+=(stripped.match(/\{/g)||[]).length-(stripped.match(/\}/g)||[]).length;
    while(groups.length && groups.at(-1).depth>depth) groups.pop();
  });
  const declarations=(read(source).match(/Route::(?:get|post|any|put|delete|patch)\(/g)||[]).length;
  if(routes.filter(x=>x.area===area).length!==declarations) throw Error(`Route count mismatch: ${source}`);
}
csv('routes.csv',routes,['id','area','method','path','target','controller','encrypted','source','line','status']);
const files=[...list('epay_pro'),...list('epay_release')];
const protectedFiles=files.filter(p=>p.endsWith('.php') && read(p).slice(0,250).includes("extension_loaded('swoole_loader')")).map(source=>({source,bytes:fs.statSync(path.join(root,source)).size,status:'Swoole Loader；未解密或执行'}));
csv('encrypted-files.csv',protectedFiles,['source','bytes','status']);
const tables=[];
for(const source of ['epay_pro/app/sql/install.sql','epay_release/install/install.sql']) {
  const text=read(source);
  for(const match of text.matchAll(/create\s+table\s+(?:if\s+not\s+exists\s+)?`([^`]+)`\s*\(([\s\S]*?)\)\s*ENGINE/gi)) {
    const columns=[...match[2].matchAll(/^\s*`([^`]+)`\s+([^\r\n]+)/gm)];
    tables.push({edition:source.includes('epay_pro')?'pro':'release',table:match[1],columns:columns.map(x=>`${x[1]} ${x[2]}`).join(' | '),source,line:text.slice(0,match.index).split('\n').length});
  }
}
csv('tables.csv',tables,['edition','table','columns','source','line']);
const plugins=[];
for(const source of files.filter(p=>/^epay_release\/plugins\/[^/]+\/[^/]+_plugin\.php$/.test(p))) {
 const t=read(source); const encrypted=t.slice(0,250).includes("extension_loaded('swoole_loader')");
 plugins.push({edition:'release',name:source.split('/')[2],methods:encrypted?'不可静态读取':[...t.matchAll(/(?:public\s+)?static\s+(?:public\s+)?function\s+(\w+)\s*\(/g)].map(m=>m[1]).join(';'),source,encrypted,status:'可参考旧版；不代表 Pro 插件已覆盖'});
}
for(const source of files.filter(p=>/^epay_pro\/plugins\/[^/]+\/[^/]+\/info\.json$/.test(p))) {
 const info=JSON.parse(read(source)); plugins.push({edition:'pro',name:source.split('/').slice(2,4).join('/'),methods:'',source,encrypted:false,status:info.name??'已提供元数据'});
}
csv('plugins.csv',plugins,['edition','name','methods','source','encrypted','status']);
const docs=files.filter(p=>p.startsWith('epay_pro/app/view/doc/') && p.endsWith('.html')).map(source=>{
 const t=read(source); const body=t.match(/<h1\b[\s\S]*?(?=<div id="mask")/)?.[0]??'';
 const text=body.replace(/<script\b[\s\S]*?<\/script>/g,'').replace(/<\/(p|tr|h1|h2|h3|blockquote|pre)>/g,'\n').replace(/<\/t[dh]>/g,' | ').replace(/<[^>]+>/g,'');
 return {source,text};
});
fs.writeFileSync(path.join(output,'api-docs-extracted.txt'),docs.map(x=>`SOURCE: ${x.source}\n${x.text}`).join('\n\n'));
const hashes=files.filter(p=>/\/route\/|\/data\/menu_|\/sql\/|\/install\/.*\.sql$|\/view\/doc\/|\/files\/SDK(?:_2\.0)?\.zip$/.test(p)).map(source=>({source,sha256:crypto.createHash('sha256').update(fs.readFileSync(path.join(root,source))).digest('hex')}));
csv('source-hashes.csv',hashes,['source','sha256']);
const index=['# 完整菜单与功能验收索引','','由 `docs/tools/inventory.mjs` 从当前源码生成；这些条目均为已定位，未进行实现和运行验收。','','菜单之外的登录、授权、弹窗、任务、插件动态功能必须联合 routes.csv 和总体设计验收。路由中的 operation 等分发动作还需要继续拆分子操作。',''];
for(const area of ['admin','user']) {
 index.push(`## ${area==='admin'?'管理后台':'用户后台'}页面`,'','| ID | 菜单层级 | 完整路径 | 隐藏 | 角色 |','|---|---|---|---|---|');
 for(const m of menus.filter(m=>m.area===area&&m.leaf)) index.push(`| ${m.id} | ${m.title} | ${m.path} | ${m.hidden?'是':'否'} | ${m.roles||'依后端身份/配置'} |`);
 index.push('');
}
for(const area of ['admin','user','api','app']) {
 index.push(`## ${area} 路由功能分组`,'','动作名称是原代码证据；具体请求、权限和业务分支仍需契约样本。每个动作按 routes.csv 中的ID追踪。','','| 控制器 | 动作 |','|---|---|');
 const targets=[...new Set(routes.filter(r=>r.area===area).map(r=>r.target.split('/')[0]))];
 for(const target of targets) index.push(`| ${target} | ${routes.filter(r=>r.area===area&&r.target.split('/')[0]===target).map(r=>r.target.split('/')[1]??r.path).join('、')} |`);
 index.push('');
}
index.push('## Release 支付插件入口','','下列60个入口全部保留在盘点范围，不据此推定Pro插件覆盖；逐个补充能力与差异测试。','',plugins.filter(p=>p.edition==='release').map(p=>p.name).join('、'),'');
fs.writeFileSync(path.join(output,'feature-index.md'),index.join('\n'));
const summary={menus:Object.fromEntries(['admin','user'].map(a=>[a,{nodes:menus.filter(m=>m.area===a).length,leaves:menus.filter(m=>m.area===a&&m.leaf).length,hiddenLeaves:menus.filter(m=>m.area===a&&m.leaf&&m.hidden).length}])),routes:Object.fromEntries(['admin','user','api','app'].map(a=>[a,routes.filter(r=>r.area===a).length])),encrypted:Object.fromEntries(['epay_pro','epay_release'].map(a=>[a,protectedFiles.filter(p=>p.source.startsWith(a+'/')).length])),tables:Object.fromEntries(['pro','release'].map(a=>[a,tables.filter(t=>t.edition===a).length])),plugins:plugins.length,releasePlugins:plugins.filter(p=>p.edition==='release').length,documents:docs.length};
fs.writeFileSync(path.join(output,'summary.json'),JSON.stringify(summary,null,2)+'\n');
console.log(JSON.stringify(summary,null,2));
