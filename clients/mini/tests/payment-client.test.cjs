const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const file = path.join(__dirname, '../src/payment-client.cjs');
let createClient;
test.beforeEach(() => { if (fs.existsSync(file)) { delete require.cache[require.resolve(file)]; createClient = require(file).createClient; } });
const origin = 'https://pay.example.invalid';
const state = 'a'.repeat(64);
const trade = '202610240000000001';
const appIDs = { wxpay: 'wxsyntheticapp', alipay: '2026000000000001' };
const launch = (provider = 'wxpay', suffix = '') => ({ money: '12.34', url: `${origin}/oauth/mini/complete/${provider}/7/${trade}/?state=${state}${suffix}` });
const checkout = `${origin}/checkout/${trade}?expires=1792850000&token=${'b'.repeat(43)}`;
const wxMaterial = () => ({ appId: appIDs.wxpay, timeStamp: '1792850000', nonceStr: 'syntheticnonce', package: 'prepay_id=syntheticprepay', signType: 'MD5', paySign: 'A'.repeat(32) });
function envelope(provider) {
  return { code: 0, data: { trade_no: trade, checkout_url: checkout, attempt: { id: '1', trade_no: trade, channel_id: '7', state: 'pending', method: provider === 'wxpay' ? 'jsapi' : 'mini', material_type: 'jsapi', material: provider === 'wxpay' ? JSON.stringify(wxMaterial()) : '2026102422000000000000000001', provider_trade_no: provider === 'wxpay' ? '' : '2026102422000000000000000001' } } };
}
test('strict launch rejects foreign origin, provider, channel, extra query, double encoding and unsafe money before login', async () => {
  const bad = [
    { url: launch().url.replace(origin, 'https://evil.example.invalid') },
    { url: launch().url.replace('wxpay', 'alipay') }, { url: launch().url.replace('/7/', '/07/') },
    { url: launch().url.replace('/7/', '/8/') }, { url: launch().url + '&state=b' },
    { url: launch().url + '&code=not-allowed' }, { url: launch().url + '#fragment' },
    { url: launch().url.replace('https://', 'http://') }, { url: launch().url.replace('https://', 'https://user@') },
    { url: encodeURIComponent(encodeURIComponent(launch().url)) }, { url: launch().url.replace(trade, '../foreign') },
    { money: '1e2' }, { money: '-1.00' }, { money: '0.00' }, { money: '<script>' },
  ];
  for (const change of bad) {
    const f = fixture(); await f.start({ ...launch(), ...change });
    assert.equal(f.calls.filter(x => ['login', 'post', 'pay'].includes(x[0])).length, 0); assert.equal(f.views.at(-1).phase, 'error');
  }
});
test('launch permits one SDK URL decoding and never uses display money in exchange', async () => {
  const f = fixture(); await f.start({ money: '99.99', url: encodeURIComponent(launch().url) });
  assert.equal(f.calls.find(x => x[0] === 'post')[1].url, launch().url);
  assert.equal(f.views.at(-1).money, '99.99'); assert.equal(f.calls.find(x => x[0] === 'post')[1].data.money, undefined);
});
test('unknown attempts cannot invoke payment or authorize again', async () => {
  const f = fixture('wxpay', { sdk: { request(o) { f.calls.push(['post', o]); const body = envelope('wxpay'); body.data.attempt.state = 'unknown'; o.success({ statusCode: 200, data: body }); } } });
  await f.start(launch()); await f.start(launch());
  assert.equal(f.calls.filter(x => x[0] === 'pay').length, 0); assert.equal(f.calls.filter(x => x[0] === 'login').length, 1);
  assert.equal(f.views.at(-1).phase, 'verification_required');
});
test('invalid response binding, transport failure and forged material never invoke SDK payment', async () => {
  const cases = [
    b => { b.code = '0'; }, b => { b.data.trade_no = 'foreign'; }, b => { b.data.attempt.channel_id = '8'; },
    b => { b.data.attempt.trade_no = 'foreign'; }, b => { b.data.attempt.method = 'app'; },
    b => { b.data.attempt.material_type = 'html'; }, b => { b.data.attempt.material = JSON.stringify({ ...wxMaterial(), appId: 'foreign' }); },
    b => { b.data.attempt.material = JSON.stringify({ ...wxMaterial(), signType: 'bogus' }); },
    b => { b.data.checkout_url = 'https://evil.example.invalid/'; }, b => { b.data.checkout_url = checkout + '&return=https://evil.example.invalid'; },
  ];
  let count = 0;
  for (const mutate of cases) {
    const f = fixture('wxpay', { sdk: { request(o) { f.calls.push(['post', o]); const body = envelope('wxpay'); mutate(body); o.success({ statusCode: 200, data: body }); } } });
    const opts = launch(); opts.url = opts.url.replace(state, (++count).toString(16).padStart(64, 'c')); await f.start(opts);
    assert.equal(f.calls.filter(x => x[0] === 'pay').length, 0); assert.notEqual(f.views.at(-1).phase, 'paying');
    assert.doesNotMatch(JSON.stringify(f.views), /evil\.example|synthetic-login-code|prepay_id/);
  }
});
for (const provider of ['wxpay', 'alipay']) {
  test(`${provider}: SDK cancellation remains terminal and no server-success claim is displayed`, async () => {
    const cancel = o => provider === 'wxpay' ? o.fail({ errMsg: 'requestPayment:fail cancel' }) : o.success({ resultCode: '6001' });
    const f = fixture(provider, { sdk: { requestPayment: cancel, tradePay: cancel } });
    await f.start(launch(provider)); await f.start(launch(provider));
    assert.equal(f.views.at(-1).phase, 'cancelled'); assert.equal(f.calls.filter(x => x[0] === 'post').length, 1);
    assert.doesNotMatch(f.views.at(-1).message, /支付成功/);
  });
}
test('timed-out exchange aborts once and a late accepted response never initiates payment', async () => {
  let tick, late, aborted = 0;
  const timers = { setTimeout(fn) { tick = fn; return 1; }, clearTimeout() {} };
  const f = fixture('wxpay', { timers, sdk: { request(o) { f.calls.push(['post', o]); late = o.success; return { abort() { aborted++; } }; } } });
  const running = f.start(launch()); await new Promise(resolve => setImmediate(resolve)); tick(); await running;
  late({ statusCode: 200, data: envelope('wxpay') }); await f.start(launch());
  assert.equal(aborted, 1); assert.equal(f.calls.filter(x => x[0] === 'pay').length, 0); assert.equal(f.calls.filter(x => x[0] === 'post').length, 1);
  assert.equal(f.views.at(-1).phase, 'verification_required');
});
test('unload aborts pending exchange and suppresses all subsequent views and SDK actions', async () => {
  let late, aborted = 0;
  const f = fixture('wxpay', { sdk: { request(o) { late = o.success; return { abort() { aborted++; } }; } } });
  const running = f.start(launch()); await new Promise(resolve => setImmediate(resolve)); const before = f.views.length;
  f.dispose(); late({ statusCode: 200, data: envelope('wxpay') }); await running;
  assert.equal(aborted, 1); assert.equal(f.views.length, before); assert.equal(f.calls.filter(x => x[0] === 'pay').length, 0);
});
test('Alipay only accepts an original string tradeNO, never numeric or different provider material', async () => {
  let counter = 50;
  for (const mutate of [a => { a.material = 1234567890123456; a.provider_trade_no = a.material; }, a => { a.provider_trade_no = '202600000000000000'; }, a => { a.id = 1; }]) {
    const f = fixture('alipay', { sdk: { request(o) { const body = envelope('alipay'); mutate(body.data.attempt); o.success({ status: 200, data: body }); } } });
    const opts = launch('alipay'); opts.url = opts.url.replace(state, (++counter).toString().padStart(64, 'e')); await f.start(opts);
    assert.equal(f.calls.filter(x => x[0] === 'pay').length, 0);
  }
});
function fixture(provider = 'wxpay', changes = {}) {
  assert.equal(typeof createClient, 'function', 'one-use mini client is not implemented');
  const calls = [], views = [];
  const sdk = {
    getAccountInfoSync() { calls.push(['app']); return { miniProgram: { appId: appIDs.wxpay } }; },
    getAppIdSync() { calls.push(['app']); return { appId: appIDs.alipay }; },
    login(o) { calls.push(['login']); o.success({ code: 'synthetic-login-code' }); },
    getAuthCode(o) { calls.push(['login', o.scopes]); o.success({ authCode: 'synthetic-auth-code' }); },
    request(o) { calls.push(['post', o]); o.success({ statusCode: 200, status: 200, data: envelope(provider) }); return { abort() { calls.push(['abort']); } }; },
    requestPayment(o) { calls.push(['pay', o]); o.success({ errMsg: 'requestPayment:ok' }); },
    tradePay(o) { calls.push(['pay', o]); o.success({ resultCode: '9000' }); },
    ...changes.sdk,
  };
  const config = { enabled: true, origin, appId: appIDs[provider], channelIDs: ['7'], ...changes.config };
  const c = createClient(provider, sdk, config, x => views.push(x), changes.timers);
  return { ...c, calls, views, sdk, config };
}
test('mini one-use client exists', () => assert.equal(typeof createClient, 'function', 'one-use mini client is not implemented'));
for (const provider of ['wxpay', 'alipay']) {
  test(`${provider}: one code, JSON POST, original SDK payment, verification required`, async () => {
    const f = fixture(provider); await f.start(launch(provider));
    assert.equal(f.calls.filter(x => x[0] === 'login').length, 1);
    const request = f.calls.find(x => x[0] === 'post')[1];
    assert.equal(request.method, 'POST'); assert.equal(request.url, launch(provider).url);
    assert.deepEqual(request.data, { code: provider === 'wxpay' ? 'synthetic-login-code' : 'synthetic-auth-code' });
    assert.equal((request.header || request.headers)['Content-Type'], 'application/json');
    const pay = f.calls.find(x => x[0] === 'pay')[1];
    if (provider === 'wxpay') { for (const [key, value] of Object.entries(wxMaterial())) if (key !== 'appId') assert.equal(pay[key], value); assert.equal(pay.appId, undefined); }
    else { assert.equal(pay.tradeNO, envelope(provider).data.attempt.provider_trade_no); assert.equal(pay.orderStr, undefined); }
    assert.equal(f.views.at(-1).phase, 'verification_required');
    assert.equal(f.views.at(-1).checkoutURL, checkout);
    assert.match(f.views.at(-1).message, /查单|核实/);
    assert.doesNotMatch(JSON.stringify(f.views), /synthetic-login-code|synthetic-auth-code|prepay_id|paySign|state=/);
  });
  test(`${provider}: disabled and wrong actual application do not authorize or request`, async () => {
    for (const changes of [{ config: { enabled: false } }, { config: { appId: 'foreign-application' } }]) {
      const f = fixture(provider, changes); await f.start(launch(provider));
      assert.equal(f.calls.filter(x => ['login', 'post', 'pay'].includes(x[0])).length, 0); assert.equal(f.views.at(-1).phase, 'error');
    }
  });
  test(`${provider}: process-wide state claim blocks concurrent pages and double taps`, async () => {
    let release; const f = fixture(provider, { sdk: { request(o) { f.calls.push(['post', o]); release = () => o.success({ statusCode: 200, status: 200, data: envelope(provider) }); } } });
    const a = f.start(launch(provider)); const b = f.start(launch(provider));
    await new Promise(resolve => setImmediate(resolve));
    const second = fixture(provider); await second.start(launch(provider));
    assert.equal(second.calls.filter(x => x[0] === 'login').length, 0);
    release(); await Promise.all([a, b]); assert.equal(f.calls.filter(x => x[0] === 'post').length, 1); assert.equal(f.calls.filter(x => x[0] === 'pay').length, 1);
  });
}
