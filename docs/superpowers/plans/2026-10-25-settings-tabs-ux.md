# 原17类系统设置界面修复计划

**Goal:** 系统设置使用原分组、中文字段及适当控件，代替要求管理员逐行维护设置键/值。

**Architecture:** 只渲染当前服务端schema允许且实际消费的设置，按原17类分组，分/基点用精确字符串转换展示元/百分比。分页读取已有设置，不给缺省项编造当前值，不自动写入默认值；只保存当前分组明确编辑的项。保存仍使用原受权限/CSRF/审计保护的资源接口，逐项显示结果，未知结果刷新核对，不能因部分保存失败称全部成功。部署管理的秘密或当前未实现的原PHP配置明确说明，不创建不消费的假设置。

**Tech Stack:** Vue3、Vitest、Playwright；既有Go设置接口不变。

**Spec:** 全后台 UX-003；本地 index-C6la0DtP.js 原 site/merchant/pay/complaint/risk/settle/transfer/oauth/notice/certificate/ocr/anounce/mail/sms/iptype/proxy/advanced 17类。

- [ ] 先通过App真实路由测试复现键值列表替代设置页、元/分转换与未编辑默认值被写入的问题。
- [ ] settings.ts定义受控分组、标签、控件和精确转换；SettingsPage.vue读取所有分页并仅写脏字段，处理部分成功、未知和只读权限。
- [ ] App.vue/menu.ts切换专用页面，既有商户消息设置及会员销售配置保持独立入口。
- [ ] 跑组件、类型、完整mock浏览器，验证375/1280px、键盘分组导航、请求字段和失败保护。其余不适用/暂缓配置不能写成完整原版能力已实现。
