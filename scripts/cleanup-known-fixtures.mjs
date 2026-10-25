// Exact historical local browser fixtures only. Never deletes financial rows.
import fs from 'node:fs';
import assert from 'node:assert/strict';
const base='http://127.0.0.1:8181';
const config=Object.fromEntries(fs.readFileSync(new URL('../.env',import.meta.url),'utf8').split(/\r?\n/).filter(x=>x&&!x.startsWith('#')).map(x=>{const i=x.indexOf('=');return [x.slice(0,i),x.slice(i+1)]}));
const login=await fetch(base+'/console/v1/admin/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'admin',password:config.GOEPAY_SEED_PASSWORD})});
assert.equal(login.status,200,'local sandbox login unavailable');const session=(await login.json()).data;const cookie=login.headers.get('set-cookie').split(';')[0];
const api=async(path,method='GET',body)=>{const r=await fetch(base+'/console/v1/admin'+path,{method,headers:{Cookie:cookie,'Content-Type':'application/json','X-CSRF-Token':session.csrf_token},body:body===undefined?undefined:JSON.stringify(body)});const result=await r.json();assert.equal(r.status,200,`fixture operation rejected (${r.status})`);assert.equal(result.code,0);return result.data;};
assert.equal((await api('/dashboard')).sandbox,true,'local sandbox required');
let disabled=false,deleted=false;
const admins=await api('/resources/admin-users?q='+encodeURIComponent('live-operator-1792716414764'));
const admin=admins.items.find(x=>String(x.id)==='3'&&x.username==='live-operator-1792716414764');
if(admin?.enabled===true){await api('/resources/admin-users/3','PATCH',{enabled:false});disabled=true;}
const styles=await api('/resources/collection-styles?q='+encodeURIComponent('live-collection-1792716304200'));
const matching=styles.items.filter(x=>x.name==='live-collection-1792716304200');assert.ok(matching.length<=1,'ambiguous fixture; refuse cleanup');
if(matching[0]){await api('/resources/collection-styles/'+encodeURIComponent(String(matching[0].id)),'DELETE');deleted=true;}
await api('/logout','POST',{});
console.log(JSON.stringify({sandbox:true,exact_old_operator_disabled:disabled,exact_old_collection_style_deleted:deleted,financial_records_deleted:false}));
