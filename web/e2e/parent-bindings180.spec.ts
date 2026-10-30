import {test,expect,type Page} from '@playwright/test';
import actual from './parent-bindings180-http-fixture.json' with {type:'json'};
import type {ParentChannel} from '../src/parent-channels';
async function fixture(page:Page,options:{uncertain?:boolean;operator?:boolean;stale?:boolean}={}){
 const writes:{path:string;body:Record<string,unknown>}[]=[],reads:string[]=[];let state=structuredClone(actual.bound) as ParentChannel;
 const catalog=structuredClone(actual.catalog);if(options.stale&&state.account?.weixin_binding_state?.web){state.account.weixin_binding_state.web.current=false;catalog.items[0].revision='2';catalog.items[0].appid='synthetic-web-B';catalog.fingerprint='b'.repeat(64)}
 await page.route('**/console/v1/**',async route=>{const req=route.request(),url=new URL(req.url()),path=url.pathname;let data:unknown={items:[],total:0};if(req.method()==='GET')reads.push(path);else writes.push({path,body:req.postDataJSON()});
  if(path.endsWith('/session'))data={user:{id:'1',role:'admin',admin_class:options.operator?'operator':'super'},csrf_token:'binding180-local'};
  else if(path.endsWith('/security/payment-password'))data={configured:false,enforced:false,credential_epoch:0,totp_required:false};
  else if(path.endsWith('/channels/schema'))data={title:'支付通道',permissions:{create:true,update:true,delete:true},fields:[]};
  else if(path.endsWith('/payment-channels/templates'))data={items:[],total:0};
  else if(path.endsWith('/payment-channels'))data={items:[state],total:1,page:1,size:20};
  else if(path.endsWith('/weixin-options'))data=catalog;
  else if(path.endsWith('/payment-channels/'+state.id)){
   if(req.method()==='PATCH'){state=structuredClone(actual.rotated) as ParentChannel;const selected=req.postDataJSON().account as {appwxmp?:string;appwxa?:string};if(state.account){for(const key of ['appwxmp','appwxa']as const){if(selected?.[key]==='0'){delete state.account[key];if(state.account.weixin_binding_state)delete state.account.weixin_binding_state[key==='appwxmp'?'web':'mini']}else if(selected?.[key]!==undefined)state.account[key]=selected[key]}if(state.account.weixin_binding_state&&!Object.keys(state.account.weixin_binding_state).length)delete state.account.weixin_binding_state}if(options.uncertain){await route.abort('failed');return}}
   data=state;
  }else if(path.endsWith('/payment-channels/bind-weixin')){data={type:0,weixin:'0',items:[{...state,account:{...state.account,appwxmp:undefined}}],total:1}}
  await route.fulfill({json:{code:0,message:'',data}});
 });
 await page.goto('/admin/payapi/pay-channel');await page.waitForLoadState('networkidle');return {writes,reads};
}
for(const width of [375,1440])test('original parent Weixin selectors and batch review at '+width,async({page})=>{
 await page.setViewportSize({width,height:900});const f=await fixture(page);await expect(page.getByRole('region',{name:'支付通道',exact:true})).toBeVisible();await page.locator('[data-parent-parameters]').click();
 const dialog=page.getByRole('dialog',{name:'父通道配置'});await expect(dialog.getByLabel('关联微信公众号')).toHaveValue('81');await expect(dialog.getByLabel('关联微信小程序')).toHaveValue('82');await dialog.getByLabel('关联微信公众号').selectOption('0');
 await page.screenshot({path:test.info().outputPath('weixin-editor-'+width+'.png'),fullPage:true});
 await dialog.locator('[data-parent-save]').click();await expect(dialog).toHaveCount(0);expect(f.writes.filter(x=>x.path.endsWith('/payment-channels/'+actual.bound.id))).toHaveLength(1);
 const request=f.writes.at(-1)!;expect(request.body.account).toMatchObject({appwxmp:'0',expected_bindings_fingerprint:actual.catalog.fingerprint});
 await page.getByRole('checkbox',{name:'选择通道 '+actual.bound.id}).check();await page.getByRole('button',{name:'批量操作',exact:true}).click();await page.getByRole('menuitem',{name:'批量关联微信公众号',exact:true}).click();await expect(page.getByRole('dialog',{name:'批量关联微信公众号'})).toContainText(actual.bound.id);
 await page.getByLabel('批量微信应用').selectOption('0');await page.screenshot({path:test.info().outputPath('weixin-batch-'+width+'.png'),fullPage:true});await page.getByRole('button',{name:'取消',exact:true}).click();expect(f.writes.some(x=>x.path.endsWith('/bind-weixin'))).toBe(false);
});
test('uncertain binding save reads the original parent before another mutation',async({page})=>{
 const f=await fixture(page,{uncertain:true});await page.locator('[data-parent-parameters]').click();await page.getByLabel('关联微信公众号').selectOption('0');await page.locator('[data-parent-save]').click();await expect(page.getByRole('alert').filter({hasText:'结果未知'}).first()).toBeVisible();await expect(page.locator('[data-parent-save]')).toBeDisabled();expect(f.writes).toHaveLength(1);await page.getByRole('button',{name:'读取原结果',exact:true}).click();await expect(page.getByRole('dialog',{name:'父通道配置'})).toHaveCount(0);expect(f.writes).toHaveLength(1);expect(f.reads.filter(x=>x.endsWith('/payment-channels/'+actual.bound.id)).length).toBeGreaterThanOrEqual(2);
});
test('operator never sees executable binding controls',async({page})=>{await fixture(page,{operator:true});await expect(page.locator('[data-parent-parameters]')).toHaveCount(0);await expect(page.getByRole('button',{name:'批量关联微信公众号',exact:true})).toHaveCount(0)});
test('rotated profile warns and explicitly rebinds the same application IDs',async({page})=>{const f=await fixture(page,{stale:true});await page.locator('[data-parent-parameters]').click();await expect(page.getByRole('alert').filter({hasText:'应用资料已更新'})).toBeVisible();await page.getByRole('button',{name:'重新关联所选应用',exact:true}).click();await page.locator('[data-parent-save]').click();await expect(page.getByRole('dialog',{name:'父通道配置'})).toHaveCount(0);expect(f.writes).toHaveLength(1);expect(f.writes[0].body.account).toMatchObject({appwxmp:'81',appwxa:'82',expected_bindings_fingerprint:'b'.repeat(64)})});
