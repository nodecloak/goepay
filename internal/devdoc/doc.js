// Developer documentation enhancements. Requests are restricted to the isolated
// documentation sandbox; merchant credentials and signing keys are never stored.
const SYSTEM_FIELDS = new Set(['pid', 'timestamp', 'sign', 'sign_type']);
const ENDPOINTS = new Set(['pay_submit', 'pay_create', 'pay_query', 'pay_refund', 'pay_refundquery', 'pay_close', 'merchant_info', 'merchant_orders', 'transfer_submit', 'transfer_query', 'transfer_balance']);
const ASCII_EMPTY = /^[ \t\n\r\0\v]*$/;
const MAX_REPLY_BYTES = 128 * 1024;
const encoder = new TextEncoder();

function byteOrder(a, b) {
  const x = encoder.encode(a), y = encoder.encode(b);
  for (let n = 0; n < Math.min(x.length, y.length); n++) if (x[n] !== y[n]) return x[n] - y[n];
  return x.length - y.length;
}

function wellFormed(value) {
  if (typeof value.isWellFormed === 'function') return value.isWellFormed();
  for (let n = 0; n < value.length; n++) {
    const code = value.charCodeAt(n);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(++n);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (code >= 0xdc00 && code <= 0xdfff) return false;
  }
  return true;
}

export function canonical(fields, response = false) {
  return Object.keys(fields).filter(key => !['sign', 'sign_type'].includes(key) && fields[key] !== '' && (!response || !ASCII_EMPTY.test(fields[key])))
    .sort(byteOrder).map(key => key + '=' + fields[key]).join('&');
}

export function responseFields(body) {
  if (!body || Array.isArray(body) || typeof body !== 'object') throw Error('响应格式不正确');
  const result = Object.create(null);
  for (const [key, value] of Object.entries(body)) {
    if (!wellFormed(key)) throw Error('响应字符编码不正确');
    if (value !== null && typeof value === 'object') continue;
    if (typeof value === 'number' && (!Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value)))) throw Error('响应数字超出安全数值范围');
    if (!['string', 'number', 'boolean'].includes(typeof value) && value !== null) throw Error('响应字段类型不正确');
    result[key] = value === true ? '1' : value === false || value === null ? '' : String(value);
    if (!wellFormed(result[key])) throw Error('响应字符编码不正确');
  }
  return result;
}

export function validateParams(params) {
  if (!params || Array.isArray(params) || typeof params !== 'object') throw Error('请求参数必须是唯一的文本字段');
  const result = Object.create(null);
  for (const [key, value] of Object.entries(params)) {
    if (!key || /[\[\] .\0\t\r\n]/.test(key) || !wellFormed(key) || typeof value !== 'string' || !wellFormed(value)) throw Error('请求参数名称或字符编码不正确');
    if (SYSTEM_FIELDS.has(key)) throw Error('商户号、时间戳和签名由文档沙箱自动生成');
    result[key] = value;
  }
  return result;
}

function base64(bytes) {
  let value = '';
  const data = new Uint8Array(bytes);
  for (let n = 0; n < data.length; n += 32768) value += String.fromCharCode(...data.subarray(n, n + 32768));
  return btoa(value);
}

function unbase64(value) {
  if (typeof value !== 'string' || value.length === 0 || value.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(value)) throw Error('响应签名格式不正确');
  return Uint8Array.from(atob(value), character => character.charCodeAt(0));
}

function publicPEM(bytes) {
  return '-----BEGIN PUBLIC KEY-----\n' + base64(bytes).match(/.{1,64}/g).join('\n') + '\n-----END PUBLIC KEY-----\n';
}

function publicDER(value) {
  if (typeof value !== 'string' || value.length > 4096) throw Error('沙箱平台公钥不正确');
  const match = value.trim().match(/^-----BEGIN PUBLIC KEY-----\s+([A-Za-z0-9+/=\s]+)\s+-----END PUBLIC KEY-----$/);
  if (!match) throw Error('沙箱平台公钥不正确');
  return unbase64(match[1].replace(/\s/g, ''));
}

function requireCrypto(crypto) {
  if (!crypto?.subtle) throw Error('此浏览器无法使用安全签名，请通过 HTTPS 打开文档或使用 Go SDK');
  return crypto.subtle;
}

export function createSigner(crypto = globalThis.crypto) {
  let pending;
  return {
    async sign(fields) {
      const subtle = requireCrypto(crypto);
      pending ||= subtle.generateKey({ name: 'RSASSA-PKCS1-v1_5', modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: 'SHA-256' }, true, ['sign', 'verify']);
      const pair = await pending;
      const signature = await subtle.sign('RSASSA-PKCS1-v1_5', pair.privateKey, encoder.encode(canonical(fields)));
      return { params: { ...fields, sign: base64(signature) }, public_key: publicPEM(await subtle.exportKey('spki', pair.publicKey)) };
    },
  };
}

export async function signSandboxRequest(slug, params, signer, now = Date.now()) {
  if (!ENDPOINTS.has(slug)) throw Error('此页面不提供可发送的接口');
  const fields = validateParams(params);
  fields.pid = '900001';
  fields.timestamp = String(Math.floor(now / 1000));
  fields.sign_type = 'RSA';
  const signed = await signer.sign(fields);
  return { endpoint: slug, params: signed.params, public_key: signed.public_key };
}

export async function verifySandboxEnvelope(envelope, expected, options = {}) {
  if (!envelope || envelope.environment !== 'documentation-sandbox' || envelope.request?.signature_verified !== true || envelope.request.method !== expected.method || envelope.request.path !== expected.path) throw Error('返回结果与当前文档沙箱请求不匹配');
  const body = envelope.response;
  if (!body || !Number.isInteger(body.code)) throw Error('响应业务代码格式不正确');
  const fields = responseFields(body);
  if (fields.sign_type !== 'RSA' || !/^\d+$/.test(fields.timestamp || '')) throw Error('响应签名或时间戳缺失');
  const timestamp = Number(fields.timestamp), current = Math.floor((options.now || Date.now)() / 1000);
  if (!Number.isSafeInteger(timestamp) || timestamp <= 0 || timestamp < current - 300 || timestamp > current + 300) throw Error('响应时间戳超出 300 秒窗口，请检查设备时间');
  const subtle = requireCrypto(options.crypto || globalThis.crypto);
  const key = await subtle.importKey('spki', publicDER(envelope.platform_public_key), { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-256' }, false, ['verify']);
  if (key.algorithm.modulusLength < 2048 || key.algorithm.modulusLength > 4096) throw Error('沙箱平台公钥长度不正确');
  const signature = unbase64(fields.sign);
  if (signature.byteLength !== key.algorithm.modulusLength / 8 || !await subtle.verify('RSASSA-PKCS1-v1_5', key, signature, encoder.encode(canonical(fields, true)))) throw Error('响应签名验证失败，不能使用此结果');
  return { ...envelope, unsignedStructures: Object.values(body).some(value => value !== null && typeof value === 'object') };
}

async function responseText(reply) {
  const contentType = reply.headers.get('content-type') || '';
  if (!/^application\/json(?:;|$)/i.test(contentType)) throw Error('沙箱未返回 JSON，请稍后重试');
  if (reply.body?.getReader) {
    const reader = reply.body.getReader(), decoder = new TextDecoder('utf-8', { fatal: true });
    let size = 0, output = '';
    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > MAX_REPLY_BYTES) { await reader.cancel(); throw Error('沙箱响应超过大小限制'); }
        output += decoder.decode(value, { stream: true });
      }
      return output + decoder.decode();
    } finally { reader.releaseLock(); }
  }
  const output = await reply.text();
  if (encoder.encode(output).byteLength > MAX_REPLY_BYTES) throw Error('沙箱响应超过大小限制');
  return output;
}

export async function sendSandboxRequest(input, options = {}) {
  const crypto = options.crypto || globalThis.crypto, signer = options.signer || createSigner(crypto);
  const controller = new AbortController(), timeout = setTimeout(() => controller.abort('timeout'), options.timeoutMs || 10000);
  const abort = () => controller.abort(options.signal.reason);
  if (options.signal?.aborted) controller.abort(options.signal.reason);
  else options.signal?.addEventListener('abort', abort, { once: true });
  const started = performance.now();
  try {
    const payload = await signSandboxRequest(input.slug, input.params, signer, (options.now || Date.now)());
    if (controller.signal.aborted) throw new DOMException('已取消请求', 'AbortError');
    const fetcher = options.fetcher || globalThis.fetch.bind(globalThis);
    const reply = await fetcher('/doc/playground/run', { method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: JSON.stringify(payload), credentials: 'omit', mode: 'same-origin', redirect: 'error', cache: 'no-store', referrerPolicy: 'no-referrer', signal: controller.signal });
    let envelope;
    try { envelope = JSON.parse(await responseText(reply)); }
    catch (error) { if (error.name === 'AbortError') throw error; throw Error(error.message === 'Unexpected end of JSON input' ? '沙箱响应不完整，请重试' : error.message); }
    if (controller.signal.aborted) throw new DOMException('已取消请求', 'AbortError');
    if (!reply.ok) throw Error(typeof envelope.error === 'string' ? envelope.error : '沙箱请求未通过校验（HTTP ' + reply.status + '）');
    const verified = await verifySandboxEnvelope(envelope, input, { crypto, now: options.now || Date.now });
    if (controller.signal.aborted) throw new DOMException('已取消请求', 'AbortError');
    return { envelope: verified, httpStatus: reply.status, headers: Object.fromEntries(reply.headers.entries()), durationMs: Math.round(performance.now() - started) };
  } finally {
    clearTimeout(timeout);
    options.signal?.removeEventListener('abort', abort);
  }
}

function exampleURL(meta) {
  const base = new URL(meta.origin || '/', globalThis.location?.origin || 'https://pay.example.com');
  if (!['https:', 'http:'].includes(base.protocol) || base.username || base.password || base.search || base.hash) throw Error('接口地址配置不正确');
  return new URL(meta.path.replace(/^\//, ''), base.href.replace(/\/?$/, '/'));
}

const shellQuote = value => "'" + String(value).replaceAll("'", "'\"'\"'") + "'";
const goMethods = { pay_create: 'Create', pay_query: 'Query', pay_refund: 'Refund', pay_refundquery: 'RefundQuery', pay_close: 'Close', pay_submit: 'SubmitURL' };

export function requestExample(meta, params, language = 'curl') {
  const values = validateParams(params), target = exampleURL(meta);
  const fields = { pid: '<MERCHANT_ID>', ...values, timestamp: '<CURRENT_UNIX_SECONDS>', sign_type: 'RSA', sign: '<RSA_SIGNATURE>' };
  if (language === 'go') {
    const rows = ['// client 由 Go SDK 配置商户服务端凭据；参见「Go SDK 与接入示例」。', 'params := make(url.Values)'];
    for (const key of Object.keys(values).sort(byteOrder)) rows.push('params.Set(' + JSON.stringify(key) + ', ' + JSON.stringify(values[key]) + ')');
    const method = goMethods[meta.slug];
    if (method === 'SubmitURL') rows.push('', 'signedURL, err := client.SubmitURL(params)', '// 按业务需要将用户导航至 signedURL；这里不发送请求。');
    else if (method) rows.push('', 'result, err := client.' + method + '(ctx, params)', '// 先处理 err，再按原业务单号核对结果。');
    else rows.push('', '// 此接口未封装在支付 Go SDK 中。', '// 按签名规则以服务端私钥签署 POST 表单，使用平台公钥验证响应。');
    return rows.join('\n');
  }
  const encoded = new URLSearchParams(fields).toString();
  if (language === 'http') return meta.method + ' ' + target.pathname + (meta.method === 'GET' ? '?' + encoded : '') + ' HTTP/1.1\nHost: ' + target.host + '\nContent-Type: application/x-www-form-urlencoded\n\n' + (meta.method === 'GET' ? '' : encoded);
  if (meta.method === 'GET') { target.search = encoded; return '# 将占位商户资料替换并重新签名；这里只展示接口格式。\ncurl --request GET ' + shellQuote(target.href); }
  return '# 将占位商户资料替换并重新签名；这里只展示接口格式。\ncurl --request POST ' + shellQuote(target.href) + ' \\\n  --header ' + shellQuote('Content-Type: application/x-www-form-urlencoded') + Object.entries(fields).map(([key, value]) => ' \\\n  --data-urlencode ' + shellQuote(key + '=' + value)).join('');
}

function text(node, value) { if (node) node.textContent = value; }
function toggle(node, hidden) { if (node) node.hidden = hidden; }
function on(target, name, listener, cleanups) { if (target) { target.addEventListener(name, listener); cleanups.push(() => target.removeEventListener(name, listener)); } }
function activeTabs(root, attribute, value) {
  for (const button of root.querySelectorAll('[' + attribute + ']')) {
    const active = button.getAttribute(attribute) === value;
    button.setAttribute('aria-selected', String(active));
    button.tabIndex = active ? 0 : -1;
    button.classList.toggle('is-active', active);
  }
}

async function copyText(doc, value, options) {
  const clipboard = options.clipboard || doc.defaultView?.navigator?.clipboard;
  try { if (clipboard?.writeText) { await clipboard.writeText(value); return true; } } catch {}
  const original = doc.activeElement, fallback = doc.createElement('textarea');
  fallback.className = 'doc-copy-fallback'; fallback.readOnly = true; fallback.tabIndex = -1; fallback.value = value;
  doc.body.append(fallback); fallback.select();
  let copied = false;
  try { copied = !!doc.execCommand?.('copy'); } finally { fallback.remove(); original?.focus(); }
  return copied;
}

function mountCopy(doc, options, cleanups) {
  for (const block of doc.querySelectorAll('.doc-article pre')) {
    const code = block.querySelector('code');
    if (!code || block.querySelector('.doc-copy-button')) continue;
    const button = doc.createElement('button'); button.type = 'button'; button.className = 'doc-copy-button'; button.textContent = '复制'; button.setAttribute('aria-label', '复制代码');
    on(button, 'click', async () => { const ok = await copyText(doc, code.textContent, options); text(button, ok ? '已复制' : '请手动选择代码'); }, cleanups);
    block.append(button); cleanups.push(() => button.remove());
  }
}

function mountSearch(doc, cleanups) {
  const dialog = doc.getElementById('doc-search-dialog');
  if (!dialog) return;
  const input = dialog.querySelector('[data-search-input]'), results = dialog.querySelector('[data-search-results]'), status = dialog.querySelector('[data-search-status]');
  if (!input || !results) return;
  const unique = new Map();
  for (const link of doc.querySelectorAll('[data-doc-link]')) {
    const href = link.getAttribute('href');
    if (!href?.startsWith('/doc/') || unique.has(href)) continue;
    const title = link.querySelector('.doc-nav-title')?.textContent.trim() || link.textContent.trim(), group = link.dataset.group || link.closest('details')?.querySelector('summary')?.textContent.trim() || '接入指南';
    unique.set(href, { href, title, group, searchable: (title + ' ' + group + ' ' + href + ' ' + (link.dataset.searchExtra || '')).toLowerCase() });
  }
  let priorFocus;
  const showResults = () => {
    const query = input.value.trim().toLowerCase(), items = [...unique.values()].filter(item => !query || item.searchable.includes(query)).slice(0, 12);
    results.replaceChildren();
    for (const item of items) {
      const li = doc.createElement('li'), link = doc.createElement('a'), title = doc.createElement('span'), detail = doc.createElement('span');
      link.className = 'doc-search-link'; link.href = item.href; title.className = 'doc-search-title'; title.textContent = item.title; detail.className = 'doc-search-meta'; detail.textContent = item.group;
      link.append(title, detail); li.append(link); results.append(li);
    }
    text(status, items.length ? '找到 ' + items.length + ' 个页面，使用方向键选择' : '没有匹配页面，请试试接口名称或路径');
    if (!items.length) { const li = doc.createElement('li'); li.className = 'doc-search-empty'; li.textContent = '未找到匹配文档'; results.append(li); }
  };
  const close = () => { if (typeof dialog.close === 'function' && dialog.open) dialog.close(); else dialog.removeAttribute('open'); priorFocus?.focus(); };
  const open = () => { priorFocus = doc.activeElement; input.value = ''; showResults(); if (typeof dialog.showModal === 'function') dialog.showModal(); else dialog.setAttribute('open', ''); input.focus(); };
  for (const button of doc.querySelectorAll('[data-search-open]')) on(button, 'click', open, cleanups);
  for (const button of dialog.querySelectorAll('[data-search-close]')) on(button, 'click', close, cleanups);
  on(input, 'input', showResults, cleanups);
  on(dialog, 'cancel', event => { event.preventDefault(); close(); }, cleanups);
  on(dialog, 'click', event => { if (event.target === dialog) close(); }, cleanups);
  on(dialog, 'keydown', event => {
    if (event.key === 'Escape') { event.preventDefault(); close(); return; }
    const links = [...results.querySelectorAll('a')];
    if (['ArrowDown', 'ArrowUp'].includes(event.key)) { event.preventDefault(); const at = links.indexOf(doc.activeElement), next = event.key === 'ArrowDown' ? at + 1 : at - 1; (next >= 0 && next < links.length ? links[next] : input).focus(); }
    if (event.key === 'Enter' && doc.activeElement === input && links[0]) { event.preventDefault(); links[0].click(); }
    if (event.key === 'Tab') {
      const focusable = [...dialog.querySelectorAll('button:not([disabled]),input:not([disabled]),a[href]')].filter(node => !node.hidden && !node.closest('[hidden]'));
      const first = focusable[0], last = focusable.at(-1);
      if (event.shiftKey && doc.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && doc.activeElement === last) { event.preventDefault(); first?.focus(); }
    }
  }, cleanups);
  on(doc, 'keydown', event => { if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); if (!dialog.open) open(); else close(); } }, cleanups);
}

function mountTOC(doc, cleanups) {
  const containers = [...doc.querySelectorAll('[data-doc-toc]')], headings = [...doc.querySelectorAll('.doc-article h2[id]')];
  for (const container of containers) {
    container.replaceChildren();
    for (const heading of headings) { const li = doc.createElement('li'), link = doc.createElement('a'); link.href = '#' + encodeURIComponent(heading.id); link.textContent = heading.textContent.trim(); li.append(link); container.append(li); }
  }
  const Observer = doc.defaultView?.IntersectionObserver;
  if (Observer && headings.length) {
    const observer = new Observer(entries => {
      const current = entries.filter(entry => entry.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
      if (!current) return;
      for (const link of doc.querySelectorAll('[data-doc-toc] a')) { if (decodeURIComponent(link.hash.slice(1)) === current.target.id) link.setAttribute('aria-current', 'location'); else link.removeAttribute('aria-current'); }
    }, { rootMargin: '-90px 0px -55% 0px' });
    headings.forEach(heading => observer.observe(heading)); cleanups.push(() => observer.disconnect());
  }
}

function mountConsole(root, doc, options, cleanups) {
  const select = name => root.querySelector('[data-' + name + ']'), fields = [...root.querySelectorAll('[data-param-name]')];
  const meta = { slug: root.dataset.endpointSlug, method: root.dataset.method, path: root.dataset.path, origin: root.dataset.apiOrigin };
  const send = select('console-send'), cancel = select('console-cancel'), reset = select('console-reset'), status = select('console-status'), error = select('console-error'), empty = select('console-empty'), result = select('console-result');
  const defaults = new Map(fields.map(field => [field, field.value]));
  const signer = options.signer || createSigner(options.crypto || globalThis.crypto);
  let language = 'curl', responseTab = 'body', responseVersion = 0, controller, busy = false, disposed = false;
  const responseCopy = select('response-copy');
  const copyFeedback = (label = '复制响应') => { text(responseCopy, label); responseCopy?.setAttribute('aria-label', label === '复制响应' ? '复制当前响应内容' : label); };
  const disclosure = root.closest('[data-console-disclosure]') || select('console-disclosure'), media = options.matchMedia || doc.defaultView?.matchMedia?.bind(doc.defaultView);
  if (disclosure && media?.('(max-width: 1279px)').matches) disclosure.open = false;
  const values = () => {
    const output = Object.create(null);
    for (const field of fields) { if (Object.hasOwn(output, field.dataset.paramName)) throw Error('请求字段重复'); output[field.dataset.paramName] = field.value; }
    return validateParams(output);
  };
  const setBusy = value => { busy = value; root.setAttribute('aria-busy', String(value)); if (send) send.disabled = value; if (reset) reset.disabled = value; toggle(cancel, !value); for (const field of fields) field.disabled = value; };
  const clearResult = (message = '发送沙箱请求后查看响应') => { responseVersion++; copyFeedback(); toggle(result, true); toggle(empty, false); text(empty, message); text(select('response-signature'), '尚未验签'); text(select('request-verification'), '临时 RSA 凭据'); text(select('response-http'), '—'); text(select('response-duration'), '—'); };
  const examples = () => { try { text(select('request-code'), requestExample(meta, values(), language)); } catch (failure) { text(select('request-code'), failure.message); } activeTabs(root, 'data-request-lang', language); };
  const switchResponse = value => { responseVersion++; copyFeedback(); responseTab = value; activeTabs(root, 'data-response-tab', value); for (const panel of root.querySelectorAll('[data-response-panel]')) panel.hidden = panel.dataset.responsePanel !== value; };
  const validate = () => {
    let first;
    for (const field of fields) {
      const invalid = field.required && field.value.trim() === '';
      field.setAttribute('aria-invalid', String(invalid));
      const row = [...root.querySelectorAll('[data-param-error]')].find(item => item.dataset.paramError === field.dataset.paramName);
      text(row, invalid ? (field.dataset.label || field.dataset.paramName) + '为必填项' : '');
      toggle(row, !invalid);
      if (invalid && !first) first = field;
    }
    if (first) { first.focus(); return false; }
    return true;
  };
  for (const field of fields) on(field, 'input', () => { if (busy) return; root.dataset.state = 'idle'; toggle(error, true); clearResult('参数已更改，请重新发送'); text(status, '参数已更改，请重新发送'); field.removeAttribute('aria-invalid'); const row = [...root.querySelectorAll('[data-param-error]')].find(item => item.dataset.paramError === field.dataset.paramName); text(row, ''); toggle(row, true); examples(); }, cleanups);
  for (const button of root.querySelectorAll('[data-request-lang]')) on(button, 'click', () => { language = button.dataset.requestLang; examples(); }, cleanups);
  for (const button of root.querySelectorAll('[data-response-tab]')) on(button, 'click', () => switchResponse(button.dataset.responseTab), cleanups);
  on(select('console-copy'), 'click', async event => { const button = event.currentTarget, ok = await copyText(doc, select('request-code')?.textContent || '', options); text(button, ok ? '已复制' : '请手动选择代码'); }, cleanups);
  on(responseCopy, 'click', async () => {
    if (busy || disposed || result?.hidden !== false) return;
    const version = responseVersion, target = { body: 'response-code', headers: 'response-headers', request: 'response-request' }[responseTab];
    let copied = false;
    try { copied = await copyText(doc, select(target)?.textContent || '', options); } catch {}
    if (!disposed && !result.hidden && version === responseVersion) copyFeedback(copied ? '已复制' : '复制失败，请手动选择内容');
  }, cleanups);
  on(reset, 'click', () => { if (busy) return; for (const field of fields) { field.value = defaults.get(field); field.removeAttribute('aria-invalid'); } for (const row of root.querySelectorAll('[data-param-error]')) { text(row, ''); toggle(row, true); } root.dataset.state = 'idle'; toggle(error, true); clearResult(); text(status, '已恢复示例参数'); examples(); }, cleanups);
  on(cancel, 'click', () => { controller?.abort(); text(status, '正在取消等待…'); }, cleanups);
  on(send, 'click', async () => {
    if (busy || disposed) return;
    if (!validate()) { root.dataset.state = 'error'; text(status, '请填写必填参数'); return; }
    controller = new AbortController(); root.dataset.state = 'loading'; setBusy(true); toggle(error, true); clearResult('正在签署并发送文档沙箱请求…'); text(status, '正在使用临时 RSA 凭据发送请求…');
    try {
      const received = await sendSandboxRequest({ ...meta, params: values() }, { ...options, signer, signal: controller.signal });
      if (disposed) return;
      const body = received.envelope.response;
      root.dataset.state = body.code === 0 ? 'success' : 'error';
      text(select('response-code'), JSON.stringify(body, null, 2));
      text(select('response-headers'), Object.entries(received.headers).map(([key, value]) => key + ': ' + value).join('\n'));
      text(select('response-request'), JSON.stringify({ environment: received.envelope.environment, ...received.envelope.request }, null, 2));
      text(select('response-http'), 'HTTP ' + received.httpStatus); text(select('response-duration'), received.durationMs + ' ms'); text(select('request-verification'), '请求签名已验证');
      text(select('response-signature'), received.envelope.unsignedStructures ? '仅顶层标量验签通过 · data 完整性依赖 HTTPS' : '响应验签通过');
      toggle(empty, true); toggle(result, false); switchResponse(responseTab);
      text(status, body.code === 0 ? '文档沙箱已响应，不会创建真实订单或移动资金' : '沙箱返回业务错误，响应验签通过；请检查参数');
      if (body.code !== 0) { text(error, typeof body.msg === 'string' ? body.msg : '请按接口说明调整参数后重试'); toggle(error, false); }
    } catch (failure) {
      if (disposed) return;
      root.dataset.state = 'error'; clearResult(failure.name === 'AbortError' ? '已取消本次等待' : '本次请求未得到可验证的结果');
      const message = failure.name === 'AbortError' ? '请求已取消或超时，可手动重新发送文档沙箱请求' : failure.message || '请求未完成，请稍后重试';
      text(error, message); toggle(error, false); text(status, message);
    } finally { if (!disposed) setBusy(false); }
  }, cleanups);
  for (const attribute of ['data-request-lang', 'data-response-tab']) on(root, 'keydown', event => {
    const target = event.target.closest?.('[' + attribute + ']');
    if (!target || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    const buttons = [...root.querySelectorAll('[' + attribute + ']')], at = buttons.indexOf(target);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (at + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
    event.preventDefault(); buttons[next].click(); buttons[next].focus();
  }, cleanups);
  cleanups.push(() => { disposed = true; controller?.abort(); });
  examples(); switchResponse(responseTab);
}

export function mountDocumentation(doc = document, options = {}) {
  const cleanups = [];
  mountCopy(doc, options, cleanups); mountSearch(doc, cleanups); mountTOC(doc, cleanups);
  for (const root of doc.querySelectorAll('[data-api-console]')) mountConsole(root, doc, options, cleanups);
  return { destroy() { for (const cleanup of cleanups.reverse()) cleanup(); } };
}

if (typeof document !== 'undefined') {
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', () => mountDocumentation(document), { once: true });
  else mountDocumentation(document);
}
