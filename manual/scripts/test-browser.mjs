import fs from 'node:fs'
import http from 'node:http'
import path from 'node:path'
import assert from 'node:assert/strict'
import { chromium, expect } from 'playwright/test'
import { manualRoot } from './menu-catalog.mjs'
import { checkProductMenu } from './check-product-menu.mjs'
import { checkDesktopShell, checkResponsiveShell } from './check-shell-interactions.mjs'

const dist = path.join(manualRoot, '.vitepress/dist')
assert.ok(fs.existsSync(path.join(dist, 'index.html')), 'Build the handbook first')
const output = path.join(manualRoot, 'test-results')
fs.mkdirSync(output, { recursive: true })
const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2' }
const failedRequests = []
const server = http.createServer((request, response) => {
  let relative = decodeURIComponent(new URL(request.url, 'http://127.0.0.1').pathname)
  if (!relative.startsWith('/manual/')) { response.writeHead(404); response.end(); return }
  relative = relative.slice('/manual/'.length)
  if (!relative || relative.endsWith('/')) relative += 'index.html'
  const file = path.resolve(dist, relative)
  if (!['GET', 'HEAD'].includes(request.method) || !file.startsWith(dist + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) {
    failedRequests.push(relative); response.writeHead(404); response.end(); return
  }
  const body = fs.readFileSync(file)
  response.writeHead(200, { 'Content-Type': types[path.extname(file)] || 'application/octet-stream', 'Content-Length': body.length })
  response.end(request.method === 'HEAD' ? undefined : body)
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const origin = `http://127.0.0.1:${server.address().port}`
const browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || undefined })
const errors = [], external = [], scenarios = []
function allHtml(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap(item => {
    const p = path.join(directory, item.name)
    return item.isDirectory() ? allHtml(p) : item.name.endsWith('.html') && item.name !== '404.html' ? [p] : []
  })
}
function targetFile(href) {
  const url = new URL(href)
  let relative = decodeURIComponent(url.pathname).slice('/manual/'.length)
  if (!relative || relative.endsWith('/')) relative += 'index.html'
  return { file: path.resolve(dist, relative), hash: decodeURIComponent(url.hash.slice(1)) }
}
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: ['clipboard-read', 'clipboard-write'] })
  await context.route('**/*', route => {
    if (new URL(route.request().url()).origin !== origin) { external.push(route.request().url()); return route.abort() }
    return route.continue()
  })
  const page = await context.newPage()
  page.on('pageerror', error => errors.push(error.message))
  let checkedLinks = 0, checkedImages = 0
  for (const file of allHtml(dist)) {
    const relative = path.relative(dist, file).split(path.sep).join('/')
    const response = await page.goto(`${origin}/manual/${relative}`)
    assert.equal(response.status(), 200)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('.vp-doc h1')).toBeVisible()
    const images = await page.locator('img').evaluateAll(async images => {
      return Promise.all(images.map(async image => {
        image.loading = 'eager'
        let error = ''
        try { await image.decode() } catch (failure) { error = String(failure) }
        return { src: image.currentSrc || image.src, width: image.naturalWidth, height: image.naturalHeight, error }
      }))
    })
    assert.ok(images.every(img => img.width > 0 && img.height > 0 && !img.error), JSON.stringify({ page: relative, images, failedRequests }))
    checkedImages += images.length
    const links = await page.locator('a[href]').evaluateAll(links => links.map(a => a.href))
    for (const href of links) {
      const u = new URL(href)
      if (u.origin !== origin) continue
      assert.ok(u.pathname.startsWith('/manual/'), 'Base path lost: ' + href)
      const target = targetFile(href)
      assert.ok(target.file.startsWith(dist + path.sep) && fs.existsSync(target.file), 'Missing built target: ' + href)
      if (target.hash && target.file.endsWith('.html')) assert.ok(fs.readFileSync(target.file, 'utf8').includes(`id="${target.hash}"`), 'Missing anchor: ' + href)
      checkedLinks++
    }
  }
  await page.goto(`${origin}/manual/`)
  await page.waitForLoadState('networkidle')
  const geometry = await page.evaluate(() => {
    const sidebar = document.querySelector('.docs-sidebar').getBoundingClientRect()
    const h1 = document.querySelector('.vp-doc h1')
    return { sidebar: sidebar.width, h1x: h1.getBoundingClientRect().x, h1size: parseFloat(getComputedStyle(h1).fontSize), navHeight: document.querySelector('.docs-header').getBoundingClientRect().height }
  })
  assert.deepEqual(geometry, { sidebar: 300, h1x: 316, h1size: 42, navHeight: 60 })
  await page.screenshot({ path: path.join(output, 'manual-home-light-1440.png'), fullPage: false })
  await checkProductMenu(page, origin, output)
  scenarios.push({ name: 'product-menu-dismiss-keyboard-links-layering-responsive', passed: true })
  await page.getByRole('tab', { name: '普通商户', exact: true }).click()
  await expect(page.locator('#role-panel-merchant')).toBeVisible()
  await page.keyboard.press('ArrowLeft')
  await expect(page.locator('#role-panel-admin')).toBeVisible()
  await page.getByRole('button', { name: '复制本页', exact: true }).click()
  await expect(page.getByRole('button', { name: '已复制', exact: true })).toBeVisible()
  assert.ok((await page.evaluate(() => navigator.clipboard.readText())).includes('快速开始'))
  await page.getByRole('button', { name: '搜索使用手册' }).click()
  await page.locator('#localsearch-input').fill('默认通道')
  await expect(page.locator('.VPLocalSearchBox a.result').first()).toBeVisible()
  await expect(page.locator('.VPLocalSearchBox a.result[href*="admin/groups"]').first()).toBeVisible()
  await page.screenshot({ path: path.join(output, 'manual-search.png'), fullPage: true })
  await page.keyboard.press('Escape')
  await page.getByRole('switch', { name: /深色模式/ }).click()
  await expect(page.locator('html')).toHaveClass(/dark/)
  await page.screenshot({ path: path.join(output, 'manual-home-dark-1440.png'), fullPage: false })
  await page.goto(`${origin}/manual/concepts/payment-flow.html`)
  await expect(page.locator('.diagram-dark')).toBeVisible()
  assert.ok(await page.locator('.diagram-dark').evaluate(img => img.complete && img.naturalWidth > 0))
  await page.screenshot({ path: path.join(output, 'manual-payment-sequence-dark.png'), fullPage: true })
  const paymentFigure = page.locator('[data-diagram="payment-sequence"]')
  await expect(paymentFigure.locator('.diagram-kind')).toHaveText('时序图')
  const openDiagram = paymentFigure.getByRole('button', { name: /^放大时序图/ })
  await openDiagram.click()
  const diagramDialog = page.getByRole('dialog')
  await expect(diagramDialog).toBeVisible()
  const scaleBefore = await diagramDialog.locator('[aria-live="polite"]').innerText()
  await diagramDialog.getByRole('button', { name: '放大图表', exact: true }).click()
  assert.notEqual(await diagramDialog.locator('[aria-live="polite"]').innerText(), scaleBefore)
  await expect(diagramDialog.getByRole('link', { name: '下载图表' })).toHaveAttribute('href', /\.dark\.svg$/)
  await diagramDialog.getByRole('button', { name: '适合窗口', exact: true }).click()
  await page.screenshot({ path: path.join(output, 'manual-sequence-fullscreen.png'), fullPage: false })
  await page.keyboard.press('Escape')
  await expect(diagramDialog).toHaveCount(0)
  await expect(openDiagram).toBeFocused()
  for (const [slug, diagram] of [['transactions', 'payment-sequence'], ['refunds', 'refund-sequence'], ['settlements', 'settlement-sequence']]) {
    await page.goto(`${origin}/manual/modules/${slug}.html`)
    await expect(page.locator(`figure[data-diagram="${diagram}"] .diagram-kind`)).toHaveText('时序图')
  }
  scenarios.push({ name: 'all-pages-desktop', pages: allHtml(dist).length, checked_links: checkedLinks, decoded_images: checkedImages })
  scenarios.push({ name: 'search-chinese-keyword', passed: true }, { name: 'theme-switch-and-dark-diagram', passed: true })
  scenarios.push({ name: 'reference-layout-geometry', ...geometry }, { name: 'role-tabs-and-copy', passed: true }, { name: 'sequence-fullscreen-zoom-keyboard', passed: true }, { name: 'module-specific-sequences', passed: true })
  await page.getByRole('switch', { name: '切换到浅色模式', exact: true }).click()
  await checkDesktopShell(page, origin, output)
  scenarios.push({ name: 'sidebar-expand-keyboard-history-search-select-clear-empty-not-found', passed: true })
  await context.close()
  const mobile = await browser.newContext({ viewport: { width: 375, height: 900 }, isMobile: true, hasTouch: true })
  await mobile.route('**/*', route => {
    if (new URL(route.request().url()).origin !== origin) { external.push(route.request().url()); return route.abort() }
    return route.continue()
  })
  const phone = await mobile.newPage()
  phone.on('pageerror', error => errors.push(error.message))
  for (const width of [320, 375]) {
    await phone.setViewportSize({ width, height: 900 })
    for (const file of allHtml(dist)) {
      const relative = path.relative(dist, file).split(path.sep).join('/')
      await phone.goto(`${origin}/manual/${relative}`)
      await phone.waitForLoadState('networkidle')
      await expect(phone.locator('.vp-doc h1')).toBeVisible()
      assert.ok(await phone.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `Mobile page overflow at ${width}px: ${relative}`)
    }
    scenarios.push({ name: `all-pages-mobile-${width}`, pages: allHtml(dist).length, passed: true })
  }
  await checkResponsiveShell(phone, origin, output)
  scenarios.push({ name: 'responsive-320-1440-drawer-focus-resize-search-toc-landscape-diagram', passed: true })
  await phone.goto(`${origin}/manual/`)
  await phone.screenshot({ path: path.join(output, 'manual-home-mobile-375.png'), fullPage: true })
  await expect(phone.locator('.docs-sidebar')).not.toBeVisible()
  await phone.getByRole('button', { name: '目录', exact: true }).click()
  await expect(phone.locator('.docs-sidebar')).toBeVisible()
  await expect(phone.locator('.docs-sidebar')).toHaveClass(/open/)
  await expect(phone.locator('.docs-sidebar').getByRole('heading', { name: '功能模块', exact: true })).toBeVisible()
  await phone.locator('.docs-sidebar').getByRole('link', { name: '收款与订单', exact: true }).click()
  await expect(phone.locator('.vp-doc h1')).toHaveText('收款与订单')
  await expect(phone.locator('.docs-sidebar')).not.toHaveClass(/open/)
  await expect(phone.locator('.docs-sidebar')).not.toBeVisible()
  scenarios.push({ name: 'mobile-sidebar-navigation', passed: true })
  await mobile.close()
  assert.deepEqual(errors, [])
  assert.deepEqual(external, [])
  assert.deepEqual(failedRequests, [])
  const receipt = { success: true, scenarios, javascript_errors: 0, failed_requests: 0, external_requests: 0, real_transactions: 0 }
  fs.writeFileSync(path.join(output, 'browser.json'), JSON.stringify(receipt, null, 2) + '\n')
  console.log(JSON.stringify(receipt))
} finally {
  await browser.close()
  await new Promise(resolve => server.close(resolve))
}
