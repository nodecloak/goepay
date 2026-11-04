import fs from 'node:fs'
import path from 'node:path'
import { catalog, guide, manualRoot } from './menu-catalog.mjs'

for (const area of ['admin', 'merchant']) {
  const rows = catalog(area)
  const title = area === 'admin' ? '管理员全部菜单' : '商户全部菜单'
  let text = `# ${title}\n\n从当前菜单及补充入口同步。路径用于定位；账号权限、功能开关与已接入服务决定实际可用性。部分业务入口会按角色隐藏。\n\n`
  text += `## 可见业务入口\n\n共 ${rows.filter(r => !r.hidden).length} 个入口。阅读指南包含操作步骤与业务限制。\n\n`
  text += '| 菜单 | 入口路径 | 阅读指南 | 范围 |\n| --- | --- | --- | --- |\n'
  for (const row of rows.filter(r => !r.hidden)) {
    text += `| ${row.title} | \`${row.path}\` | [操作指南](${guide(row)}) | ${row.superOnly ? '超级管理员本人' : '按账号授权及功能条件'} |\n`
  }
  text += '\n## 从业务上下文进入的页面\n\n这些页面不一定作为独立菜单显示，从对应列表、安全入口或表单进入。\n\n'
  text += '| 页面 | 路径 | 阅读指南 |\n| --- | --- | --- |\n'
  for (const row of rows.filter(r => r.hidden)) text += `| ${row.title} | \`${row.path}\` | [操作指南](${guide(row)}) |\n`
  text += '\n菜单不存在时，先核对角色与功能开放条件，见[页面操作说明](../start/navigation.md)。\n'
  fs.writeFileSync(path.join(manualRoot, 'reference', `${area}-menu.md`), text)
  console.log(`${area}: ${rows.length} source paths documented`)
}
