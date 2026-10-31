# 示例支付页模板（sample-pay）

这是一个可下载、修改后重新上传的**支付页模板（pay）**示例包。它只提供页面
外壳——品牌页眉、帮助提示和带版权的页脚——真正的收银内容由系统写入
`{{content}}` 处。

## 目录结构

```
sample-pay/
├── template.json      模板元信息（必需，根目录）
├── index.html         页面入口（必需，根目录，含且仅含一个 {{content}}）
├── preview.png        控制台卡片预览图（可选，根目录）
├── css/
│   └── style.css      样式
└── images/
    └── logo.svg       品牌 logo
```

打包为 `.zip` 时，文件可放在根目录或统一放在**一个**顶层文件夹内
（系统会自动去掉该层文件夹）。

## 限制与允许的文件类型

- 压缩包不超过 10 MiB；最多 300 个文件；单文件解压后不超过 3 MiB；
  解压后总计不超过 24 MiB。
- `index.html` 不超过 512 KiB；`template.json` 不超过 4 KiB。
- 根目录必须有 `template.json` 和 `index.html`，不允许其它 `.html` / `.htm`。
- 支付页允许的扩展名：
  `css`、`png`、`jpg`、`jpeg`、`gif`、`webp`、`avif`、`svg`、`ico`、
  `woff`、`woff2`、`ttf`、`otf`。
- **支付页和收款页不运行脚本**：包内不允许 `js`、`mjs`、`json`、`mp4`、`webm`，
  否则整个包会被拒绝。
- 以 `.` 开头的文件/目录、`__MACOSX/`、`Thumbs.db`、`desktop.ini`、
  `*.md`、`*.txt`、`*.map`、`LICENSE`、`README` 会被静默忽略（本 README 也在其中）。

## template.json 字段

- `type`：必须为 `pay`，且与上传到的类型一致。
- `name`：`sample-pay`。
- `title`：`示例支付页模板`，1–40 个字符。
- `version`、`author`、`description`：可选。

## 占位符（支付页）

语法为 `{{名称}}`，大括号内可有空格；取值会做 HTML 转义；页面没有的值渲染为
空文本。本示例用到：

| 占位符 | 含义 |
|---|---|
| `{{content}}` | **必需，且只能出现一次**：系统在此写入收银内容 |
| `{{title}}` | 支付页标题 |
| `{{brand_name}}` | 品牌名称（页眉、页脚版权） |
| `{{merchant_name}}` | 商户名称 |
| `{{year}}` | 当前年份（用于页脚版权） |

支付页还支持 `{{amount}}`、`{{trade_no}}`、`{{order_name}}` 与 URL 占位符
`{{logo_url}}`，可按需使用。

### 关于 {{content}}

`{{content}}` 必须在整段 HTML 中**恰好出现一次**，而且它的祖先元素只能是
`body`、`div`、`section`、`article`、`aside`、`header`、`footer` 之一（本示例把它
放在 `<div class="pay-body">` 里）。系统会在这里写入收银台卡片：**金额、订单号、
二维码、以及各类按钮**都由系统生成，无需你在模板里自己写表单或按钮。

## 不允许的内容（会被清理或拒绝）

支付页 HTML 会经过清理：

- 不运行脚本；`<main>`、表单、输入框、按钮、`<iframe>` 与内联 `<svg>` 会被
  移除并记录；`<title>`、`<meta>`、非样式表 `<link>` 会被静默移除。
- 图片只能用 `<img src>` 指向**包内图片**，其它来源的图片会被丢弃；不要使用
  外部 URL。
- CSS 中 `url()` 只能指向**包内文件**或 `data:`；出现外部 `url()` 或
  `@import` 会**拒绝整个上传**。
- `<link rel="stylesheet">` 只能指向包内 `.css`；`<style>` 块与 `style` 属性
  会被保留并移入 `<head>`。

## 相对路径

资源使用**相对路径**即可（如 `css/style.css`、`images/logo.svg`）。渲染时
`<img>` 会被改写为 `/template-files/<key>/<path>`，CSS 内相对 `url()` 也会按包
根目录改写。

## CSS 变量（系统收银卡片）

系统在 `:root` 上声明以下变量（来自配置），并用它们给 `.goepay-checkout`
（即收银台卡片）排版。模板 CSS **在系统样式之后加载**，因此可以覆盖它们：

- `--goepay-accent`：强调色（主按钮等）；
- `--goepay-on-accent`：强调色之上的文字颜色；
- `--goepay-bg`：页面背景色；
- `--goepay-fg`：正文前景色；
- `--goepay-pad`：卡片内边距。

本示例在 `css/style.css` 顶部给出了这些变量的示例取值，并对 `.goepay-checkout`
做了轻量装饰（圆角、描边、阴影、内边距用 `var(--goepay-pad)`）。修改这些变量
即可调整收银卡片的观感。

## 版本与恢复

- 用**相同的 name** 再次上传会生成**新版本**，始终展示最新版本。
- 「版本」对话框里可以对历史版本执行**恢复**，把旧版本复制为一个新
  版本。每次上传或恢复都会产生一条管理员通知。
