# 用户组购买设置与真实购买规则修复计划

**Goal:** 管理员路由用于配置销售，不再执行会员购买；配置的上下架、价格、月有效期、可见用户组、排序和简介由商户购买真实消费。

**Architecture:** 新增受权限和乐观版本保护的 membership-sales 配置接口及专用页面，所有配置与审计同事务。购买保留旧按天配置兼容，显式月配置按原 PHP 的日历月溢出及北京时间日期零点处理，0月为永久；数量1..300，金额按整数分相乘。新增84迁移仅添加不可变购买条款快照并放宽旧按天字段，旧1..83迁移不改。私有原购买HMAC绑定条款，旧无条款的购买保留原证明格式，历史旧schema继续可验证。

**Tech Stack:** Go、PostgreSQL、Vue3、Vitest、Playwright。

**Spec:** 本地原 GroupBuy 模块及 epay_release/user/ajax2.php 的 num、expire、price、endtime 规则；全页面复核 UX-002。

- [ ] 数据库失败用例证明月/永久/下架当前未消费；再实现整数金额、期限、上架和可见范围。
- [ ] internal/payment/membership_sale.go 管理条款与期限；membership_deposit.go 事务消费、重放数量校验；membership_referral_source.go 按总价计算原奖励并绑定快照；backoffice/business.go 接受数量并投影真实条款。
- [ ] internal/server/membership_sales.go 提供管理员配置与商户可购买目录，资金授权摘要绑定数量、销售配置及总价。
- [ ] GroupSalesPage.vue 恢复配置页；CommercePage.vue 使用可购买目录、数量与月/永久展示，不把30天改名为1月。
- [ ] 测试月份月底溢出、续期、永久拒绝重复购买、数量与价格溢出、关闭/下架/可见范围、相同请求号重复及改变数量冲突、资金证明篡改和配置失效；运行历史会员奖励和资金授权回归。
