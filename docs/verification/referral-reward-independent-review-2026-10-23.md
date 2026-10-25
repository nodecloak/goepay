# 57普通支付手续费奖励独立复查

只读审查`referral_terms.go`、`referral_policy.go`、`referral_reward.go`、`000057_referral_rewards.up.sql`及`fees.go`、`store.go`、`backoffice/business.go`的接入。仅新增独立回归`internal/payment/referral_reward_review_test.go`，未修改57业务实现或迁移。58仍在独立实施，本文不构成58审查结论。

当前没有未解决的重要finding。原订单的正负快照与关联关系使用HMAC复验；原fee快照不可变，奖励率整数0..10000 BPS并以原fee为上限，账户锁使用稳定顺序。充值目的与负快照同事务出生；支付回调只有可信原receipt与原fee入账后才能奖励，已支付重放复验原奖励而不再次发放。SQL约束只验证结构，不能证明HMAC，可信读取和账本检查保留此区别。

真实本地PG新增四项回归，命令`go test ./internal/payment -run '^TestReferralReview' -count=1`最终13.245s，exit0（初始前三项10.141s，随后补第四项及负快照checker断言后重跑）：

1. 默认关闭时出生的关联订单，即使之后开启50%也保持原负快照。新positive订单fee10奖励5；充值和零fee订单不奖励。四单手续费总30，奖励总5，fees余25、promoter5、原merchant370，账本0违规。
2. global→payer group→promoter group字段合并：promoter true/80%覆盖payer false/25%，之后改成nil继承只影响新单。旧单晚付仍奖励8、新负单0，fees余12。
3. 正常数据库约束启用时，构造结构完整但HMAC为零的负快照。支付拒绝ErrConflict、原单仍created、journal/inbox均0；账本检查明确报告InvalidReferralTerms=1，不将伪负证据当历史缺失。
4. 可信receipt写入之后，强制reward audit失败，验证receipt、reward、journal、inbox、两个商户余额全部回滚，订单仍submitting。移除故障后相同event/receipt可重放恢复，仅奖励5、账本0违规。

上述测试使用合成receipt直接验证Store事务边界，不声称新增真实上游/TLS签验覆盖。原签验链路由完整项目既有测试单独验证。未访问生产、真实资金或导入凭据；未据专项声称完整Windows/Linux全库或race通过。
