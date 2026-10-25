const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const builder = path.join(__dirname, '../build.mjs');
test('reproducible build emits two closed, disabled native projects and refuses overwrite', () => {
  assert.ok(fs.existsSync(builder), 'native mini-program builder is not implemented');
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'goepay-mini81-test-'));
  try {
    const output = path.join(temp, 'output');
    const first = spawnSync(process.execPath, [builder, '--output', output], { encoding: 'utf8' }); assert.equal(first.status, 0, first.stderr);
    const manifest = JSON.parse(fs.readFileSync(path.join(output, 'MANIFEST.json')));
    assert.equal(manifest.schema, 'goepay-mini-client-v1'); assert.equal(manifest.default_external_io_enabled, false);
    for (const provider of ['wxpay', 'alipay']) {
      const root = path.join(output, provider);
      const config = require(path.join(root, 'config.js')); assert.equal(config.enabled, false); assert.equal(config.origin, 'https://pay.example.invalid');
      const app = JSON.parse(fs.readFileSync(path.join(root, 'app.json'))); assert.deepEqual(app.pages, ['pages/pay/pay']);
      const suffix = provider === 'wxpay' ? 'wxml' : 'axml'; const page = fs.readFileSync(path.join(root, `pages/pay/pay.${suffix}`), 'utf8');
      assert.match(page, /确认支付/); assert.match(page, /money/); assert.match(page, /canPay/); assert.match(page, /busy/); assert.doesNotMatch(page, /web-view|script|https?:\/\//);
      const utils = fs.readFileSync(path.join(root, 'utils/page.js'), 'utf8'); assert.match(utils, /require\('\.\/payment-client\.js'\)/);
      assert.equal(fs.existsSync(path.join(root, 'tests')), false);
    }
    for (const item of manifest.files) {
      const contents = fs.readFileSync(path.join(output, item.path)); assert.equal(crypto.createHash('sha256').update(contents).digest('hex'), item.sha256);
      assert.doesNotMatch(contents.toString(), /BEGIN (?:RSA )?PRIVATE KEY|appsecret|synthetic-login-code/);
    }
    const secondOutput = path.join(temp, 'second'); const second = spawnSync(process.execPath, [builder, '--output', secondOutput], { encoding: 'utf8' }); assert.equal(second.status, 0, second.stderr);
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(secondOutput, 'MANIFEST.json'))), manifest);
    const rejected = spawnSync(process.execPath, [builder, '--output', output], { encoding: 'utf8' }); assert.notEqual(rejected.status, 0);
    const relative = spawnSync(process.execPath, [builder, '--output', 'relative-mini-output'], { cwd: temp, encoding: 'utf8' }); assert.notEqual(relative.status, 0);
    for (const item of manifest.files) assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(output, item.path))).digest('hex'), item.sha256);
  } finally {
    assert.equal(path.dirname(path.resolve(temp)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(temp).startsWith('goepay-mini81-test-'));
    fs.rmSync(temp, { recursive: true, force: true });
  }
});
