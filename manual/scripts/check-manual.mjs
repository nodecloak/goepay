import fs from 'node:fs'
import path from 'node:path'
import { catalog, guide, manualRoot } from './menu-catalog.mjs'

function allFiles(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap(item => {
    if (['node_modules', '.git', '.vitepress', 'test-results'].includes(item.name)) return []
    const p = path.join(dir, item.name)
    return item.isDirectory() ? allFiles(p) : [p]
  })
}
const pages = allFiles(manualRoot).filter(p => p.endsWith('.md'))
let links = 0, diagrams = 0, menus = 0
const errors = []
for (const file of pages) {
  const text = fs.readFileSync(file, 'utf8')
  if (/go\.ma3fu\.com|p\.ma3fu\.com|BEGIN (?:RSA )?PRIVATE KEY/.test(text)) errors.push(`Private deployment material in ${file}`)
  const refs = [...text.matchAll(/!?\[[^\]]*\]\(([^\s)]+)(?:\s+"[^"]*")?\)/g)].map(m => m[1])
  refs.push(...[...text.matchAll(/href="([^"#]+)"/g)].map(m => m[1]))
  for (let ref of refs) {
    if (/^(?:https?:|mailto:|#)/.test(ref)) continue
    ref = decodeURIComponent(ref.split('#')[0].split('?')[0])
    if (!ref) continue
    const target = ref.startsWith('/') ? path.join(manualRoot, ref) : path.resolve(path.dirname(file), ref)
    if (!target.startsWith(manualRoot + path.sep) || !fs.existsSync(target.replace(/\.html$/, '.md'))) errors.push(`Missing local link: ${path.relative(manualRoot, file)} -> ${ref}`)
    links++
  }
  for (const match of text.matchAll(/<Diagram\s+name="([a-z-]+)"\s+caption="([^"]+)"/g)) {
    for (const mode of ['light', 'dark']) {
      if (!fs.existsSync(path.join(manualRoot, 'public/diagrams', `${match[1]}.${mode}.svg`))) errors.push(`Missing diagram ${match[1]}.${mode}`)
    }
    if (!['mmd', 'json'].some(ext => fs.existsSync(path.join(manualRoot, 'diagrams', `${match[1]}.${ext}`)))) errors.push(`Missing diagram source ${match[1]}`)
    diagrams++
  }
}
for (const area of ['admin', 'merchant']) {
  const index = fs.readFileSync(path.join(manualRoot, 'reference', `${area}-menu.md`), 'utf8')
  for (const row of catalog(area)) {
    if (!index.includes('`' + row.path + '`') || !fs.existsSync(path.resolve(manualRoot, 'reference', guide(row)))) errors.push(`Uncovered ${row.path}`)
    menus++
  }
}
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log(JSON.stringify({ success: true, markdown_pages: pages.length, local_links: links, diagram_placements: diagrams, documented_menu_paths: menus }))
