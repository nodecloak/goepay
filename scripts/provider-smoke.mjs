// Local simulation only: never contacts WeChat, Alipay, or a merchant endpoint.
import fs from 'node:fs';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
const base = process.env.GOEPAY_SMOKE_URL ?? 'http://127.0.0.1:8180';
assert.match(base, /^http:\/\/(127\.0\.0\.1|localhost):\d+$/);
const config = Object.fromEntries(fs.readFileSync(new URL('../.env', import.meta.url), 'utf8').split(/\r?\n/).filter(x => x && !x.startsWith('#')).map(x => { const i = x.indexOf('='); return [x.slice(0, i), x.slice(i + 1)] }));
const json = async (path, options = {}) => { const r = await fetch(base + path, options); const data = await r.json(); return { r, data } };
const auth = await json('/console/v1/merchant/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: 'merchant', password: config.GOEPAY_SEED_PASSWORD }) });
assert.equal(auth.r.status, 200, 'local login required');
const cookie = auth.r.headers.get('set-cookie').split(';')[0];
assert.equal((await json('/console/v1/merchant/dashboard', { headers: { Cookie: cookie } })).data.data.sandbox, true, 'sandbox required');
const canonical = p => Object.entries(p).filter(([k, v]) => !['sign', 'sign_type'].includes(k) && v !== '').sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([k, v]) => `${k}=${v}`).join('&');
const results = [];
for (const type of ['wxpay', 'alipay']) {
 const p = { pid: '1000', type, out_trade_no: `provider-smoke-${crypto.randomUUID()}`, name: '本地通道闭环测试', money: '1.23', sign_type: 'MD5' };
 p.sign = crypto.createHash('md5').update(canonical(p) + config.GOEPAY_SEED_API_KEY).digest('hex');
 const created = await json('/mapi.php', { method: 'POST', body: new URLSearchParams(p) });
 assert.equal(created.data.code, 1, 'provider create failed');
 const checkout = await fetch(new URL(created.data.payurl, base));
 assert.equal(checkout.status, 200, `${type} checkout`);
 const html = await checkout.text();
 const match = html.match(/http:\/\/(?:127\.0\.0\.1|localhost):\d+\/simulation\/pay\?token=[A-Za-z0-9_.%~-]+/);
 assert.ok(match, 'only a local simulation material may be submitted');
 const payment = new URL(match[0]);
 assert.equal(payment.origin, new URL(base).origin);
 assert.equal((await fetch(`${base}/checkout/${created.data.trade_no}`)).status, 403, 'unsigned checkout accepted');
 if (type === 'wxpay') {
  const qr = new URL(created.data.payurl, base); qr.pathname += '/qr.png';
  const png = await fetch(qr); assert.equal(png.status, 200); assert.equal(png.headers.get('content-type'), 'image/png');
  assert.deepEqual([...new Uint8Array(await png.arrayBuffer()).slice(0, 8)], [137, 80, 78, 71, 13, 10, 26, 10]);
 }
 for (let i = 0; i < 2; i++) assert.equal((await fetch(payment, { method: 'POST' })).status, 200, 'local callback rejected');
 const query = await json(`/api.php?${new URLSearchParams({ act: 'order', pid: '1000', key: config.GOEPAY_SEED_API_KEY, trade_no: created.data.trade_no })}`);
 assert.equal(query.data.status, 1); assert.equal(query.data.money, '1.23');
 const entries = await json(`/console/v1/merchant/ledger?q=${created.data.trade_no}`, { headers: { Cookie: cookie } });
 assert.equal(entries.data.data.total, 1, 'duplicate callback created multiple ledger credits');
 assert.equal(entries.data.data.items[0].amount, '123');
 const refundHeaders = { Cookie: cookie, 'X-CSRF-Token': auth.data.data.csrf_token, 'Content-Type': 'application/json' };
 let refundId;
 for (let i = 0; i < 2; i++) {
  const refunded = await json(`/console/v1/merchant/orders/${created.data.trade_no}/refund`, { method: 'POST', headers: refundHeaders, body: JSON.stringify({ amount: '0.23', external_no: `smoke-refund-${created.data.trade_no}` }) });
  assert.equal(refunded.r.status, 200); assert.equal(refunded.data.data.status, 'succeeded');
  if (refundId) assert.equal(refunded.data.data.id, refundId); else refundId = refunded.data.data.id;
 }
 assert.equal((await json(`/console/v1/merchant/orders/${created.data.trade_no}/close`, { method: 'POST', headers: refundHeaders, body: '{}' })).r.status, 422, 'paid order closed');
 results.push({ provider: type, create: true, checkout: true, duplicate_callback_once: true, paid_query: true, local_refund_once: true });
}
await json('/console/v1/merchant/logout', { method: 'POST', headers: { Cookie: cookie, 'X-CSRF-Token': auth.data.data.csrf_token } });
console.log(JSON.stringify({ mode: 'local simulation, no real funds', results, result: 'passed' }));
