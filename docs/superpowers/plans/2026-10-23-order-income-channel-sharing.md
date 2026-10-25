# 原订单渠道分账后的收入归属

本次只新增52，并修改原收入冻结/证据、T1可用额及站内按单规则。50财务执行和51接收关系由根任务持有，不在此改动。

1. 普通退款：只有仍 held 的原收入或没有可信 completions 的渠道预留阻止退款。completed 的 merchant_funded 订单可以用商户本人 available 承担退款，不自动PR，不虚构返还。
2. 原单可归属金额：从原 merchant_net 扣可信成功PS净流出、已成功退款的 merchant部分，以及有原 reserve/review journal 证据、未 rejected 的站内按单分配；最低0。无来源证据、活跃渠道预留或待定退款拒绝冻结/新分配。PR暂为0，今后只能扣除有独立可信回款proof和journal的金额。
3. 冻结快照：新增独立 channel_sharing_amount/internal_allocated_amount，refund_merchant_amount保持真实原含义；旧行默认0并保留旧版证据，不把新算法回写旧事实。新插入由DB锁原订单并核对实际组成。
4. 站内规则继续按原gross×rate_bps向下取整，但上限是原单真实可归属余额。多个rule串行于原order，不能借另一单wallet余额重复分配。rejected分配须有真实原review返还journal才能重用预算。
5. T1排除活跃渠道预留；可信释放后，当天原gross减成功PS净流出参与保守阻塞。保留原平台费和部分/全额退款gross政策，不将已释放今日收入当作旧钱，也不把预留中的今日款再次挡旧钱。
6. 测试先红：真实现有TLS/PG PS fixture完成后的普通退款、旧钱/新钱T1、PS+退款+站内分配独立快照、多rule预算、审计回滚和SQL篡改。验证通过前不作完整或生产兼容声明。
