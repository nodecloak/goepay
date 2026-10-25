'use strict';
// Test bridge only: the native SDK is replaced and all HTTP is forced to the
// Go test's loopback listener. This file is excluded from distributable builds.
const fs = require('node:fs');
const http = require('node:http');
const { createClient } = require('../src/payment-client.cjs');
async function main() {
  const raw = fs.readFileSync(0, 'utf8');
  if (raw.length > 8192) throw new Error('invalid local contract input');
  const input = JSON.parse(raw);
  if (!/^http:\/\/127\.0\.0\.1:[1-9][0-9]{0,4}$/.test(input.localServer) || !['wxpay', 'alipay'].includes(input.provider)) throw new Error('loopback only');
  let posts = 0, logins = 0, payments = 0, phase;
  const sdk = {
    getAccountInfoSync() { return { miniProgram: { appId: input.appId } }; },
    getAppIdSync() { return { appId: input.appId }; },
    login(o) { logins++; o.success({ code: 'mini81-synthetic-code' }); },
    getAuthCode(o) { if (JSON.stringify(o.scopes) !== '["auth_base"]') throw new Error('unexpected scope'); logins++; o.success({ authCode: 'mini81-synthetic-code' }); },
    request(o) {
      if (!o.url.startsWith('https://pay.example.invalid/oauth/mini/complete/') || o.method !== 'POST') throw new Error('invalid request');
      posts++;
      const destination = input.localServer + o.url.slice('https://pay.example.invalid'.length);
      const request = http.request(destination, { method: 'POST', headers: o.header || o.headers }, response => {
        let body = '';
        response.setEncoding('utf8');
        response.on('data', part => { body += part; if (body.length > 65536) request.destroy(new Error('response limit')); });
        response.on('end', () => { try { o.success({ statusCode: response.statusCode, status: response.statusCode, data: JSON.parse(body) }); } catch (_) { o.fail({}); } });
      });
      request.on('error', () => o.fail({}));
      request.end(JSON.stringify(o.data));
      return { abort() { request.destroy(); } };
    },
    requestPayment(o) { if (!o.package.startsWith('prepay_id=') || o.appId || !o.paySign) throw new Error('invalid original material'); payments++; o.success({ errMsg: 'requestPayment:ok' }); },
    tradePay(o) { if (!/^[0-9]{16,64}$/.test(o.tradeNO) || o.orderStr) throw new Error('invalid original tradeNO'); payments++; o.success({ resultCode: '9000' }); },
  };
  const client = createClient(input.provider, sdk, { enabled: true, origin: 'https://pay.example.invalid', appId: input.appId, channelIDs: ['7'] }, state => { phase = state.phase; });
  const launch = { money: input.money, url: input.completionURL };
  await client.start(launch); await client.start(launch); client.dispose();
  process.stdout.write(JSON.stringify({ phase, posts, logins, payments }));
  if (phase !== 'verification_required' || posts !== 1 || logins !== 1 || payments !== 1) process.exitCode = 1;
}
main().catch(() => { process.stderr.write('Local mini client contract failed\n'); process.exitCode = 1; });
