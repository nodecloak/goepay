import {createServer} from 'vite';
import {chromium,expect} from '@playwright/test';
import fs from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const web=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..'),api=process.env.MAIL194_API,token=process.env.MAIL194_COOKIE,artifacts=process.env.MAIL194_ARTIFACTS;
if(!api?.startsWith('http://127.0.0.1:')||!token||!artifacts)throw Error('explicit local synthetic fixture required');
const vite=await createServer({root:web,configFile:path.join(web,'vite.config.ts'),logLevel:'silent',server:{host:'127.0.0.1',port:0,strictPort:true,proxy:{'/console':{target:api,changeOrigin:false}}}});let browser;
try {
 await vite.listen();await fs.mkdir(artifacts,{recursive:true});const origin='http://127.0.0.1:'+vite.httpServer.address().port;
 browser=await chromium.launch({headless:true});const results=[];
 for(const width of [375,1440]) {
  const context=await browser.newContext({viewport:{width,height:1000}});let external=0;const errors=[];
  await context.route('**/*',async route=>{if(!['localhost','127.0.0.1'].includes(new URL(route.request().url()).hostname)){external++;return route.abort()};return route.continue()});
  await context.addCookies([{name:'goepay_admin',value:token,url:origin,httpOnly:true,sameSite:'Lax'}]);
  const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.goto(origin+'/admin/system/settings');await page.getByRole('tab',{name:'邮箱配置',exact:true}).click();await page.getByRole('tab',{name:'发送频率配置',exact:true}).click();
  const form=page.getByRole('form',{name:'邮件发送频率配置',exact:true});await expect(form).toBeVisible();
  for(const label of ['同一邮箱发送间隔(秒)','同一IP发送间隔(秒)','同一邮箱每日发送次数','同一IP每日发送次数'])await expect(page.getByLabel(label,{exact:true})).toBeVisible();
  if(width===375){await expect(page.getByText('当前实际生效：',{exact:false})).toBeVisible();await expect(page.getByLabel('同一邮箱每日发送次数',{exact:true})).toHaveValue('10')}
  await page.getByLabel('同一邮箱每日发送次数',{exact:true}).fill('4');await page.getByLabel('同一IP每日发送次数',{exact:true}).fill(width===375?'18':'17');
  await form.getByRole('button',{name:'保存',exact:true}).click();await expect(page.getByText('保存成功',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'重新读取邮件频率配置',exact:true}).click();await expect(page.getByLabel('同一邮箱每日发送次数',{exact:true})).toHaveValue('4');await expect(page.getByLabel('同一IP每日发送次数',{exact:true})).toHaveValue(width===375?'18':'17');
  await page.screenshot({path:path.join(artifacts,'frequency-'+width+'.png'),fullPage:true});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(errors).toEqual([]);expect(external).toBe(0);
  results.push({width,real_http:true,persisted_original_fields:true,external_requests:external});await context.close();
 }
 await fs.writeFile(path.join(artifacts,'result.json'),JSON.stringify({production:false,real_funds:false,real_mail:false,results},null,2)+'\n');
}finally {await browser?.close();await vite.close()}
