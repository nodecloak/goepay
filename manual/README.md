# GOEPAY 使用手册工程

本目录是独立的中文使用文档站，面向管理员与普通商户。参考
[LiteLLM 文档](https://docs.litellm.ai/docs/proxy/docker_quick_start)的三栏导航、
步骤式说明、搜索和页内目录；使用自定义文档布局，内容按 GOEPAY 当前实现重写。
导航按 14 个业务模块组织，每个模块同时列出管理员与商户的操作入口与处理流程。

## 本地预览与构建

使用 Node.js 22.12+（本轮使用 24），在 `manual` 目录执行：

```sh
npm ci
npm run check
npm run dev
```

打开 `http://127.0.0.1:5188/manual/`。发布前执行 `npm run build`，静态产物在
`.vitepress/dist/`，部署到 `/manual/` 路径；`npm run preview` 可预览生产构建。
日常编辑请使用 `npm run dev` 自动更新；若使用构建预览，重新构建后需重启
preview，使预览服务识别新的资源文件名。
本目录不会自动部署到任何生产站，不修改支付程序或创建容器。

若必须部署到其它前缀，先修改 `.vitepress/config.mts` 中的 `base` 与 favicon
地址，重新构建并检查链接。不要直接在 `file://` 下打开构建 HTML。

## 内容维护

- `.vitepress/config.mts`：站点与中文本地搜索、`/manual/` 前缀。
- `.vitepress/navigation.ts`：按功能模块组织的分层导航。
- `.vitepress/theme/`：自定义布局、角色步骤页签、图表放大与键盘交互。
- `modules/`：跨角色业务模块的完整使用流程。
- `admin/`、`merchant/`：各角色的具体操作。
- `concepts/`：架构、支付时序、账务及权限。
- `reference/`：从菜单源码同步的全入口索引与术语。
- `diagrams/*.mmd`：时序与流程语义；`*.json`：架构和流程的显式布局。
- `public/diagrams/`：直接发布的明暗 SVG，无图表 CDN 依赖。
- `assets/screenshots/`：由当前应用前端在本地合成数据下拍摄的截图。

新增菜单或补充入口后运行 `npm run sync:menus`，逐项确认目标指南映射，再运行
`npm run check`。校验会检查可见菜单覆盖、页内引用、截图及图表文件。

图表已预生成并纳入版本控制，普通构建不需要浏览器或外网。修改 `.mmd` / `.json` 后运行
`npm run diagrams`；首次使用可执行 `npx playwright install chromium`，也可通过
`PLAYWRIGHT_CHROMIUM_EXECUTABLE` 指向现有受信任的 Chromium。脚本只访问本地文件。
浏览器验收使用 `npm run test:browser`，支持同一个浏览器路径变量。
验收读取生产构建，逐页检查桌面端与 320 / 375px 手机端，并测试下拉菜单、侧栏的
键盘与触摸交互、搜索、页内目录、主题以及图表横竖屏适配。结果与截图写入忽略的
`test-results/`，不会提交依赖、构建缓存或测试产物。

更新截图前先在 `web/` 构建当前应用，然后运行 `npm run screenshots`；如构建位于
其它目录，通过 `GOEPAY_WEB_DIST` 指向它。脚本只使用本地合成接口，拒绝写请求与
外部请求。更新后检查每张图及其 `provenance.json`，不要用旧构建冒充新页面。

## 内容证据和截图约束

以 `web/src/data/menu-*.json`、`web/src/menu.ts` 的补充菜单、实际 Vue 表单和
Go 业务实现为依据。费用、原单恢复、通知、认证与迁移说明另核对项目相应设计、
操作及验收记录；历史文档中的“尚未接入”不自动覆盖后续已完成实现。

截图标注为演示数据，不包含生产账号、订单、登录信息、API 密钥、完整证件或
真实收款资料。不要为了更新截图在生产触发支付、退款、结算、短信、打印或认证。
必要的机构侧正式产品验收由运营方另行安排。

完整验证范围与截图来源保存在 `VERIFICATION.md`。
本文件和验证记录供维护人员阅读，不进入面向用户的网站与搜索索引。
