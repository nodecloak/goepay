/**
 * Local Nginx/Chromium template-preview regression.
 *
 * Run from the repository: node scripts/template-proxy-test.mjs --local-only
 * Prerequisites: a running local Docker engine, an already cached nginx:alpine,
 * web/node_modules and the already installed Playwright Chromium. No image or
 * browser is downloaded. No .env, credentials or existing app services are read.
 *
 * Uses current web/nginx.conf, the current TemplatePage iframe sandbox setting,
 * and a minimal synthetic API. This isolates proxy/CSP/CORP behavior; the real
 * authenticated renderer remains covered by page_template_http_test.go.
 * Reports/config copies are retained under .tools/verification/template-proxy-*.
 */
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {createHash, randomUUID} from 'node:crypto';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';

const usage='node scripts/template-proxy-test.mjs --local-only';
if(process.argv.includes('--help')){console.log(usage);process.exit(0);}
if(process.argv.length!==3||process.argv[2]!=='--local-only'){
 console.error('Explicit local test flag required. '+usage);
 process.exit(2);
}
const root=fileURLToPath(new URL('../',import.meta.url));
const runID=randomUUID(),prefix='goepay-template-review-'+runID.slice(0,12);
const label='goepay.template-proxy-test='+runID;
const output=path.join(root,'.tools','verification','template-proxy-'+runID);
fs.mkdirSync(output,{recursive:true});
const execute=promisify(execFile);
const sha=data=>createHash('sha256').update(data).digest('hex');
const save=(name,data)=>fs.writeFileSync(path.join(output,name),JSON.stringify(data,null,2)+'\n');
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const docker=async args=>(await execute('docker',args,{cwd:root,windowsHide:true,timeout:30000,maxBuffer:1024*1024})).stdout.trim();
let networkID, browser, cleanupPromise;
async function cleanup(){
 if(cleanupPromise)return cleanupPromise;
 cleanupPromise=(async()=>{
  const result={containers:[],network:null,errors:[]};
  if(browser){try{await browser.close();}catch{}}
  try{
   const owned=await docker(['ps','-a','--filter','label='+label,'--format','{{.ID}}']);
   for(const id of owned.split(/\s+/).filter(Boolean)){
    if(!/^[0-9a-f]{12,64}$/.test(id))throw new Error('Invalid owned container ID');
    await docker(['rm','-f',id]);result.containers.push(id);
   }
  }catch(e){result.errors.push(String(e));}
  if(networkID){try{await docker(['network','rm',networkID]);result.network=networkID;}catch(e){result.errors.push(String(e));}}
  save('cleanup.json',result);
  return result;
 })();
 return cleanupPromise;
}
for(const [signal,code] of [['SIGINT',130],['SIGTERM',143]])process.once(signal,()=>{void cleanup().finally(()=>process.exit(code));});

async function ready(url){
 const deadline=Date.now()+10000;
 while(Date.now()<deadline){
  try{if((await fetch(url,{signal:AbortSignal.timeout(1000)})).status===200)return;}catch{}
  await pause(100);
 }
 throw new Error('Disposable proxy did not become ready');
}
async function observe(page,base){
 const consoleMessages=[];
 const onConsole=m=>consoleMessages.push(m.text());
 const onFailure=r=>consoleMessages.push(r.url()+': '+r.failure()?.errorText);
 page.on('console',onConsole);page.on('requestfailed',onFailure);
 await page.waitForTimeout(250);
 const frame=page.frames().find(f=>f.url().includes('/templates/pay/default/preview'));
 let detail={text:null,color:null,logo:0};
 if(frame){try{detail=await frame.evaluate(()=>({text:document.getElementById('proof')?.textContent,color:document.getElementById('proof')?getComputedStyle(document.getElementById('proof')).color:null,logo:document.getElementById('logo')?.naturalWidth||0}));}catch{}}
 page.off('console',onConsole);page.off('requestfailed',onFailure);
 return {previewLoaded:detail.text==='PREVIEW-ONLY',inlineStyle:detail.color==='rgb(1, 2, 3)',logoLoaded:detail.logo===1,consoleMessages,frames:page.frames().map(f=>f.url())};
}
try{
 const config=fs.readFileSync(path.join(root,'web','nginx.conf'));
 const component=fs.readFileSync(path.join(root,'web','src','components','TemplatePage.vue'),'utf8');
 const sandbox=component.match(/<iframe\b[\s\S]*?\bsandbox="([^"]*)"/)?.[1];
 if(sandbox===undefined)throw new Error('TemplatePage iframe needs an explicit sandbox attribute');
 const previewPath='/console/v1/admin/templates/pay/default/preview';
 const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII=','base64');
 const imagePath='/console/v1/admin/templates/images/'+sha(png);
 const backendCSP="default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; script-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'self'; sandbox allow-same-origin";
 const escape=s=>s.replaceAll('&','&amp;').replaceAll('"','&quot;').replaceAll('<','&lt;');
 fs.writeFileSync(path.join(output,'nginx.conf'),config);
 fs.writeFileSync(path.join(output,'logo.png'),png);
 fs.writeFileSync(path.join(output,'index.html'),`<!doctype html><html><body><h1>Local template proxy test</h1><iframe id="preview" title="preview" src="${previewPath}" sandbox="${escape(sandbox)}"></iframe></body></html>`);
 fs.writeFileSync(path.join(output,'upstream.conf'),`server { listen 8080;
 location = ${previewPath} { default_type text/html; add_header Content-Security-Policy "${backendCSP}" always; add_header Cache-Control no-store always; return 200 '<!doctype html><html><head><style>#proof{color:rgb(1,2,3)}</style></head><body><p id="proof">PREVIEW-ONLY</p><img id="logo" src="${imagePath}"></body></html>'; }
 location = ${imagePath} { alias /tmp/template-proxy-logo.png; default_type image/png; add_header Cross-Origin-Resource-Policy same-origin always; add_header Cache-Control no-store always; }
 location / { default_type text/plain; return 200 'SYNTHETIC-API'; }
}`);
 save('source.json',{runID,nginx_sha256:sha(config),component_sha256:sha(component),sandbox,scope:'Current proxy + component sandbox; synthetic API only, no app services or credentials'});
 // An independent bridge is required for localhost port publishing. Docker's
 // --internal network discards that published port on some desktop versions.
 // All application URLs below remain fixed to this disposable localhost peer.
 networkID=await docker(['network','create','--label',label,prefix]);
 const mount=(file,target)=>'type=bind,source='+path.join(output,file)+',target='+target+',readonly';
 await docker(['run','--pull','never','-d','--name',prefix+'-api','--label',label,'--network',prefix,'--network-alias','api','--mount',mount('upstream.conf','/etc/nginx/conf.d/default.conf'),'--mount',mount('logo.png','/tmp/template-proxy-logo.png'),'nginx:alpine']);
 const proxyID=await docker(['run','--pull','never','-d','--name',prefix+'-web','--label',label,'--network',prefix,'-p','127.0.0.1::80','--mount',mount('nginx.conf','/etc/nginx/conf.d/default.conf'),'--mount',mount('index.html','/usr/share/nginx/html/index.html'),'nginx:alpine']);
 const published=await docker(['port',proxyID,'80/tcp']);
 if(!/^127\.0\.0\.1:\d+$/.test(published))throw new Error('Proxy must publish only a random localhost port');
 const base='http://'+published;
 await ready(base+previewPath);
 const paths=[];
 for(const kind of ['home','pay','qrpay'])for(const name of ['default','compact'])paths.push({path:`/console/v1/admin/templates/${kind}/${name}/preview`,allowed:true});
 for(const route of [previewPath+'/',previewPath+'-extra','/console/v1/admin/templates/unknown/default/preview','/console/v1/admin/templates/pay/arbitrary/preview','/console/v1/admin/session','/console/v1/merchant/templates/pay/default/preview','/console/v1/admin/templates/pay/default/config'])paths.push({path:route,allowed:false});
 const boundaries=[];
 for(const entry of paths){
  const response=await fetch(base+entry.path),xfo=response.headers.get('x-frame-options'),csp=response.headers.get('content-security-policy')||'';
  const passed=entry.allowed?xfo==='SAMEORIGIN'&&csp.includes("frame-ancestors 'self'")&&csp.includes("script-src 'none'")&&csp.includes("form-action 'none'")&&!csp.includes('allow-scripts'):xfo==='DENY'&&csp.includes("frame-ancestors 'none'");
  boundaries.push({...entry,xfo,csp,passed});
 }
 save('boundaries.json',boundaries);
 const {chromium}=await import(pathToFileURL(path.join(root,'web','node_modules','playwright-core','index.mjs')).href);
 browser=await chromium.launch({headless:true});
 const page=await browser.newPage();
 const browserMessages=[];
 page.on('console',m=>browserMessages.push(m.text()));
 page.on('requestfailed',r=>browserMessages.push(r.url()+': '+r.failure()?.errorText));
 await page.goto(base,{waitUntil:'networkidle'});
 const actualSandbox=await page.locator('#preview').getAttribute('sandbox');
 const positive=await observe(page,base);
 save('preview.json',{sandbox:actualSandbox,...positive,consoleMessages:browserMessages.slice()});
 await page.locator('#preview').evaluate(el=>{el.setAttribute('sandbox','');el.src=el.src;});
 await page.waitForLoadState('networkidle');
 const negative=await observe(page,base);
 save('opaque-negative-control.json',{...negative,consoleMessages:browserMessages.slice()});
 const passed=boundaries.every(x=>x.passed)&&actualSandbox==='allow-same-origin'&&positive.previewLoaded&&positive.inlineStyle&&positive.logoLoaded&&negative.previewLoaded&&negative.inlineStyle&&!negative.logoLoaded&&browserMessages.some(x=>x.includes('NotSameOrigin'));
 save('result.json',{passed,path_checks:boundaries.length,path_checks_passed:boundaries.filter(x=>x.passed).length,iframe_same_origin:actualSandbox==='allow-same-origin',preview:positive,opaqueLogoBlocked:!negative.logoLoaded,nginx_sha256:sha(config)});
 console.log(JSON.stringify({passed,path_checks:boundaries.length,preview:positive.previewLoaded,inline_style:positive.inlineStyle,logo:positive.logoLoaded,opaque_logo_blocked:!negative.logoLoaded,evidence:output}));
 if(!passed)process.exitCode=1;
}catch(e){save('error.json',{error:String(e)});console.error(String(e));console.error('Evidence: '+output);process.exitCode=1;}
finally{
 const cleaned=await cleanup();
 if(cleaned.errors.length){console.error('Disposable resource cleanup incomplete; see cleanup.json');process.exitCode=1;}
}
