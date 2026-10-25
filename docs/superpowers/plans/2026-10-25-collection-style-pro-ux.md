# 聚合收款码样式编辑 Implementation Plan

**Goal:** 恢复原Pro专用样式列表/直接编辑路由及真实可下载海报，字段和布局由实际渲染消费。

**Architecture:** `internal/collectionstyle`负责受限布局、图片归一化与内容寻址素材；后台资源持久样式，专用版本更新防覆盖；商户已绑定码的链接返回公开布局，由共用Canvas渲染器生成真实QR海报。保留旧简单颜色样式和现有签名收款链接。

**Tech Stack:** Go/PostgreSQL、Vue/TypeScript、现有qrcode库/Canvas。没有新的外部服务。

**Spec:** 本地原 `epay_pro/public/static/assets/index-C_AO16KG.js`、`index-Dc9Owjji.js`、`index-DVR_b5pg.js` 和全部菜单UX复核表UX-004。原新建/编辑参数name,label,sort,status,url,img_width,img_height,qr_width,qr_height,qr_left,qr_top,foreground,background,font,font_color,rec_name_left,rec_name_top,word逐项对应；接口内部沿用enabled表示status。

## Global Constraints

- E:\GOEPAY；不访问生产/导入真实凭证/发起真实资金交易。其它业务插件暂缓不影响此项。
- 不执行原PHP/JS。旧颜色样式继续可用，新布局需完整且在画布边界内；字体/颜色/URL有明确校验。
- 上传PNG/JPEG≤1MiB，解码≤4MP且边长≤4096，重新编码≤4MiB，拒绝SVG/动画/脚本。默认900×1200背景可用。
- 图片初始私有，仅在启用样式实际引用后公开；停用/删除后即时拒绝新请求，不借用页模板的发布权限。
- 不在服务器抓取远程地址；HTTPS背景由浏览器在明确预览时读取，导出需CORS，建议本地上传。实际测试只用本地合成图。
- 画布、二维码和收款方文字预览/下载使用同一渲染函数。拖动/缩放只改变显式表单值，不自动保存。
- 专用更新携带原revision，过期409；通用接口不能无版本覆盖新布局。样式启用不代表支付通道成功。

## Tasks

- [x] 先增加HTTP失败用例：完整原字段创建、边界/字体/URL拒绝、链接消费布局；实现类型、版本更新与旧样式兼容。
- [x] 增加85迁移只存不可变背景素材；测试上传归一化、私有/启用引用/停用回收、CSRF/角色和审计回滚。
- [x] 专用列表/编辑页：直接新增或按query.id加载，背景上传/尺寸同步、QR颜色/尺寸/位置、文字模式/字体/坐标、拖动/缩放、预览/保存/返回及冲突保护。
- [x] 商户现有详情生成链接沿用原签名有效期，共用渲染生成实际可下载PNG；保留旧纯二维码兼容。
- [ ] 单元、真实数据库、桌面/手机浏览器及固定源码全套前端回归；最终镜像部署前再核对迁移与实际下载像素。

商户原 `/onecode` 是直接生成表单，另有收款方名称、选择域名、风格、固定金额。当前记录详情消费新布局不等于这些原流程已齐全，后续单独实施，UX-004仍保留未完成。
