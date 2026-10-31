import test, { before } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { webcrypto } from 'node:crypto';
import { createRequire } from 'node:module';

const require = createRequire('E:/GOEPAY/web/package.json');
const { JSDOM } = require('jsdom');
let source = '';
try { source = await readFile(new URL('../doc.js', import.meta.url), 'utf8'); } catch {}
const app = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));
let platform, platformPublic;
const now = Date.UTC(2026, 9, 31, 2);
const meta = { slug: 'pay_query', method: 'POST', path: '/api/pay/query', origin: 'https://pay.example.com/' };
const scalar = (body) => Object.fromEntries(Object.entries(body).filter(([, v]) => v === null || typeof v !== 'object').map(([k, v]) => [k, v === true ? '1' : v === false || v === null ? '' : String(v)]));
const canonicalFixture = (body) => Object.entries(scalar(body)).filter(([k, v]) => !['sign', 'sign_type'].includes(k) && v.replace(/[ \t\n\r\0\v]/g, '') !== '').sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([k,v]) => k+'='+v).join('&');
const pem = (bytes) => '-----BEGIN PUBLIC KEY-----\n'+Buffer.from(bytes).toString('base64').match(/.{1,64}/g).join('\n')+'\n-----END PUBLIC KEY-----\n';

before(async () => {
  platform = await webcrypto.subtle.generateKey({ name:'RSASSA-PKCS1-v1_5', modulusLength:2048, publicExponent:new Uint8Array([1,0,1]), hash:'SHA-256' },true,['sign','verify']);
  platformPublic = pem(await webcrypto.subtle.exportKey('spki',platform.publicKey));
});
async function envelope(overrides={}) {
  const response={code:0,trade_no:'DOC_PAID',money:'1.00',status:1,timestamp:String(now/1000),sign_type:'RSA',...overrides};
  response.sign=Buffer.from(await webcrypto.subtle.sign('RSASSA-PKCS1-v1_5',platform.privateKey,new TextEncoder().encode(canonicalFixture(response)))).toString('base64');
  return { environment:'documentation-sandbox',request:{method:meta.method,path:meta.path,canonical:'out_trade_no=DOC_PAID',signature_verified:true},response,platform_public_key:platformPublic,elapsed_ms:1 };
}
function fixture() {
 return new JSDOM(`<!doctype html><html><body><button data-search-open>搜索</button><a data-doc-link data-group="支付" href="/doc/pay_query.html">订单查询</a><a data-doc-link href="/doc/sdk.html">Go SDK</a><a data-doc-link href="/doc/pay_query.html">订单查询</a><dialog id="doc-search-dialog"><button data-search-close>关闭</button><input data-search-input><ul data-search-results></ul><p data-search-status></p></dialog><article class="doc-article"><h1>查询</h1><h2 id="params">请求参数</h2><pre><code>hello &lt;world&gt;</code></pre></article><ol data-doc-toc></ol><section data-api-console data-endpoint-slug="pay_query" data-method="POST" data-path="/api/pay/query" data-api-origin="https://pay.example.com/"><details data-console-disclosure open><summary>接口测试</summary><input data-param-name="out_trade_no" data-label="订单号" value="DOC_PAID" required><span data-param-error="out_trade_no"></span><button data-console-send>发送沙箱请求</button><button data-console-cancel hidden>取消</button><button data-console-reset>重置</button><button data-request-lang="curl">cURL</button><button data-request-lang="go">Go</button><button data-request-lang="http">HTTP</button><pre><code data-request-code></code></pre><button data-console-copy>复制</button><p data-console-status role="status"></p><p data-console-error hidden></p><span data-response-http></span><span data-response-duration></span><span data-request-verification></span><span data-response-signature></span><div data-console-empty>尚未发送</div><div data-console-result hidden><button data-response-tab="body">JSON</button><button data-response-tab="headers">响应头</button><button data-response-tab="request">请求</button><div data-response-panel="body"><pre><code data-response-code></code></pre></div><div data-response-panel="headers" hidden><pre><code data-response-headers></code></pre></div><div data-response-panel="request" hidden><pre><code data-response-request></code></pre></div></div></details></section></body></html>`,{url:'https://pay.example.com/doc/pay_query.html',pretendToBeVisual:true});
}
const tick = () => new Promise(resolve => setTimeout(resolve,0));

function responseCopyButton(doc) {
 const button=doc.createElement('button');button.type='button';button.className='api-copy';button.setAttribute('data-response-copy','');button.textContent='复制响应';
 doc.querySelector('[data-console-result]').prepend(button);return button;
}

async function waitForConsole(doc) {
 for(let n=0;n<100&&doc.querySelector('[data-api-console]').dataset.state==='loading';n++)await new Promise(r=>setTimeout(r,10));
}

function delayedVerification() {
 let entered,release;
 const ready=new Promise(resolve=>{entered=resolve;}),gate=new Promise(resolve=>{release=resolve;});
 return {ready,release,crypto:{subtle:{importKey:(...args)=>webcrypto.subtle.importKey(...args),verify:async(...args)=>{const valid=await webcrypto.subtle.verify(...args);entered();await gate;return valid;}}}};
}

test('request canonical uses original scalar values and byte-order sorting',()=>{
 assert.equal(typeof app.canonical,'function');
 assert.equal(app.canonical({z:'0',a:' +&汉字 ',empty:'',blank:' \t',sign:'x',sign_type:'RSA'}),'a= +&汉字 &blank= \t&z=0');
 assert.equal(app.canonical({'\u{10000}':'b','\uE000':'a'}),'\uE000=a&\u{10000}=b');
});
test('response profile omits ASCII whitespace and unsigned structures',()=>{
 assert.equal(typeof app.responseFields,'function');
 assert.deepEqual({...app.responseFields({code:0,t:true,f:false,n:null,blank:'\v\0 \t',data:[{id:1}]})},{code:'0',t:'1',f:'',n:'',blank:'\v\0 \t'});
 assert.equal(app.canonical(app.responseFields({code:0,t:true,blank:'\v\0 \t',data:[]}),true),'code=0&t=1');
 assert.throws(()=>app.responseFields({code:0,id:9007199254740992}),/数字|数值|范围/);
});
test('business values reject overrides, arrays and ambiguous keys without modifying input',()=>{
 assert.equal(typeof app.validateParams,'function');
 for(const p of [{pid:'1'},{timestamp:'1'},{sign:'x'},{sign_type:'MD5'},{'a.b':'x'},{'a[]':'x'},{a:['x','y']},{a:'\uD800'}])assert.throws(()=>app.validateParams(p));
 const original={name:'汉字 +&',money:'1.00'};assert.deepEqual({...app.validateParams(original)},original);
});
test('browser signer produces real interoperable RSA and only exports public material',async()=>{
 assert.equal(typeof app.createSigner,'function');
 const signer=app.createSigner(webcrypto);
 const request=await app.signSandboxRequest(meta.slug,{out_trade_no:'DOC_PAID'},signer,now);
 assert.equal(request.endpoint,'pay_query');assert.equal(request.params.pid,'900001');assert.equal(request.params.timestamp,String(now/1000));assert.equal(request.params.sign_type,'RSA');assert.ok(request.public_key.startsWith('-----BEGIN PUBLIC KEY-----'));
 const der=Buffer.from(request.public_key.replace(/-----[^-]+-----|\s/g,''),'base64');
 const pub=await webcrypto.subtle.importKey('spki',der,{name:'RSASSA-PKCS1-v1_5',hash:'SHA-256'},false,['verify']);
 assert.equal(await webcrypto.subtle.verify('RSASSA-PKCS1-v1_5',pub,Buffer.from(request.params.sign,'base64'),new TextEncoder().encode(app.canonical(request.params))),true);
 assert.ok(!JSON.stringify(request).includes('PRIVATE KEY'));
 await assert.rejects(()=>app.signSandboxRequest('https://evil.example/create',{},signer,now));
});
test('response verifies RSA, exact environment, endpoint and inclusive timestamp window',async()=>{
 assert.equal(typeof app.verifySandboxEnvelope,'function');
 for(const offset of [-300,0,300])await app.verifySandboxEnvelope(await envelope({timestamp:String(now/1000+offset)}),meta,{crypto:webcrypto,now:()=>now});
 for(const offset of [-301,301])await assert.rejects(()=>envelope({timestamp:String(now/1000+offset)}).then(e=>app.verifySandboxEnvelope(e,meta,{crypto:webcrypto,now:()=>now})),/时间/);
 const bad=await envelope();bad.response.money='2.00';await assert.rejects(()=>app.verifySandboxEnvelope(bad,meta,{crypto:webcrypto,now:()=>now}),/签名/);
 const wrong=await envelope();wrong.environment='production';await assert.rejects(()=>app.verifySandboxEnvelope(wrong,meta,{crypto:webcrypto,now:()=>now}));
 const route=await envelope();route.request.path='/api/pay/refund';await assert.rejects(()=>app.verifySandboxEnvelope(route,meta,{crypto:webcrypto,now:()=>now}));
});
test('signed business errors and unsigned data are presented without claiming completion',async()=>{
 const e=await envelope({code:-1,msg:'invalid money',data:[{id:'unsigned'}]});
 const verified=await app.verifySandboxEnvelope(e,meta,{crypto:webcrypto,now:()=>now});
 assert.equal(verified.response.code,-1);assert.equal(verified.unsignedStructures,true);
});
test('network target is the fixed docs sandbox with no credentials or redirects',async()=>{
 assert.equal(typeof app.sendSandboxRequest,'function');
 const calls=[];const signer=app.createSigner(webcrypto);
 const got=await app.sendSandboxRequest({...meta,params:{out_trade_no:'DOC_PAID'}},{crypto:webcrypto,signer,now:()=>now,fetcher:async(path,options)=>{calls.push({path,options});return new Response(JSON.stringify(await envelope()),{status:200,headers:{'Content-Type':'application/json','Cache-Control':'no-store'}});}});
 assert.equal(calls.length,1);assert.equal(calls[0].path,'/doc/playground/run');assert.equal(calls[0].options.method,'POST');assert.equal(calls[0].options.credentials,'omit');assert.equal(calls[0].options.redirect,'error');assert.equal(got.httpStatus,200);
 const body=JSON.parse(calls[0].options.body);assert.equal(body.endpoint,'pay_query');assert.equal(body.params.pid,'900001');
});
test('HTTP errors, wrong content, oversize replies and invalid signatures are rejected',async()=>{
 for(const response of [new Response(JSON.stringify({error:'invalid signature'}),{status:400,headers:{'Content-Type':'application/json'}}),new Response('<html>no</html>',{status:200,headers:{'Content-Type':'text/html'}}),new Response('x'.repeat(131073),{status:200,headers:{'Content-Type':'application/json'}})]){
  await assert.rejects(()=>app.sendSandboxRequest({...meta,params:{out_trade_no:'DOC_PAID'}},{crypto:webcrypto,signer:app.createSigner(webcrypto),now:()=>now,fetcher:async()=>response}));
 }
});
test('cancel and timeout never retry or make a second request',async()=>{
 const abort=new AbortController();let calls=0;const signer=app.createSigner(webcrypto);
 await app.signSandboxRequest(meta.slug,{},signer,now);
 const fetcher=async(_path,{signal})=>{calls++;return new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError')),{once:true}));};
 const pending=app.sendSandboxRequest({...meta,params:{out_trade_no:'DOC_PAID'}},{crypto:webcrypto,signer,fetcher,signal:abort.signal,timeoutMs:20});
 await assert.rejects(()=>pending);assert.equal(calls,1);
});
test('code examples escape user values and never include temporary signatures or secrets',()=>{
 assert.equal(typeof app.requestExample,'function');
 const params={name:"x'$(touch x)`hi`",money:'1.00',out_trade_no:'DOC_001'};
 const curl=app.requestExample(meta,params,'curl');assert.ok(curl.includes('https://pay.example.com/api/pay/query'));assert.ok(curl.includes("'\"'\"'"));assert.ok(curl.includes('<RSA_SIGNATURE>'));
 const go=app.requestExample(meta,params,'go');assert.ok(go.includes('client.Query'));assert.ok(go.includes('params.Set("money", "1.00")'));
 assert.ok(!go.includes('900001'));assert.ok(!go.includes('PRIVATE KEY'));assert.ok(!go.includes('sign_type'));
});
test('progressive search, TOC and copy use safe text and restore keyboard focus',async()=>{
 assert.equal(typeof app.mountDocumentation,'function');
 const dom=fixture(),doc=dom.window.document,writes=[];
 const mounted=app.mountDocumentation(doc,{crypto:webcrypto,clipboard:{writeText:async text=>writes.push(text)},now:()=>now,matchMedia:()=>({matches:false})});
 assert.equal(doc.querySelectorAll('[data-doc-toc] a').length,1);assert.equal(doc.querySelector('.doc-copy-button').textContent,'复制');
 doc.querySelector('.doc-copy-button').click();await tick();assert.deepEqual(writes,['hello <world>']);
 const open=doc.querySelector('[data-search-open]');open.focus();open.click();const input=doc.querySelector('[data-search-input]');assert.equal(doc.activeElement,input);
 input.value='订单';input.dispatchEvent(new dom.window.Event('input'));assert.equal(doc.querySelectorAll('[data-search-results] a').length,1);
 doc.querySelector('[data-search-close]').click();assert.equal(doc.activeElement,open);
 input.value='<img src=x>';open.click();input.value='<img src=x>';input.dispatchEvent(new dom.window.Event('input'));assert.equal(doc.querySelectorAll('[data-search-results] img').length,0);
 mounted.destroy();dom.window.close();
});
test('console sends only on click, shows verified result and clears it after input changes',async()=>{
 const dom=fixture(),doc=dom.window.document;let calls=0;
 const mounted=app.mountDocumentation(doc,{crypto:webcrypto,signer:app.createSigner(webcrypto),now:()=>now,matchMedia:()=>({matches:true}),fetcher:async()=>{calls++;return new Response(JSON.stringify(await envelope()),{status:200,headers:{'Content-Type':'application/json'}});}});
 assert.equal(calls,0);assert.equal(doc.querySelector('[data-console-disclosure]').open,false);
 doc.querySelector('[data-console-send]').click();
 for(let n=0;n<100&&doc.querySelector('[data-api-console]').dataset.state==='loading';n++)await new Promise(r=>setTimeout(r,10));
 assert.equal(calls,1);assert.equal(doc.querySelector('[data-api-console]').dataset.state,'success');assert.equal(doc.querySelector('[data-console-result]').hidden,false);assert.match(doc.querySelector('[data-response-signature]').textContent,/验签通过/);
 const field=doc.querySelector('[data-param-name]');field.value='DOC_PENDING';field.dispatchEvent(new dom.window.Event('input'));assert.equal(doc.querySelector('[data-console-result]').hidden,true);assert.match(doc.querySelector('[data-console-status]').textContent,/参数已更改/);
 field.value='';doc.querySelector('[data-console-send]').click();assert.equal(calls,1);assert.match(doc.querySelector('[data-param-error]').textContent,/必填/);
 mounted.destroy();dom.window.close();
});

test('actual disclosure wrapper and hidden field errors remain usable on mobile',()=>{
 const dom=fixture(),doc=dom.window.document,root=doc.querySelector('[data-api-console]'),inside=root.querySelector('[data-console-disclosure]');
 const wrapper=doc.createElement('details');wrapper.setAttribute('data-console-disclosure','');wrapper.open=true;
 root.before(wrapper);while(inside.firstChild)root.append(inside.firstChild);inside.remove();wrapper.append(root);
 const row=root.querySelector('[data-param-error]');row.hidden=true;
 const mounted=app.mountDocumentation(doc,{crypto:webcrypto,matchMedia:()=>({matches:true})});
 assert.equal(wrapper.open,false);
 root.querySelector('[data-param-name]').value='';root.querySelector('[data-console-send]').click();assert.equal(row.hidden,false);assert.match(row.textContent,/必填/);
 root.querySelector('[data-console-reset]').click();assert.equal(row.hidden,true);
 mounted.destroy();dom.window.close();
});

test('search includes real endpoint paths and HTTP methods',()=>{
 const dom=fixture(),doc=dom.window.document;
 doc.querySelector('[data-doc-link]').dataset.searchExtra='/api/pay/query POST';
 const mounted=app.mountDocumentation(doc,{crypto:webcrypto});doc.querySelector('[data-search-open]').click();
 const input=doc.querySelector('[data-search-input]');input.value='/api/pay/query';input.dispatchEvent(new dom.window.Event('input'));
 assert.equal(doc.querySelectorAll('[data-search-results] a').length,1);
 mounted.destroy();dom.window.close();
});

test('explicit cancellation aborts a pending fetch once and restores console controls',async()=>{
 const dom=fixture(),doc=dom.window.document;let calls=0,arrived;
 const ready=new Promise(resolve=>{arrived=resolve;});
 const signer=app.createSigner(webcrypto);await app.signSandboxRequest(meta.slug,{},signer,now);
 const mounted=app.mountDocumentation(doc,{crypto:webcrypto,signer,now:()=>now,fetcher:async(_path,{signal})=>{calls++;arrived();return new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError')),{once:true}));}});
 doc.querySelector('[data-console-send]').click();doc.querySelector('[data-console-send]').click();await ready;
 assert.equal(doc.querySelector('[data-console-send]').disabled,true);doc.querySelector('[data-console-cancel]').click();await tick();
 assert.equal(calls,1);assert.equal(doc.querySelector('[data-console-send]').disabled,false);assert.equal(doc.querySelector('[data-console-cancel]').hidden,true);assert.match(doc.querySelector('[data-console-status]').textContent,/取消/);
 mounted.destroy();dom.window.close();
});

test('signed error content is inert text and remains visibly distinct from successful completion',async()=>{
 const dom=fixture(),doc=dom.window.document;
 const mounted=app.mountDocumentation(doc,{crypto:webcrypto,signer:app.createSigner(webcrypto),now:()=>now,fetcher:async()=>new Response(JSON.stringify(await envelope({code:-1,msg:'<img src=x onerror=alert(1)>'})),{status:200,headers:{'Content-Type':'application/json'}})});
 doc.querySelector('[data-console-send]').click();for(let n=0;n<100&&doc.querySelector('[data-api-console]').dataset.state==='loading';n++)await new Promise(r=>setTimeout(r,10));
 assert.equal(doc.querySelector('[data-api-console]').dataset.state,'error');assert.equal(doc.querySelectorAll('[data-console-error] img').length,0);assert.match(doc.querySelector('[data-console-error]').textContent,/<img/);assert.equal(doc.querySelector('[data-console-error]').hidden,false);assert.match(doc.querySelector('[data-response-signature]').textContent,/验签通过/);
 mounted.destroy();dom.window.close();
});

test('response toolbar has a copy button inside the initially hidden result',async()=>{
 const template=await readFile(new URL('../console.html',import.meta.url),'utf8');
 const dom=new JSDOM(template),doc=dom.window.document,button=doc.querySelector('[data-response-copy]');
 assert.ok(button);assert.ok(button.closest('[data-console-result][hidden]'));assert.ok(button.closest('.api-code-toolbar'));assert.equal(button.className,'api-copy');assert.equal(button.type,'button');
 dom.window.close();
});

test('response copy follows the active body, headers and request tabs without copying stale results',async()=>{
 const dom=fixture(),doc=dom.window.document,writes=[],button=responseCopyButton(doc);
 const mounted=app.mountDocumentation(doc,{crypto:webcrypto,signer:app.createSigner(webcrypto),now:()=>now,clipboard:{writeText:async value=>writes.push(value)},fetcher:async()=>new Response(JSON.stringify(await envelope()),{status:200,headers:{'Content-Type':'application/json','Cache-Control':'no-store'}})});
 button.click();await tick();assert.deepEqual(writes,[]);
 doc.querySelector('[data-console-send]').click();await waitForConsole(doc);
 for(const [tab,selector] of [['body','response-code'],['headers','response-headers'],['request','response-request']]){
  doc.querySelector('[data-response-tab="'+tab+'"]').click();assert.equal(button.textContent,'复制响应');
  button.click();await tick();assert.equal(writes.at(-1),doc.querySelector('[data-'+selector+']').textContent);assert.equal(button.textContent,'已复制');assert.match(button.getAttribute('aria-label'),/已复制/);
 }
 assert.equal(writes.length,3);assert.ok(writes.every(value=>!value.includes('PRIVATE KEY')));
 const field=doc.querySelector('[data-param-name]');field.value='DOC_PENDING';field.dispatchEvent(new dom.window.Event('input'));button.click();await tick();assert.equal(writes.length,3);
 mounted.destroy();dom.window.close();
});

test('signed business errors can be copied and clipboard failure gives visible recoverable feedback',async()=>{
 const dom=fixture(),doc=dom.window.document,writes=[],button=responseCopyButton(doc);let denied=false;
 const mounted=app.mountDocumentation(doc,{crypto:webcrypto,signer:app.createSigner(webcrypto),now:()=>now,clipboard:{writeText:async value=>{if(denied)throw Error('denied');writes.push(value);}},fetcher:async()=>new Response(JSON.stringify(await envelope({code:-1,msg:'invalid money'})),{status:200,headers:{'Content-Type':'application/json'}})});
 doc.querySelector('[data-console-send]').click();await waitForConsole(doc);assert.equal(doc.querySelector('[data-api-console]').dataset.state,'error');
 button.click();await tick();assert.equal(JSON.parse(writes[0]).code,-1);assert.equal(button.textContent,'已复制');
 denied=true;button.click();await tick();assert.match(button.textContent,/复制失败.*手动选择/);assert.match(button.getAttribute('aria-label'),/复制失败/);assert.equal(writes.length,1);assert.equal(doc.querySelector('.doc-copy-fallback'),null);
 denied=false;button.click();await tick();assert.equal(button.textContent,'已复制');assert.equal(writes.length,2);
 mounted.destroy();dom.window.close();
});

for(const mode of ['cancel','timeout'])test(mode+' during response WebCrypto verification rejects with AbortError',{timeout:5000},async()=>{
 const delayed=delayedVerification(),abort=new AbortController(),signer=app.createSigner(webcrypto),reply=JSON.stringify(await envelope());
 await app.signSandboxRequest(meta.slug,{},signer,now);
 let sawAbort;
 const aborted=new Promise(resolve=>{sawAbort=resolve;});
 const pending=app.sendSandboxRequest({...meta,params:{out_trade_no:'DOC_PAID'}},{crypto:delayed.crypto,signer,now:()=>now,signal:abort.signal,timeoutMs:mode==='timeout'?200:10000,fetcher:async(_path,{signal})=>{signal.addEventListener('abort',sawAbort,{once:true});return new Response(reply,{status:200,headers:{'Content-Type':'application/json'}});}});
 await delayed.ready;if(mode==='cancel')abort.abort();await aborted;delayed.release();
 await assert.rejects(pending,{name:'AbortError'});
});

test('cancel during WebCrypto verification never publishes a successful console result',{timeout:5000},async()=>{
 const dom=fixture(),doc=dom.window.document,delayed=delayedVerification(),signer=app.createSigner(webcrypto),reply=JSON.stringify(await envelope());
 await app.signSandboxRequest(meta.slug,{},signer,now);
 const mounted=app.mountDocumentation(doc,{crypto:delayed.crypto,signer,now:()=>now,fetcher:async()=>new Response(reply,{status:200,headers:{'Content-Type':'application/json'}})});
 try {
  doc.querySelector('[data-console-send]').click();await delayed.ready;doc.querySelector('[data-console-cancel]').click();delayed.release();await waitForConsole(doc);
  assert.equal(doc.querySelector('[data-api-console]').dataset.state,'error');assert.equal(doc.querySelector('[data-console-result]').hidden,true);assert.match(doc.querySelector('[data-console-status]').textContent,/取消/);assert.doesNotMatch(doc.querySelector('[data-response-signature]').textContent,/验签通过/);assert.equal(doc.querySelector('[data-console-send]').disabled,false);
 } finally {delayed.release();mounted.destroy();dom.window.close();}
});
