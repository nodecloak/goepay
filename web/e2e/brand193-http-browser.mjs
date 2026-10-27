import {createServer} from 'vite';
import {chromium,expect} from '@playwright/test';
import QRCode from 'qrcode';
import fs from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const web=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..'),api=process.env.BRAND193_API,token=process.env.BRAND193_COOKIE,artifacts=process.env.BRAND193_ARTIFACTS;
if(!api?.startsWith('http://127.0.0.1:')||!token||!artifacts)throw new Error('explicit local synthetic fixture required');
const home=await QRCode.toBuffer('synthetic193-home',{width:80}),icon=await QRCode.toBuffer('synthetic193-icon',{width:80});
const vite=await createServer({root:web,configFile:path.join(web,'vite.config.ts'),logLevel:'silent',server:{host:'127.0.0.1',port:0,strictPort:true,proxy:{'/console':{target:api,changeOrigin:false},'/static/img':{target:api,changeOrigin:false}}}});let browser;
try{
 await vite.listen();await fs.mkdir(artifacts,{recursive:true});const origin='http://127.0.0.1:'+vite.httpServer.address().port;
 browser=await chromium.launch({headless:true});const results=[];
 for(const width of [375,1440]){
  const context=await browser.newContext({viewport:{width,height:1000}});let external=0;const errors=[];
  await context.route('**/*',async route=>{if(!['localhost','127.0.0.1'].includes(new URL(route.request().url()).hostname)){external++;return route.abort()};return route.continue()});
  await context.addCookies([{name:'goepay_admin',value:token,url:origin,httpOnly:true,sameSite:'Lax'}]);const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.goto(origin+'/admin/system/settings');
  await page.getByLabel('首页LOGO',{exact:true}).setInputFiles({name:'home.png',mimeType:'image/png',buffer:home});await expect(page.getByText('首页LOGO上传成功',{exact:true})).toBeVisible();
  await page.getByLabel('全局LOGO',{exact:true}).setInputFiles({name:'global.png',mimeType:'image/png',buffer:icon});await expect(page.getByText('全局LOGO上传成功',{exact:true})).toBeVisible();
  for(const name of ['当前首页LOGO','当前全局LOGO']){const image=page.getByRole('img',{name,exact:true});await expect(image).toBeVisible();await expect.poll(()=>image.evaluate(i=>i.naturalWidth)).toBe(80)}
  await page.screenshot({path:path.join(artifacts,'settings-'+width+'.png'),fullPage:true});
  if(width===375)await page.getByRole('button',{name:'展开菜单',exact:true}).click();const logo=page.getByRole('img',{name:'全局品牌图标',exact:true});await expect(logo).toBeVisible();await expect.poll(()=>logo.evaluate(i=>i.naturalWidth)).toBe(80);
  if(width===375)await page.getByRole('button',{name:'关闭菜单',exact:true}).click();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(errors).toEqual([]);expect(external).toBe(0);
  const clean=await browser.newContext({viewport:{width,height:1000}});const login=await clean.newPage();await login.goto(origin+'/merchant/dashboard');
  // On small screens the login introduction is intentionally collapsed. The
  // same global image must still load, independent of administrator cookies.
  const loginLogo=login.locator('img[alt="全局品牌图标"]');await expect(loginLogo).toBeAttached();await expect.poll(()=>loginLogo.evaluate(i=>i.naturalWidth)).toBe(80);
  results.push({width,real_http:true,separate_images:true,anonymous_global_image:true,external_requests:external});await clean.close();await context.close();
 }
 await fs.writeFile(path.join(artifacts,'result.json'),JSON.stringify({production:false,real_funds:false,results},null,2)+'\n');
}finally{await browser?.close();await vite.close()}
