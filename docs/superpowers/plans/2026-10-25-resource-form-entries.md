# 本地进件资料专用表单入口修复

**Goal:** 原新增/表单路由直接显示真实本地资料表单，记录列表保持独立。用户已暂缓的真实进件插件不新增外部提交能力。

**Architecture:** ResourcePage复用现有schema、校验、权限和读写方法，增加明确entry（create/edit、规范ID、返回记录路径）。挂载只读取schema/当前记录；不读取无关列表、不自动POST。直接表单不弹遮罩；同页面切换资料ID仍使用已有代次与取消保护。保存后显示真实结果及返回链接，不自动开启另一次新增。

**Tech Stack:** Vue3、Vitest、Playwright；Go接口保持原样。

**Spec:** 全页面UX-010：merchant/applyments/create、admin/applyments/form、merchant/applyments/form。

- [x] App路由失败测试：直达新增、ID编辑、非法ID拒绝、初始化只读及保存后不自动重复。
- [x] resources.ts明确入口映射；ResourcePage.vue读取入口并复用已有表单，App.vue传递原query.id。
- [x] 保留其它资源列表、商户上下文切换、表单权限、读失败等回归；手机/桌面验证且始终标明本地草稿。专项7个组件用例（含原上下文）和6个浏览器用例通过；整套固定源码待再次执行。
