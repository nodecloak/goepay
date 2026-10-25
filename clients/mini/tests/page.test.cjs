const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const source = path.join(__dirname, '../src/page.cjs');
test('native pages defer SDK authorization until explicit click and block repeated clicks', async () => {
  assert.ok(fs.existsSync(source), 'native payment page is not implemented');
  const { createPage } = require(source);
  let logins = 0, release;
  const sdk = { getAccountInfoSync() { return { miniProgram: { appId: 'wxsyntheticapp' } }; }, login(o) { logins++; release = o; } };
  const p = createPage('wxpay', sdk, { enabled: true, origin: 'https://pay.example.invalid', appId: 'wxsyntheticapp', channelIDs: ['7'] });
  p.setData = values => { p.data = { ...p.data, ...values }; };
  p.onLoad({ money: '12.34', url: `https://pay.example.invalid/oauth/mini/complete/wxpay/7/202610240000000002/?state=${'f'.repeat(64)}` });
  assert.equal(logins, 0); assert.equal(p.data.money, '12.34'); assert.equal(p.data.canPay, true);
  const active = p.onPay(); p.onPay(); assert.equal(logins, 1); assert.equal(p.data.busy, true);
  const before = JSON.stringify(p.data); p.onUnload(); release.success({ code: 'late-code' }); await active;
  assert.equal(JSON.stringify(p.data), before);
});
test('disabled config and invalid launch render fixed messages without SDK calls or URL/code in data', () => {
  assert.ok(fs.existsSync(source), 'native payment page is not implemented');
  const { createPage } = require(source);
  for (const enabled of [false, true]) {
    const p = createPage('alipay', {}, { enabled, origin: 'https://pay.example.invalid', appId: '2026000000000001', channelIDs: ['7'] });
    p.setData = values => { p.data = { ...p.data, ...values }; };
    p.onLoad({ money: '12.34', url: 'https://evil.example.invalid/?code=secret&state=secret' });
    assert.equal(p.data.canPay, false); assert.match(p.data.message, /不可用|未配置/);
    assert.doesNotMatch(JSON.stringify(p.data), /evil\.example|code=|state=/); p.onUnload();
  }
});
