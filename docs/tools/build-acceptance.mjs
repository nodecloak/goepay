// Generates a menu-level implementation index, not a full behavior acceptance claim.
import fs from 'node:fs';
const root = new URL('../../', import.meta.url);
const rows = JSON.parse(fs.readFileSync(new URL('web/capabilities.json', root), 'utf8'));
const seen = new Set();
const quote = value => `"${String(value ?? '').replaceAll('"', '""')}"`;
const output = [['feature_id','source_path','console_path','resource','capabilities','status','limitations','evidence']];
for (const item of rows) {
  const id = `${item.area.toUpperCase()}-${item.id}`;
  if (seen.has(id)) throw Error(`duplicate feature ${id}`);
  seen.add(id);
  if (!['partial','pending','deferred'].includes(item.status)) throw Error(`unsupported acceptance claim ${id}`);
  output.push([id,`epay_pro/app/data/menu_${item.area === 'merchant' ? 'user' : item.area}.json`,`/${item.area}${item.path}`,item.resource ?? '',item.capabilities.join(';'),item.status,item.limitations,'web/capabilities.json;docs/test-results.md']);
}
if (rows.length !== 82) throw Error(`expected 82 original menu leaves, found ${rows.length}`);
fs.writeFileSync(new URL('docs/baseline/acceptance-map.csv', root), output.map(row => row.map(quote).join(',')).join('\n')+'\n');
console.log(JSON.stringify({menu_leaves:rows.length,partial:rows.filter(x=>x.status==='partial').length,pending:rows.filter(x=>x.status==='pending').length,deferred:rows.filter(x=>x.status==='deferred').length,full_behavior_acceptance:false}));
