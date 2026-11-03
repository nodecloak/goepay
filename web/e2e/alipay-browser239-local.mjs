import { chromium, expect } from '@playwright/test';
import { createHash } from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const fixture = JSON.parse(process.env.ALIPAY_BROWSER239_FIXTURE ?? '{}');
const artifacts = path.resolve(process.env.GOEPAY_ALIPAY_BROWSER239_ARTIFACTS ?? '');
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const owned = path.join(root, '.tools', 'alipay-checkout239', 'owned-run');
const inside = (base, target) => {
  const relative = path.relative(base, target);
  return relative !== '..' && !relative.startsWith('..' + path.sep) && !path.isAbsolute(relative);
};
if (!process.env.GOEPAY_ALIPAY_BROWSER239_ARTIFACTS || !inside(owned, artifacts)) throw Error('explicit owned browser239 artifact directory required');
if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(fixture.api ?? '')) throw Error('only exact loopback HTTP API is permitted');
for (const key of ['gatewayLocal', 'cashierLocal']) if (!/^https:\/\/127\.0\.0\.1:\d+$/.test(fixture[key] ?? '')) throw Error('only exact loopback TLS provider peers are permitted');
for (const method of ['page', 'wap']) {
  if (!/^\d+$/.test(fixture[method + 'Trade'] ?? '') || !fixture[method + 'Checkout']?.startsWith(fixture.api + '/checkout/' + fixture[method + 'Trade'] + '?')) throw Error('original synthetic order checkout required');
}
await fs.mkdir(artifacts, { recursive: true });
if (!inside(await fs.realpath(owned), await fs.realpath(artifacts))) throw Error('artifact symlink escaped the owned directory');

const report = { syntheticOnly: true, amount: '66.50', actualExternalNetworkRequests: 0, virtualOfficialOrigins: ['https://openapi.alipay.com', 'https://excashier.alipay.com'], loopbackPeers: [fixture.api, fixture.gatewayLocal, fixture.cashierLocal], scenarios: [], submitted: [], navigations: [], merchantReturns: [], requestFailures: [], errors: [], forbidden: [] };
const gatewayPort = new URL(fixture.gatewayLocal).port;
const cashierPort = new URL(fixture.cashierLocal).port;
const hostRules = `MAP openapi.alipay.com:443 127.0.0.1:${gatewayPort}, MAP excashier.alipay.com:443 127.0.0.1:${cashierPort}, MAP merchant.example.invalid:443 127.0.0.1:${cashierPort}, MAP * ~NOTFOUND, EXCLUDE localhost, EXCLUDE 127.0.0.1`;
const browser = await chromium.launch({ headless: true, args: ['--no-proxy-server', '--disable-background-networking', '--host-resolver-rules=' + hostRules] });
const contexts = [];
const gatewayOrigin = 'https://openapi.alipay.com';
const cashierOrigin = 'https://excashier.alipay.com';
const merchantOrigin = 'https://merchant.example.invalid';
const merchantMD5 = params => {
  const pairs = [...params.entries()].filter(([key, value]) => key !== 'sign' && key !== 'sign_type' && value !== '').sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0);
  return createHash('md5').update(pairs.map(([key, value]) => `${key}=${value}`).join('&') + 'test-secret').digest('hex');
};
function verifyMerchant(target) {
  const params = target.searchParams;
  if (target.origin !== merchantOrigin || target.pathname !== '/return' || ![fixture.pageTrade, fixture.wapTrade].includes(params.get('trade_no')) || params.get('money') !== '66.50' || params.get('pid') !== '1' || params.get('type') !== 'alipay' || params.get('trade_status') !== 'TRADE_SUCCESS' || params.get('sign') !== merchantMD5(params)) throw Error('merchant return did not authenticate the original successful synthetic order');
  return params.get('trade_no');
}

async function newPage(name, mobile) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: mobile ? 375 : 1280, height: mobile ? 812 : 900 }, isMobile: mobile, hasTouch: mobile, serviceWorkers: 'block', userAgent: mobile ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1' : 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0.0.0 Safari/537.36' });
  contexts.push(context);
  const page = await context.newPage();
  page.on('pageerror', error => report.errors.push({ scenario: name, message: error.message }));
  page.on('console', message => { if (message.type() === 'error') report.errors.push({ scenario: name, message: message.text() }); });
  context.on('requestfailed', request => {
    const target = new URL(request.url());
    const failure = { scenario: name, origin: target.origin, path: target.pathname, error: request.failure()?.errorText ?? 'unknown network failure' };
    report.requestFailures.push(failure);
    if (!failure.error.includes('ERR_ABORTED')) report.errors.push({ scenario: name, message: `${failure.error} ${failure.origin}${failure.path}` });
  });
  context.on('response', response => {
    if (response.request().isNavigationRequest()) report.navigations.push({ scenario: name, origin: new URL(response.url()).origin, path: new URL(response.url()).pathname, status: response.status(), main: response.request().frame() === page.mainFrame() });
    if (response.status() >= 400) report.errors.push({ scenario: name, message: `HTTP ${response.status()} ${new URL(response.url()).pathname}` });
    if (response.request().isNavigationRequest() && new URL(response.url()).origin === merchantOrigin && response.status() === 200) {
      try {
        const trade = verifyMerchant(new URL(response.url()));
        report.merchantReturns.push({ scenario: name, trade, main: response.request().frame() === page.mainFrame(), signatureVerified: true });
      } catch (error) { report.errors.push({ scenario: name, message: error.message }); }
    }
  });
  await context.route('**/*', async route => {
    try {
      const request = route.request();
      const target = new URL(request.url());
      if ([fixture.api, gatewayOrigin, cashierOrigin, merchantOrigin].includes(target.origin) && target.pathname === '/favicon.ico') return route.fulfill({ status: 204, body: '' });
      if (target.origin === fixture.api && request.method() === 'GET' && /^\/checkout\/\d+(?:\/(?:return|status|browser-qr\.png))?$/.test(target.pathname)) return route.continue();
      if (target.origin === merchantOrigin && target.pathname === '/return' && request.method() === 'GET' && request.isNavigationRequest()) {
        verifyMerchant(target);
        return route.continue(); // Exact host mapping reaches the cashier TLS peer's separate merchant handler.
      }
      let local;
      if (target.origin === gatewayOrigin && target.pathname === '/gateway.do' && request.method() === 'POST') {
        local = fixture.gatewayLocal;
        const params = new URLSearchParams(request.postData() ?? '');
        const biz = JSON.parse(params.get('biz_content') ?? '{}');
        const method = biz.out_trade_no === fixture.pageTrade ? 'page' : biz.out_trade_no === fixture.wapTrade ? 'wap' : '';
        if (!method || params.get('method') !== `alipay.trade.${method}.pay` || biz.total_amount !== '66.50' || biz.seller_id !== '2088000000000239') throw Error('browser POST changed original merchant, amount, order or provider method');
        if (!mobile && (method !== 'page' || biz.qr_pay_mode !== '4' || biz.qrcode_width !== '230')) throw Error('desktop page browser POST lacks its QR presentation');
        if (mobile && ('qr_pay_mode' in biz || 'qrcode_width' in biz)) throw Error('mobile browser POST gained desktop QR fields');
        if ((request.frame() === page.mainFrame()) !== mobile) throw Error('desktop must target its iframe and mobile must submit at top level');
        report.submitted.push({ scenario: name, method, trade: biz.out_trade_no, main: mobile, qrMode: biz.qr_pay_mode ?? null, amount: biz.total_amount, bodySha256: createHash('sha256').update(request.postDataBuffer()).digest('hex') });
      } else if (target.origin === cashierOrigin && ((request.method() === 'GET' && /^\/fixture\/\d+$/.test(target.pathname)) || (request.method() === 'GET' && target.pathname === '/qr.png') || (request.method() === 'POST' && target.pathname === '/confirm'))) {
        local = fixture.cashierLocal;
      } else {
        report.forbidden.push({ scenario: name, origin: target.origin, path: target.pathname });
        return route.abort('blockedbyclient');
      }
      // Playwright routes only the first URL in a redirect chain. Exact TLS
      // host mappings keep every later browser redirect on the local peers.
      if (local !== fixture.gatewayLocal && local !== fixture.cashierLocal) throw Error('missing explicit local provider mapping');
      await route.continue();
    } catch (error) {
      report.errors.push({ scenario: name, message: error.message });
      await route.abort('failed').catch(() => {});
    }
  });
  return page;
}

async function assertImage(image, withinFrameViewport = false) {
  await expect(image).toBeVisible({ timeout: 12000 });
  await expect.poll(() => image.evaluate(node => node.complete && node.naturalWidth > 0 && node.naturalHeight > 0), { timeout: 10000 }).toBe(true);
  if (withinFrameViewport) {
    await image.scrollIntoViewIfNeeded();
    const bounds = await image.evaluate(node => {
      const rect = node.getBoundingClientRect();
      return { left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom, width: rect.width, height: rect.height, viewportWidth: innerWidth, viewportHeight: innerHeight, documentHeight: document.documentElement.scrollHeight };
    });
    expect(bounds.width, 'complete 230px QR image including its quiet zone').toBe(230);
    expect(bounds.height, 'complete 230px QR image including its quiet zone').toBe(230);
    expect(bounds.left, 'QR left quiet zone stays inside iframe').toBeGreaterThanOrEqual(0);
    expect(bounds.top, 'QR top quiet zone stays inside iframe').toBeGreaterThanOrEqual(0);
    expect(bounds.right, 'QR right quiet zone stays inside iframe').toBeLessThanOrEqual(bounds.viewportWidth);
    expect(bounds.bottom, 'QR bottom quiet zone stays inside iframe').toBeLessThanOrEqual(bounds.viewportHeight);
    expect(bounds.documentHeight, 'compact mode4 fixture fits the cashier iframe').toBeLessThanOrEqual(bounds.viewportHeight);
    return bounds;
  }
}

async function assertMerchant(page, trade) {
  await page.waitForURL(url => url.origin === merchantOrigin && url.pathname === '/return', { timeout: 16000 });
  await expect(page.getByRole('heading', { name: 'Verified synthetic merchant return239' })).toBeVisible();
  if (verifyMerchant(new URL(page.url())) !== trade) throw Error('returned to another merchant order');
}

try {
  for (const method of ['page', 'wap']) {
    const trade = fixture[method + 'Trade'];
    const desktopName = `desktop-${method}1280`;
    const desktop = await newPage(desktopName, false);
    const response = await desktop.goto(fixture[method + 'Checkout'], { waitUntil: 'domcontentloaded' });
    const csp = response.headers()['content-security-policy'] ?? '';
    if (response.status() !== 200 || !csp.includes("connect-src 'self'") || !/script-src 'nonce-[^']+'/.test(csp)) throw Error('browser wrapper needs nonce CSP and local polling');
    let launch = fixture[method + 'Checkout'];
    if (method === 'page') {
      await expect(desktop.locator('iframe[name="alipay-cashier"]')).toHaveCount(1);
      const qrBounds = await assertImage(desktop.frameLocator('iframe[name="alipay-cashier"]').getByRole('img', { name: 'Local Alipay QR239' }), true);
      report.desktopPageQRBounds = qrBounds;
      if (!desktop.frames().some(frame => frame.url().startsWith(cashierOrigin + '/fixture/'))) throw Error('gateway POST did not redirect its iframe across official cashier origins');
    } else {
      await expect(desktop.locator('iframe')).toHaveCount(0);
      await assertImage(desktop.getByRole('img', { name: '支付宝支付二维码' }));
      launch = await desktop.getByRole('link', { name: '在当前设备前往支付宝支付' }).getAttribute('href');
      const continuation = new URL(launch);
      if (continuation.origin !== fixture.api || continuation.pathname !== '/checkout/' + trade || continuation.searchParams.get('browser_launch') !== '1') throw Error('desktop wap QR continuation does not address the same original order');
      await expect(desktop.locator('#alipay-browser-form input[name="method"]')).toHaveValue('alipay.trade.wap.pay');
      if (report.submitted.some(row => row.scenario === desktopName)) throw Error('desktop wap unexpectedly auto-submitted or changed method');
    }
    await desktop.screenshot({ path: path.join(artifacts, desktopName + '.png'), fullPage: true });
    const pending = await desktop.waitForResponse(row => row.url().startsWith(fixture.api + '/checkout/' + trade + '/status?') && row.status() === 200, { timeout: 12000 });
    if ((await pending.json()).paid !== false) throw Error('desktop must observe actual pending order before confirmation');
    report.scenarios.push({ name: desktopName, width: 1280, originalMethod: method, trade, visibleQR: true, pendingObserved: true });

    const mobileName = `mobile-${method}375`;
    const mobile = await newPage(mobileName, true);
    await mobile.goto(launch, { waitUntil: 'commit' });
    await mobile.waitForURL(url => url.origin === cashierOrigin && /^\/fixture\/\d+$/.test(url.pathname), { timeout: 15000 });
    await expect(mobile.locator('iframe')).toHaveCount(0);
    await assertImage(mobile.getByRole('img', { name: 'Local Alipay QR239' }));
    await mobile.screenshot({ path: path.join(artifacts, mobileName + '.png'), fullPage: true });
    await mobile.getByRole('button', { name: 'Confirm synthetic payment239' }).click();
    await Promise.all([assertMerchant(mobile, trade), assertMerchant(desktop, trade)]);
    if (!report.navigations.some(row => row.scenario === mobileName && row.origin === fixture.api && row.path === '/checkout/' + trade + '/return' && row.status === 302)) throw Error('mobile cashier did not return through the real Go return handler');
    if (!report.navigations.some(row => row.scenario === desktopName && row.origin === fixture.api && row.path === '/checkout/' + trade + '/return' && row.status === 302 && row.main)) throw Error('desktop polling did not return through Go at top level');
    report.scenarios.push({ name: mobileName, width: 375, originalMethod: method, trade, topLevelAutoPOST: true, crossOriginCashierRedirect: true, signedCallbackAndMerchantReturn: true });
  }
  if (report.submitted.length !== 3 || report.merchantReturns.filter(row => row.main).length !== 4 || report.errors.length || report.forbidden.length) throw Error('browser boundary failure: ' + JSON.stringify({ submissions: report.submitted.length, returns: report.merchantReturns.length, errors: report.errors, forbidden: report.forbidden }));
  for (const row of report.submitted) {
    if (!report.navigations.some(nav => nav.scenario === row.scenario && nav.origin === gatewayOrigin && nav.status === 303) || !report.navigations.some(nav => nav.scenario === row.scenario && nav.origin === cashierOrigin && nav.status === 200)) throw Error('real browser did not execute the virtual official gateway 303 -> cross-origin cashier chain');
  }
  report.passed = true;
  console.log(JSON.stringify({ passed: true, scenarios: report.scenarios.map(row => row.name), actualExternalNetworkRequests: 0, originalOrders: 2, browserPOSTs: 3, signedCallbacks: 2 }));
} catch (error) {
  report.passed = false;
  report.failure = error.stack ?? error.message;
  throw error;
} finally {
  await fs.writeFile(path.join(artifacts, 'browser-results.json'), JSON.stringify(report, null, 2) + '\n');
  for (const context of contexts) await context.close();
  await browser.close();
}
