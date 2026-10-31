# 示例首页模板（sample-home）

这是一个可直接下载、修改后重新上传的**首页模板（home）**示例包，演示了一个
响应式的支付服务落地页。

## 目录结构

```
sample-home/
├── template.json      模板元信息（必需，位于根目录）
├── index.html         页面入口（必需，位于根目录）
├── preview.png        控制台卡片预览图（可选，根目录）
├── css/
│   └── style.css      样式
├── js/
│   └── main.js        交互脚本（仅首页模板可用）
└── images/
    ├── logo.svg
    ├── hero.svg
    ├── icon-shield.svg
    ├── icon-bolt.svg
    └── icon-plug.svg
```

打包为 `.zip` 时，文件可以直接放在压缩包根目录，也可以统一放在**一个**顶层
文件夹内（系统会自动去掉这层文件夹）。

## 限制与允许的文件类型

- 压缩包不超过 10 MiB；最多 300 个文件；单文件解压后不超过 3 MiB；
  解压后总计不超过 24 MiB。
- `index.html` 不超过 512 KiB；`template.json` 不超过 4 KiB。
- 根目录必须有 `template.json` 和 `index.html`，且不允许出现其它
  `.html` / `.htm` 文件。
- 首页模板允许的扩展名：
  `css`、`js`、`mjs`、`json`、`png`、`jpg`、`jpeg`、`gif`、`webp`、`avif`、
  `svg`、`ico`、`woff`、`woff2`、`ttf`、`otf`、`mp4`、`webm`。
- 以 `.` 开头的文件/目录、`__MACOSX/`、`Thumbs.db`、`desktop.ini`、
  `*.md`、`*.txt`、`*.map`、`LICENSE`、`README` 会被静默忽略（本 README 也在其中）。

## template.json 字段

- `type`：必须为 `home`，且与上传到的类型一致。
- `name`：`sample-home`，形如 `^[a-z][a-z0-9-]{0,30}[a-z0-9]$`，不能使用保留名。
- `title`：`示例首页模板`，1–40 个字符。
- `version`、`author`、`description`：可选。

## 占位符（首页）

语法为 `{{名称}}`，大括号内可以有空格。占位符只在**文本和属性值**中生效，
不会出现在 `<script>` / `<style>` 内容里；取值会做 HTML 转义；未知名称原样
保留并记为警告。在 `href` / `src` 中，占位符必须是属性的全部内容，且只允许
`*_url` 占位符。

本示例用到的占位符：

| 占位符 | 含义 |
|---|---|
| `{{site_name}}` | 站点名称 |
| `{{title}}` | 首页主标题 |
| `{{description}}` | 站点简介（也用于 meta description） |
| `{{notice}}` | 首页公告文字 |
| `{{brand_name}}` | 品牌名称（页脚版权、logo alt） |
| `{{email}}` | 联系邮箱 |
| `{{qq}}` | 联系 QQ 号 |
| `{{year}}` | 当前年份（用于版权） |
| `{{home_url}}` | 首页地址（`/`） |
| `{{login_url}}` | 商户登录（`/merchant/login`） |
| `{{register_url}}` | 商户注册（`/merchant/login?enroll=register`） |
| `{{doc_url}}` | 开发文档（`/doc/`） |
| `{{test_url}}` | 在线体验（`/test`） |
| `{{agreement_url}}` | 服务协议（`/agreement.html`） |

> 首页还支持更多占位符（如 `keywords`、`org`、`accent_color`、`logo_url`、
> `qq_url`、`registration_open` 等），可按需在模板里使用。

## 脚本沙箱（仅首页）

首页模板可以运行自己的 JavaScript，但公开首页是在**隔离沙箱**（opaque origin）
中渲染的：

- 脚本**无法**读取 cookie、`localStorage` / `sessionStorage`，也**无法**调用
  管理后台接口（读不到登录状态）；
- 允许加载 **https** 资源（图片、字体、媒体等）；
- 允许弹窗和模态框。

本示例的 `js/main.js` 完全不依赖存储或网络，只做：移动端菜单开合、页面内锚点
平滑滚动、以及滚动揭示与数字递增动画，并且都尊重
`prefers-reduced-motion: reduce`（减弱动态效果偏好）。

## 相对路径

页面渲染时系统会在 `<head>` 后插入
`<base href="/template-files/<key>/">`，因此包内资源用**相对路径**引用即可
（例如 `css/style.css`、`images/logo.svg`、`js/main.js`），无需写成绝对地址。

## CSS 变量

系统会在 `<base>` 之后注入一段
`:root{--goepay-accent;--goepay-on-accent;--goepay-bg;--goepay-fg}`，取值来自
模板配置：

- `--goepay-accent`：主题强调色；
- `--goepay-on-accent`：强调色之上的文字颜色；
- `--goepay-bg`：页面背景色；
- `--goepay-fg`：页面前景（正文）色。

本示例在 `css/style.css` 顶部用 `var(--goepay-accent, #2563eb)` 等方式读取它们，
并提供回退值，这样配置改变主题色时首页会自动跟随。

## 版本与恢复

- 用**相同的 name** 再次上传会生成**新版本**，始终展示最新版本。
- 「版本」对话框里可以对历史版本执行**恢复**，恢复会把旧版本的文件
  复制成一个新版本。每次上传或恢复都会产生一条管理员通知。
