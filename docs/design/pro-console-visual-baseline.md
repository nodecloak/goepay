# Pro 控制台视觉基线与复刻范围

取证日期：2026-10-24。只读取本地 `epay_pro/public/static/assets`、明文入口构建模块和现有 Go/Vue 前端；没有执行原 Pro bundle、调用其中接口、读取真实配置或使用外部字体/CDN。原站名称、头像和登录文案来自运行配置，静态资源不能证明其部署值；继续保留 GOEPAY 和现有可访问操作名称。

## 实际资源与精确值

| 项目 | 本地证据 | 值/行为 |
| --- | --- | --- |
| 管理端默认设置 | `index-IIq1IliX.js` 的 settingStore 初始对象 | LEFT；展开230px，收起枚举64px；默认展开；DARK 菜单；showWorkTab=true；面包屑和菜单按钮开启；同层唯一展开 |
| 商户端默认设置 | `index-_K93c9Ev.js` 的 settingStore 初始对象 | 同样230/64px；LIGHT 菜单；showWorkTab=false；面包屑开启；不默认添加管理端的工作页签 |
| 主色与菜单 | 上述两份 JS 的 systemMainColor/themeList | 首选主色 `#5D87FF`；深色侧栏 `#191A23`，名称 `#D9DADB`，菜单/图标 `#BABBBD`；浅色侧栏 `#ffffff`，菜单 `#29343D`，图标 `#6B6B6B` |
| 页面与文字 | `index-2g6phfp_.css` / `index-nN4maGP0.css` 的 :root | 页底 `#fafbfc`，卡片 `#fff`；边框 `#00000014`、普通输入边框 `#e2e8ee`；灰阶 `#f9fafb/#f2f4f5/#e6eaeb/#dbdfe1/#949eb7/#7987a1/#4d5875/#383853/#323251` |
| 圆角与控件 | 同上 CSS 与 JS customRadius | customRadius=.75rem（基准16px时12px）；普通控件36px高；基础控件圆角 `customRadius/3+2px`=6px；标准卡片圆角 `customRadius+4px`=16px；表格卡片圆角 `customRadius/2+2px`=8px |
| 侧栏 | `index-CGCixCaQ.css` / `index-CigcHpOk.css` | 侧栏100dvh；品牌头60px高、文字18px、logo左距22px；窄屏品牌头50px；移动遮罩 `#00000080`；不展示滚动条但可滚动 |
| 表格/表单 | 上述全局 CSS，`element-plus-CKYNvudA.css` | Element Plus14px字，表单间距18px、标签32px基础行高（全局覆盖36px）；浅边框、白底、紧凑工具条。保留现有列/字段语义，只调排版、尺寸、色值与横向滚动容器 |
| 管理端登录 | `index-a-WavX4F.css` | 左图65vw、全高，`login-BjdLlRgv.webp` 居中铺满；品牌文字20px；底部标题24px/正文14px。存在本地图像不等于已确认可再分发许可，本次不复制图片/bundle，使用对应色块与本地 SVG 装饰表达相同分栏 |
| 商户端登录 | `index-BSlOwJiR.css`（另有相同版本 CYDDKOYp/DP1wVaEG） | 背景 `#f0f2f5` 到 `#e6e9f0`；1000×600白卡，20px圆角，`0 20px 40px #00000014`；左右各50%；左侧135deg `#3b82f6`→`#2563eb`；右侧padding60px；标题28px/600；输入40px，底色 `#f9fafb`，圆角8px |

原资源包含多种可配置主题和布局，不声称恢复了线上保存的用户偏好。取证摘录保存在 `.tools/pro-console-{evidence,layout-evidence,precise}.log`；不把可外呼的 Pro 模块加入当前构建。

## 实施计划

1. 先写真实交互 RED：桌面230/64折叠状态、路由变化后的菜单/面包屑、管理端工作页签选中/关闭和跨会话清空；商户浅色壳默认无工作页签；移动菜单选择与 Escape 关闭。CSS 不写镜像测试。
2. 新增独立 ConsoleShell 和本地 SVG 图标组件；App 只替换外壳与显示属性，保留原 login/logout、资金确认/CSRF、epoch/abort 与所有业务组件。MenuTree 维持现有可访问菜单名称、aria-current及路由，仅补真实折叠行为与图标。
3. 全局 CSS 采用以上精确主题值，管理端/商户端分别应用；表格、表单、卡片和登录统一接近 Pro 的密度与层次。375px用抽屉、可横向滚动表格和可换行工具条；不裁掉操作，不隐藏字段。
4. 运行 focused RED/GREEN、全量unit/typecheck/build及本地全mock浏览器回归；截图覆盖登录/管理/商户、桌面/375px。所有外联保护基于测试fixture的baseURL，不写死测试端口；禁止真实授权/支付/生产。根任务负责后续独立审查。

## 保留边界

只编辑 `web/src/style.css`、App外壳模板及展示计算、`components/MenuTree.vue`，新增壳/图标helper及专属测试。另一个 owner 负责字段/表列；不修改支付业务、限流、会话策略、来源证明、冻结71/72或 Docker/native71。页签只保留当前会话内已授权的菜单路径，不存 query/token、localStorage/sessionStorage，也不额外请求业务接口。

## 已实施与本地验证

已新增 `ConsoleShell.vue` / `ShellIcon.vue`，App只替换导航壳并透传原账户操作、默认业务内容与原有overlay；资金授权仍在壳外，原inert与会话/路由取消逻辑保留。管理端页签最多20个、首页固定，关闭当前页签返回相邻原页；不同area/session清空页签。MenuTree同层仅展开一个分组，切换路由恢复当前分组，折叠状态点击原分组会展开并保留子项。手机宽度不沿用桌面64px文字隐藏状态；关闭的手机菜单inert，Escape关闭并返回菜单按钮。

真实RED记录为 `.tools/pro-console-shell-red.log`、`pro-console-shell-rail-red.log`、`pro-console-shell-responsive-red.log`；最后focused GREEN为7项（含原App登录测试）。首次工作树全量unit为50文件174项通过，随后新增窄屏回归并重新通过focused。原console+新增shell的mock浏览器8项通过，窄屏修正后仅重跑新增shell4项全部通过；均基于127.0.0.1:5175的全mock接口与baseURL外联守卫，没有真实业务/授权外呼。vue-tsc和Vite build通过；单包超过500kB的现有提示仍存在，此增量不重构业务分包。

截图已实际打开检查：`.tools/pro-shell-{admin,merchant}-{desktop,375}.png` 与 `.tools/pro-shell-login-{admin,merchant}-{desktop,375}.png`。375px无全页横向溢出，原用户名/密码/动态码与原会话操作名称均保留；桌面商户登录背景修正为整屏一致浅灰。证据日志、截图和SHA256均在`.tools/pro-console-shell-*`，供主任务独立审查后统一集成冻结。

## 逐页补充对照（2026-10-24）

继续检查打印、账户资料/密码、自动结算参与、结算收款编辑和资源表单弹窗。原全局 input 样式使 checkbox 继承全宽/36px，实际截图确认后改为16px原生复选/单选；不更改 v-model 或提交行为。账户表单统一卡片、标题分隔、标签列，375px转为纵排；fieldset、禁用控件、明细与 JSON 长文本补齐紧凑间距和换行。截图 `.tools/pro-settings-{printing,account,schedule,resource,recipient}-{desktop,375}.png` 均由本地 mock 生成；最后已实际打开检查账户/打印、结算参与和收款编辑、资源弹窗窄屏图。

字段 owner 的实际 RED `.tools/pro-console-search-width-red.log` 显示1280px工具条搜索输入被挤至24px；仅通过工具条换行与搜索组合理 flex-basis/min-width 修复，保留其 >=160px 真实回归断言，GREEN为 `.tools/pro-console-search-width-green.log`。未修改字段 owner 的映射或测试。

首页另据 `index-BayLJ6d8.js` 的 stats-cards 取证：卡片 `h-35`=140px、`px-5`=20px、数值26px/medium、右侧50px主色10%底图标。保留当前四个统计字段、单位和请求，只补上述排版及本地手绘 SVG，375px图标缩小避免挤压数值。不添加当前接口未提供的今日/昨日统计或曲线。补充证据 `.tools/pro-dashboard-research.log`；表单/结算原构建模块证据 `.tools/pro-settlement-{research,fields}.log`。

扩展后的 shell 浏览器回归9项通过（`.tools/pro-console-shell-settings-expanded.log`），全部接口mock、所有外联按baseURL origin禁止，没有真实保存；设置表单仅进入编辑和取消。最终focused unit 2文件7项通过（`.tools/pro-console-shell-unit-settings.log`），vue-tsc + build通过214modules（`.tools/pro-console-shell-build-settings-final.log`），仍有既有500kB分包提示。截图关闭有限CSS动画，避免捕获桌面切窄屏时侧栏过渡帧；最终4项对应截图再次通过（`.tools/pro-console-shell-visual-final.log`）。
