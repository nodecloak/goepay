# 原商户支付状态与注册审核82 Implementation Plan

> **For agentic workers:** 在当前会话连续执行，用户已授权本地实现与测试；不用不可用的子代理。

**Goal:** 原pay0/1/2与user_review控制注册审核，独立于登录启停，服务端阻止未审核新收款。

**Architecture:** 一个新增列与原配置CAS事务；注册读取严格设置，付款订单/首次attempt在原事务检查，后台保留原字段和中文选项。

**Tech Stack:** Go、PostgreSQL、Vue、Vitest、Playwright。

**Spec:** `docs/design/merchant-payment-review-82.md`

## Global Constraints

- E:\GOEPAY；不访问生产、真实支付服务或真实资金；其他插件暂缓。
- 1..80迁移及94固定候选不可修改，新增schema81。
- pay不等于enabled；待审核仍可登录。只拦截新收款，不丢弃原可信付款事实。
- 所有管理员修改继续CSRF、角色和config_revision检查，金额不变。

### Task 1: 原字段及事务付款约束

**Files:** 新建 `db/migrations/000081_merchant_payment_review.up.sql`、`internal/payment/merchant_payment_review_test.go`；修改 `internal/payment/store.go`、`fees.go`、`provider_attempt.go`。

**Interfaces:** `Merchant.PayStatus int json:"pay"`读取原字段；CreateOrder/首次BeginProviderAttempt在原事务检查pay1，原ErrState不变。

- [x] RED：迁移后断言新列pay1、设置pay2后CreateOrder返回ErrState；已有created订单首次BeginProviderAttempt同样拒绝，已有attempt仍可只读恢复。
- [x] 添加列CHECK(pay IN(0,1,2))，旧行default1；读取用to_jsonb缺列兼容旧实形，修改新事务付款入口。
- [x] GREEN：原可信MarkPaid不受pay2影响；没有新增余额/attempt/重复资金；运行目标与原商户/订单/mini契约回归。

### Task 2: 已验证注册与管理员修改

**Files:** 新建 `internal/enrollment/payment_review.go`、`payment_review_test.go`、`internal/backoffice/payment_review.go`、`payment_review_test.go`；修改 `enrollment/service.go`、`backoffice/special.go`、`merchant_login.go`、`merchant_console_projection.go`。

**Interfaces:** 注册公开结果新增 `pay:string`、能力新增 `review_required:bool`；后台Schema字段 `pay` select默认1、0/1/2中文选项；user_review配置只接受0/1字符串。

- [x] RED：user_review1验证注册pay2、已有登录密码可验证、pay结果/能力准确；非法/重复配置事务不创建用户。
- [x] 在原注册事务读取设置并指定pay，后台仅在原CAS写事务的after钩子更新原列并审计；过滤profile同名字段。
- [x] GREEN：管理员审核不改账户余额/登录密码版本，越权/错误值拒绝；原default1注册与用户/渠道字段回归。

### Task 3: 原版中文状态与注册交互

**Files:** 修改 `web/src/enrollment.ts`、`components/PublicEnrollment.vue`；扩展既有enrollment单元/browser测试；检查商户通用Schema字段显示。

- [x] RED：待审核注册回包必须展示“待审核”，不声称可收款；新能力字段错误类型拒绝，旧响应缺字段兼容false。
- [x] 更新类型/解析/页面状态，使用原pay字段及中文标签；原参数字段和后台Pro样式不更换。
- [ ] GREEN：类型检查、完整前端单元、375/桌面浏览器与四包完整回归已通过；最终候选完整仓库/race及部署恢复仍待执行。

实际阶段证据见 `docs/verification/merchant-payment-review-82-2026-10-25.md`；不据此声明当前83主区或全项目完成。
