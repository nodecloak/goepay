import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const manualRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const projectRoot = path.dirname(manualRoot)
const supplement = fs.readFileSync(path.join(projectRoot, 'web/src/menu.ts'), 'utf8')
  .split('export const supplementalMenus:')[1].split('export const navigationMenus')[0]

export function catalog(area) {
  const out = []
  function walk(nodes, base = '', trail = [], hidden = false) {
    for (const n of nodes) {
      const p = n.path.startsWith('/') ? n.path : `${base}/${n.path}`
      const names = [...trail, n.meta.title]
      const isHidden = hidden || !!(n.meta.isHide || n.meta.hidden || n.meta.hideInMenu)
      if (n.children?.length) walk(n.children, p, names, isHidden)
      else out.push({ area, path: `/${area}${p}`, title: names.join(' → '), hidden: isHidden, superOnly: n.meta.roles?.length === 1 && n.meta.roles[0] === 'R_SUPER' })
    }
  }
  walk(JSON.parse(fs.readFileSync(path.join(projectRoot, `web/src/data/menu-${area}.json`), 'utf8')))
  const block = area === 'admin' ? supplement.split('admin: [')[1].split('  merchant: [')[0] : supplement.split('  merchant: [')[1]
  const pattern = /id:\s*"([^"]+)"[\s\S]*?title:\s*"([^"]+)"[\s\S]*?path:\s*"([^"]+)"[\s\S]*?roles:\s*\[([^\]]*)\]/g
  for (const match of block.matchAll(pattern)) {
    out.push({ area, path: `/${area}${match[3]}`, title: match[2], hidden: false, superOnly: match[4].includes('R_SUPER') && !match[4].includes('R_ADMIN') })
  }
  if (new Set(out.map(row => row.path)).size !== out.length) throw Error('Duplicate source menu path')
  return out
}

export function guide(row) {
  const p = row.path
  const maps = row.area === 'admin' ? [
    ['/dashboard', 'reports'], ['/trade/refund', 'refunds'], ['/trade/blacklist', 'risk-and-onboarding'],
    ['/trade/order', 'orders'], ['/trade/stat', 'reports'], ['/profitsharing', 'sharing'],
    ['/transfer', 'transfers'], ['/settle', 'settlements'],
    ['/merchant/group-list', 'groups'], ['/merchant/group-buy', 'reports'], ['/merchant/record', 'reports'],
    ['/merchant/internal-transfer', 'transfers'], ['/merchant/notice', 'notifications'],
    ['/merchant/deposit-review', 'settlements'], ['/merchant', 'merchants'],
    ['/payapi', 'channels'], ['/applyments', 'risk-and-onboarding'], ['/complain', 'risk-and-onboarding'],
    ['/other/risk', 'risk-and-onboarding'], ['/other/get-token', 'risk-and-onboarding'], ['/other/onecode', 'appearance'],
    ['/app', 'appearance'], ['/system/migration', 'migration'], ['/system/settings', 'settings'],
    ['/system/printing', 'notifications'], ['/system/referral-policies', 'settings'],
    ['/system/order-subject-policies', 'settings'], ['/system', 'system'],
  ] : [
    ['/dashboard', 'quickstart'], ['/account/api-info', 'api'], ['/account/settings', 'account'],
    ['/account/certificate', 'certification'], ['/account/notice', 'notifications'], ['/account/printing', 'notifications'],
    ['/account/settlement-schedule', 'settlements'], ['/account/internal-transfer', 'transfers'], ['/account', 'services'],
    ['/trade/order', 'orders'], ['/trade/refund', 'orders'], ['/trade', 'reports-and-support'],
    ['/settle', 'settlements'], ['/transfer', 'transfers'], ['/record', 'reports-and-support'],
    ['/domain', 'api'], ['/complain', 'reports-and-support'], ['/applyments', 'reports-and-support'],
    ['/profitsharing', 'sharing'], ['/subchannel', 'collection'], ['/onecode', 'collection'], ['/invite', 'services'],
  ]
  const suffix = p.slice(row.area.length + 1)
  const found = maps.find(([prefix]) => suffix === prefix || suffix.startsWith(prefix + '/'))
  if (!found) throw Error(`Undocumented menu: ${p}`)
  return `../${row.area}/${found[1]}.md`
}
