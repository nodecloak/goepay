import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import assert from 'node:assert/strict'
import { chromium, expect } from 'playwright/test'
import { manualRoot } from './menu-catalog.mjs'

const dist = path.resolve(process.env.GOEPAY_WEB_DIST || path.join(manualRoot, '../web/dist'))
assert.ok(fs.existsSync(path.join(dist, 'index.html')), 'Build the current GOEPAY frontend first, or set GOEPAY_WEB_DIST')
const output = path.join(manualRoot, 'assets/screenshots')
fs.mkdirSync(output, { recursive: true })
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.ico': 'image/x-icon', '.woff2': 'font/woff2' }
const methods = [['alipay', '支付宝'], ['wxpay', '微信支付'], ['qqpay', 'QQ钱包'], ['bank', '银联支付'], ['jdpay', '京东支付'], ['paypal', 'PayPal'], ['douyinpay', '抖音支付']]
const group = { id: '20', name: '演示零售组', rate_bps: 50, info: { alipay: { channel: '7', type: 'channel', rate: '99.50' }, wxpay: { channel: '8', type: 'channel', rate: '99.50' } } }
const orderRows = [
  { trade_no: '20261103DEMO001', out_trade_no: 'DEMO-SHOP-001', status: 'paid', type: 'alipay', name: '演示商品 A', amount: '10000', refunded_amount: '0', merchant_actions: { notify: true, refund: true, print: true } },
  { trade_no: '20261103DEMO002', out_trade_no: 'DEMO-SHOP-002', status: 'pending', type: 'wxpay', name: '演示商品 B', amount: '2000', refunded_amount: '0', merchant_actions: { query: true } },
].map((r, i) => ({ id: String(i + 1), merchant_id: '10001', income_status: 'none', created_at: '2026-11-03T08:00:00Z', ...r }))
const sources = []
const browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || undefined })
try {
  for (const item of [
    { name: 'admin-pay-types', area: 'admin', url: '/admin/payapi/pay-type', ready: '.pay-type-page tbody tr', count: 7 },
    { name: 'admin-group-edit', area: 'admin', url: '/admin/merchant/group-list', ready: 'tbody tr' },
    { name: 'merchant-orders', area: 'merchant', url: '/merchant/trade/order', ready: 'tbody tr' },
    { name: 'merchant-order-actions', area: 'merchant', url: '/merchant/trade/order', ready: 'tbody tr' },
    { name: 'merchant-api-info', area: 'merchant', url: '/merchant/account/api-info', ready: 'main' },
  ]) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1040 }, deviceScaleFactor: 1 })
    await context.addInitScript(() => localStorage.setItem('goepay-console-theme', 'light'))
    const page = await context.newPage()
    const errors = [], external = [], writes = []
    page.on('pageerror', error => errors.push(error.message))
    await page.route('**/*', async route => {
      const req = route.request(), u = new URL(req.url()), p = u.pathname
      if (u.origin !== 'http://127.0.0.1:25243') { external.push(req.url()); return route.abort() }
      if (req.method() !== 'GET') { writes.push(p); return route.fulfill({ status: 405, json: { code: -1, message: 'Read-only synthetic screenshot fixture' } }) }
      if (p.startsWith('/console/')) {
        let data = { items: [], total: 0 }
        if (p.endsWith('/session')) data = { user: { id: item.area === 'admin' ? '1' : '10001', name: item.area === 'admin' ? '演示管理员' : '演示商户', role: item.area, ...(item.area === 'admin' ? { admin_class: 'super' } : {}) }, csrf_token: 'manual-synthetic-only' }
        else if (p.endsWith('/pay-types/schema')) data = { title: '支付方式', fields: [], permissions: { create: true, update: true, delete: false }, actions: [] }
        else if (p.endsWith('/pay-types')) data = { items: methods.map(([type, name], i) => ({ id: '0', type, name, enabled: i < 2, device: '0' })), total: 7 }
        else if (p.endsWith('/groups/schema')) data = { title: '用户组设置', fields: [{ name: 'name', label: '名称', type: 'string', required: true }, { name: 'rate_bps', label: '费率（万分比）', type: 'integer' }, { name: 'info', label: '支付方式分成比例', type: 'json' }], permissions: { create: true, update: true, delete: true }, actions: [] }
        else if (p.endsWith('/groups/routing-options')) data = { available: true, items: [{ id: '7', type: 'alipay', name: '演示支付宝', methods: ['native'] }, { id: '8', type: 'wxpay', name: '演示微信', methods: ['native'] }] }
        else if (p.endsWith('/payment-risk/groups/20')) data = { group_id: '20', group_name: group.name, configured: false, revision: '0', fingerprint: 'a'.repeat(64), config: { pay_block_oversea: '', check_pay_regoin: '' }, effective_oversea: '0', effective_scan_region: '0', geo_configured: false }
        else if (p.endsWith('/groups/20')) data = group
        else if (p.endsWith('/groups')) data = { items: [group], total: 1 }
        else if (p.endsWith('/security/payment-password')) data = { configured: false, enforced: false, credential_epoch: 0, totp_required: false }
        else if (p.endsWith('/api-info')) data = { pid: '10001', algorithm: 'MD5', api_key_masked: '********DEMO', merchant_public_key: '', platform_public_key: '', can_rotate_rsa: false }
        else if (p.endsWith('/signature-policy')) data = { merchant_id: '10001', keytype: null, algorithm: 'MD5', rsa_configured: false, fingerprint: 'a'.repeat(64), can_update: true }
        else if (p.endsWith('/orders')) data = { items: orderRows, total: 2 }
        else if (p.includes('/orders/')) {
          const row = orderRows.find(r => p.endsWith('/' + r.trade_no))
          if (row) data = { order: row, reporting: { refunded_amount: row.refunded_amount }, merchant_actions: row.merchant_actions }
        }
        return route.fulfill({ json: { code: 0, data } })
      }
      let file = p.startsWith('/admin/') || p.startsWith('/merchant/') ? path.join(dist, 'index.html') : path.resolve(dist, '.' + p)
      if (p === '/static/img/icon.png') file = path.join(dist, 'static/apple-touch-icon.png')
      if (!file.startsWith(dist + path.sep) || !fs.existsSync(file)) return route.fulfill({ status: 404, body: 'Not found' })
      return route.fulfill({ path: file, contentType: mime[path.extname(file)] || 'application/octet-stream' })
    })
    await page.goto('http://127.0.0.1:25243' + item.url)
    await page.waitForLoadState('networkidle')
    await expect(page.locator(item.ready).first()).toBeVisible()
    if (item.count) await expect(page.locator(item.ready)).toHaveCount(item.count)
    if (item.name === 'admin-group-edit') {
      await page.getByRole('button', { name: '编辑', exact: true }).first().click()
      await expect(page.getByRole('dialog')).toBeVisible()
      await expect(page.getByRole('combobox', { name: '支付宝通道', exact: true })).toHaveValue('channel:7')
    }
    if (item.name === 'merchant-order-actions') {
      await page.getByRole('button', { name: /^订单操作 / }).first().click()
      await expect(page.getByRole('menuitem', { name: '退款', exact: true })).toBeVisible()
    }
    await page.evaluate(async () => { await document.fonts.ready; await Promise.all([...document.images].map(i => i.decode().catch(() => {}))) })
    await page.screenshot({ path: path.join(output, item.name + '.png'), fullPage: false })
    assert.deepEqual(errors, [], item.name + ' JS errors')
    assert.deepEqual(external, [], 'External request blocked')
    assert.deepEqual(writes, [], 'Unexpected mutation')
    sources.push({ name: item.name, route: item.url, synthetic_data: true, image_sha256: crypto.createHash('sha256').update(fs.readFileSync(path.join(output, item.name + '.png'))).digest('hex'), external_requests: 0, mutations: 0 })
    await context.close()
  }
} finally { await browser.close() }
const manifest = { synthetic_only: true, production_access: false, app_index_sha256: crypto.createHash('sha256').update(fs.readFileSync(path.join(dist, 'index.html'))).digest('hex'), screenshots: sources }
fs.writeFileSync(path.join(output, 'provenance.json'), JSON.stringify(manifest, null, 2) + '\n')
console.log(JSON.stringify({ success: true, screenshots: sources.length, external_requests: 0, mutations: 0 }))
