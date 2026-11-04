import fs from 'node:fs'
import path from 'node:path'
import { chromium } from 'playwright'
import { manualRoot } from './menu-catalog.mjs'

const source = path.join(manualRoot, 'diagrams')
const output = path.join(manualRoot, 'public/diagrams')
fs.mkdirSync(output, { recursive: true })

const palettes = {
  light: { bg: '#ffffff', ink: '#24364b', muted: '#5d7188', border: '#b8c9da', surface: '#f7f9fc', card: '#edf4fb', accent: '#376d9e', subtle: '#e1eaf4', note: '#f0f5fa' },
  dark: { bg: '#151e2b', ink: '#e2ebf4', muted: '#aabdd1', border: '#4a617d', surface: '#1b293b', card: '#243950', accent: '#a4c7e9', subtle: '#354c67', note: '#243447' },
}
const esc = value => String(value).replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&apos;' }[char]))

// Architecture uses an explicit grid: business roles/domains and technical
// boundaries should remain legible instead of depending on graph auto-layout.
function architectureSvg(spec, mode) {
  const p = palettes[mode]
  const isBusiness = spec.kind === 'business-architecture'
  const width = 1060
  const height = isBusiness ? 774 : 820
  const parts = []
  const rect = (x, y, w, h, fill = p.card, radius = 8, stroke = p.border) =>
    parts.push(`<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${radius}" fill="${fill}" stroke="${stroke}" stroke-width="1.4"/>`)
  const label = (x, y, value, size = 18, weight = 400, fill = p.ink, anchor = 'start', extra = '') =>
    parts.push(`<text x="${x}" y="${y}" font-size="${size}" font-weight="${weight}" fill="${fill}" text-anchor="${anchor}" ${extra}>${esc(value)}</text>`)
  const arrow = (x1, y1, x2, y2, both = false) =>
    parts.push(`<path d="M ${x1} ${y1} L ${x2} ${y2}" fill="none" stroke="${p.accent}" stroke-width="1.6" marker-end="url(#arrow)" ${both ? 'marker-start="url(#arrow-start)"' : ''}/>`)
  parts.push(`<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-labelledby="diagram-title diagram-desc" style="max-width:100%;height:auto;font-family:'Segoe UI','Microsoft YaHei',sans-serif"><title id="diagram-title">${esc(spec.title)}</title><desc id="diagram-desc">${esc(spec.description)}</desc><defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="6.4" refY="4" orient="auto"><path d="M1 1 L7 4 L1 7" fill="none" stroke="${p.accent}" stroke-width="1.3"/></marker><marker id="arrow-start" markerWidth="8" markerHeight="8" refX="1.6" refY="4" orient="auto"><path d="M7 1 L1 4 L7 7" fill="none" stroke="${p.accent}" stroke-width="1.3"/></marker></defs><rect width="${width}" height="${height}" fill="${p.bg}"/>`)
  label(24, 38, spec.title, 27, 650)
  label(24, 70, spec.subtitle, 18, 400, p.muted)

  if (isBusiness) {
    spec.roles.forEach((role, i) => {
      const x = 44 + i * 248
      rect(x, 102, 228, 84, p.surface)
      label(x + 114, 135, role.title, 21, 600, p.ink, 'middle')
      label(x + 114, 164, role.detail, 17, 400, p.muted, 'middle')
      arrow(x + 114, 187, x + 114, 216)
    })
    rect(24, 218, 1012, 382, p.surface, 10)
    label(44, 250, 'GOEPAY 业务域', 19, 600)
    spec.domains.forEach((domain, i) => {
      const x = 44 + i * 248
      rect(x, 272, 228, 236)
      label(x + 18, 306, domain.title, 22, 650)
      label(x + 18, 332, domain.tag, 12, 600, p.muted, 'start', 'letter-spacing="1.8"')
      parts.push(`<line x1="${x + 18}" y1="349" x2="${x + 210}" y2="349" stroke="${p.border}" stroke-width="1"/>`)
      domain.items.forEach((item, row) => label(x + 18, 379 + row * 32, item, 17.5))
    })
    rect(44, 534, 972, 44, p.note, 6, p.subtle)
    label(530, 562, spec.foundation, 17, 500, p.muted, 'middle')
    spec.partners.forEach((partner, i) => {
      const x = 44 + i * 496
      arrow(x + 238, 601, x + 238, 654, true)
      label(x + 255, 632, i ? '结果与协作' : '资金与核验', 15, 400, p.muted)
      rect(x, 658, 476, 92, p.surface)
      label(x + 22, 693, partner.title, 21, 600)
      label(x + 22, 725, partner.detail, 17.5, 400, p.muted)
    })
  } else {
    spec.entries.forEach((entry, i) => {
      const x = 24 + i * 350
      rect(x, 102, 312, 84, p.surface)
      label(x + 156, 135, entry.title, 21, 600, p.ink, 'middle')
      label(x + 156, 164, entry.detail, 17, 400, p.muted, 'middle')
      arrow(x + 156, 187, x + 156, 216)
    })
    rect(24, 218, 1012, 376, p.surface, 10)
    label(44, 250, 'Go 服务', 19, 600)
    rect(44, 270, 972, 58, p.note, 6, p.subtle)
    label(530, 306, spec.boundary, 19, 500, p.ink, 'middle')
    spec.modules.forEach((module, i) => {
      const x = 44 + i * 248
      arrow(x + 114, 329, x + 114, 351)
      rect(x, 355, 228, 116)
      label(x + 18, 389, module.title, 21, 650)
      module.detail.forEach((line, row) => label(x + 18, 421 + row * 28, line, 17.5))
    })
    spec.workers.forEach((worker, i) => {
      const x = 44 + i * 330
      rect(x, 499, 312, 72, p.note, 6, p.subtle)
      label(x + 18, 527, 'Worker · ' + worker.title, 18.5, 600)
      label(x + 18, 554, worker.detail, 17, 400, p.muted)
    })
    arrow(282, 595, 282, 658, true)
    arrow(778, 595, 778, 658, true)
    label(302, 632, '事务与持久化任务', 15, 400, p.muted)
    label(798, 632, 'API / 回调 / 投递', 15, 400, p.muted)
    for (const [i, record] of [spec.data, spec.external].entries()) {
      const x = 24 + i * 520
      rect(x, 662, 492, 132, p.surface)
      label(x + 22, 699, record.title, 22, 600)
      record.detail.forEach((line, row) => label(x + 22, 735 + row * 30, line, 17))
    }
  }
  parts.push('</svg>')
  return parts.join('')
}

function workflowSvg(spec, mode) {
  const p = palettes[mode]
  const width = 1060, height = 680
  const margin = 24, gap = 24
  const stageWidth = (width - margin * 2 - gap * (spec.stages.length - 1)) / spec.stages.length
  const parts = [`<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-labelledby="diagram-title diagram-desc" style="max-width:100%;height:auto;font-family:'Segoe UI','Microsoft YaHei',sans-serif"><title id="diagram-title">${esc(spec.title)}</title><desc id="diagram-desc">${esc(spec.description)}</desc><defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="6.4" refY="4" orient="auto"><path d="M1 1 L7 4 L1 7" fill="none" stroke="${p.accent}" stroke-width="1.3"/></marker></defs><rect width="${width}" height="${height}" fill="${p.bg}"/>`]
  const text = (x, y, value, size = 18, weight = 400, fill = p.ink, anchor = 'start') => parts.push(`<text x="${x}" y="${y}" font-size="${size}" font-weight="${weight}" fill="${fill}" text-anchor="${anchor}">${esc(value)}</text>`)
  const box = (x, y, w, h, fill = p.card, radius = 7, stroke = p.border) => parts.push(`<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${radius}" fill="${fill}" stroke="${stroke}" stroke-width="1.3"/>`)
  const path = (d, arrow = true) => parts.push(`<path d="${d}" fill="none" stroke="${p.accent}" stroke-width="1.5" ${arrow ? 'marker-end="url(#arrow)"' : ''}/>`)
  const lines = (x, y, content, size = 18, fill = p.ink, lineHeight = 25) => content.forEach((line, row) => text(x, y + row * lineHeight, line, size, 400, fill, 'middle'))
  text(24, 38, spec.title, 27, 650)
  text(24, 70, spec.subtitle, 18, 400, p.muted)

  spec.stages.forEach((stage, i) => {
    const x = margin + i * (stageWidth + gap)
    const cx = x + stageWidth / 2
    box(x, 112, stageWidth, 456, p.surface, 9)
    text(x + 17, 146, String(i + 1).padStart(2, '0'), 17, 650, p.accent)
    text(x + 51, 146, stage.title, 19, 650)
    parts.push(`<line x1="${x + 16}" y1="164" x2="${x + stageWidth - 16}" y2="164" stroke="${p.border}" stroke-width="1"/>`)
    if (i < spec.stages.length - 1) path(`M ${x + stageWidth + 2} 141 L ${x + stageWidth + gap - 3} 141`)
    if (stage.steps) {
      stage.steps.forEach((step, row) => {
        const y = 196 + row * 108
        box(x + 16, y, stageWidth - 32, 66)
        lines(cx, y + (step.length > 1 ? 27 : 39), step)
        if (row < stage.steps.length - 1) path(`M ${cx} ${y + 67} L ${cx} ${y + 104}`)
      })
    } else {
      const top = stage.intro ? 210 : 190
      const cy = top + 39, bottom = top + 78
      if (stage.intro) text(cx, 191, stage.intro, 15.5, 400, p.muted, 'middle')
      parts.push(`<path d="M ${cx} ${top} L ${x + stageWidth - 22} ${cy} L ${cx} ${bottom} L ${x + 22} ${cy} Z" fill="${p.card}" stroke="${p.border}" stroke-width="1.3"/>`)
      lines(cx, cy + (stage.decision.length > 1 ? -2 : 6), stage.decision, 18, p.ink, 24)
      const branchTop = stage.intro ? 308 : 288
      const branchGap = stage.branches.length === 3 ? 84 : 104
      const busX = x + 12
      const lastCenter = branchTop + (stage.branches.length - 1) * branchGap + 38
      path(`M ${cx} ${bottom + 1} L ${cx} ${bottom + 10} L ${busX} ${bottom + 10} L ${busX} ${lastCenter}`, false)
      stage.branches.forEach((branch, row) => {
        const y = branchTop + row * branchGap
        path(`M ${busX} ${y + 38} L ${x + 22} ${y + 38}`)
        box(x + 24, y, stageWidth - 48, 76, p.note, 6, p.subtle)
        text(x + 36, y + 20, branch.when, 14.5, 600, p.accent)
        lines(cx, y + (branch.lines.length > 1 ? 44 : 51), branch.lines, 17, p.ink, 23)
        if (branch.loop) {
          const railX = x + stageWidth - 9
          path(`M ${x + stageWidth - 23} ${y + 38} L ${railX} ${y + 38} L ${railX} ${cy} L ${x + stageWidth - 20} ${cy}`)
        }
      })
    }
  })
  const noteLines = []
  let current = '', used = 0
  for (const char of spec.note) {
    const weight = /[\u0000-\u00ff]/.test(char) ? 0.55 : 1
    if (used + weight > 58) { noteLines.push(current); current = ''; used = 0 }
    current += char; used += weight
  }
  if (current) noteLines.push(current)
  box(24, 594, 1012, 62, p.note, 6, p.subtle)
  noteLines.forEach((line, row) => text(44, 620 + row * 23, line, 16.5, 400, p.muted))
  parts.push('</svg>')
  return parts.join('')
}

const browserOptions = { headless: true }
const configuredBrowser = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
const installedBrowser = 'C:/Users/james/AppData/Local/ms-playwright/chromium-1228/chrome-win64/chrome.exe'
if (configuredBrowser) browserOptions.executablePath = configuredBrowser
else if (fs.existsSync(installedBrowser)) browserOptions.executablePath = installedBrowser
const browser = await chromium.launch(browserOptions)
let rendered = 0
let networkRequests = 0
try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1200 } })
  await page.route(/^https?:\/\//, route => { networkRequests++; return route.abort() })
  await page.setContent('<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>')
  await page.addScriptTag({ path: path.join(manualRoot, 'node_modules/mermaid/dist/mermaid.min.js') })
  const sources = fs.readdirSync(source).filter(name => /\.(mmd|json)$/.test(name)).sort()
  for (const mode of ['light', 'dark']) {
    for (const file of sources) {
      const name = path.basename(file, path.extname(file))
      // The old architecture URL remains a compatibility alias of the new
      // system view; the business view has an independent source and diagram.
      if (name === 'architecture' || (file.endsWith('.mmd') && sources.includes(`${name}.json`))) continue
      const native = file.endsWith('.json')
      const definition = fs.readFileSync(path.join(source, file), 'utf8')
      const spec = native ? JSON.parse(definition) : null
      const rawSvg = native ? (spec.kind === 'workflow' ? workflowSvg(spec, mode) : architectureSvg(spec, mode)) : null
      const result = await page.evaluate(async ({ mode, definition, name, rawSvg, palette }) => {
        let svg = rawSvg
        if (!svg) {
          const engine = window.mermaid.default || window.mermaid
          engine.initialize({
            startOnLoad: false, securityLevel: 'strict', theme: 'base', htmlLabels: false,
            deterministicIds: true, deterministicIDSeed: name + mode,
            fontFamily: '"Segoe UI", "Microsoft YaHei", sans-serif',
            themeVariables: {
              primaryColor: palette.card, primaryTextColor: palette.ink, primaryBorderColor: palette.border,
              lineColor: palette.accent, secondaryColor: palette.note, tertiaryColor: palette.surface,
              background: palette.bg, mainBkg: palette.card, textColor: palette.ink,
              clusterBkg: palette.surface, clusterBorder: palette.border,
              edgeLabelBackground: palette.bg, fontSize: '18px',
              actorBkg: palette.card, actorBorder: palette.border, actorTextColor: palette.ink,
              actorLineColor: palette.border, signalColor: palette.accent, signalTextColor: palette.ink,
              labelBoxBkgColor: palette.surface, labelBoxBorderColor: palette.border,
              labelTextColor: palette.ink, loopTextColor: palette.ink,
              noteBkgColor: palette.note, noteBorderColor: palette.border, noteTextColor: palette.ink,
              activationBkgColor: palette.subtle, activationBorderColor: palette.border,
              sequenceNumberColor: palette.bg,
            },
            flowchart: { htmlLabels: false, useMaxWidth: true, curve: 'linear', nodeSpacing: 22, rankSpacing: 32, padding: 15, diagramPadding: 16 },
            sequence: { useMaxWidth: true, wrap: false, actorMargin: 28, width: 108, height: 52, messageMargin: 16, noteMargin: 8, diagramMarginX: 18, diagramMarginY: 18, boxMargin: 8, boxTextMargin: 6, mirrorActors: false },
            themeCSS: '.node rect,.node polygon,.node path { stroke-width:1.3px; } .cluster rect { rx:8px; ry:8px; } .cluster-label text { font-weight:600; } .messageText,.loopText,.noteText { font-size:18px !important; } .actor text { font-size:18px !important; font-weight:600; } .actor-line { stroke-dasharray:4 4; }',
          })
          svg = (await engine.render(`diagram-${name}-${mode}`, definition)).svg
        }
        const container = document.createElement('div')
        container.innerHTML = svg
        const root = container.firstElementChild
        // Standalone <img> elements need intrinsic dimensions, not Mermaid's width="100%".
        // Otherwise the browser falls back to 300x150 and zoom percentages are misleading.
        const bounds = root.getAttribute('viewBox').trim().split(/[\s,]+/).map(Number)
        if (bounds.length !== 4 || !bounds.every(Number.isFinite) || bounds[2] <= 0 || bounds[3] <= 0) throw Error(`Invalid diagram bounds: ${name}`)
        root.setAttribute('width', String(bounds[2]))
        root.setAttribute('height', String(bounds[3]))
        root.querySelectorAll('[filter]').forEach(node => node.removeAttribute('filter'))
        const flatStyle = document.createElementNS('http://www.w3.org/2000/svg', 'style')
        flatStyle.textContent = '* { filter:none !important; }'
        root.appendChild(flatStyle)
        root.setAttribute('data-diagram', name)
        root.setAttribute('data-theme', mode)
        const standalone = new XMLSerializer().serializeToString(root)
        const parsed = new DOMParser().parseFromString(standalone, 'image/svg+xml')
        if (parsed.querySelector('parsererror')) throw Error(`Invalid standalone SVG: ${name}`)
        return standalone
      }, { mode, definition, name, rawSvg, palette: palettes[mode] })
      if (!result.includes('<svg') || !result.includes('viewBox=')) throw Error(`Invalid diagram: ${name}`)
      fs.writeFileSync(path.join(output, `${name}.${mode}.svg`), result)
      if (name === 'system-architecture') fs.writeFileSync(path.join(output, `architecture.${mode}.svg`), result)
      rendered++
    }
  }
  if (networkRequests) throw Error(`Unexpected diagram resource requests: ${networkRequests}`)
} finally { await browser.close() }
console.log(`Rendered ${rendered} source diagrams in light/dark SVG; architecture alias retained; all resources local`)
