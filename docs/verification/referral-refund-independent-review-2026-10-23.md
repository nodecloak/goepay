# 58/59退款奖励撤销及通道结果证据独立复查

审查范围：`000058_referral_refund_clawbacks.up.sql`、`referral_clawback*.go`、`reservations.go`、`provider_flow_operations.go`、退款资金授权摘要，以及修复衍生的`000059_refund_operation_evidence.up.sql`和`refund_result_evidence.go`。仅修改两个独立review测试及本文；业务修复由根任务和58负责agent实施。

最终当前审查范围内没有未解决的重要finding。本结论来自本地PG、合成凭据及临时TLS服务器，不代表生产验收、完整Pro兼容或全库/race已通过。

## 实际发现与修复复核

1. **P1：无签验通道操作缓存被重新签封为可信资金终态。** 原退款为可信unknown，托管为fees0/fee_reserved8/escrow2/promoter0。仅SQL将`provider_operations.state`改failed，同号重放就调用资金resolve并为假failed产生真HMAC，返回nil且资金变为5/0/0/5。首次真实红4.851s，增加明确金额断言后4.877s红。修复后同号重放只返回需要查询，不能根据缓存推进资金；原号真实RSA签验查询独立决定结果。
2. **恢复SQL缺口。** Go修复后，unsigned cache=failed会被旧terminal trigger挡住可信SUCCESS查询修复。扩展真实TLS组合12.583s红。新59保留旧迁移5不变，新增同事务原nonterminal→terminal审计、原订单/退款/通道/金额/原号及旧缓存状态绑定的不可变结果证据，仅允许相符可信结果修复缓存。不存在裸GUC、关闭生产guard或任意终态反转。
3. **新结果缺行被当作历史结果。** 59最初“有则验HMAC、缺则通过”无法区分新退款和历史/内部退款。真实红4.189s：新无奖励退款unknown，SQL构造正确restore journal、refund/op failed而不插结果证据，提交和公读均通过。修复后存量明确version1，新出生强制version2且不可降级；新终态要求provider或显式internal source证据及HMAC，父表checker检查缺行。内部可信Store方法在原非终态同事务封存internal证据，不能给已终态缺证据数据补签。

## 最终独立回归

`go test ./internal/payment -run '^TestProviderRefundResultReview|^TestReferralClawbackReview' -count=1` **35.040s，exit0**；`go vet ./internal/payment` exit0。

- unsigned cache分别failed/succeeded：同号重放均保持0/8/2/0；原RSA签验SUCCESS查询修复后2/0/0/0，仅POST1/QUERY1。可信CLOSED失败后同号三次仍只读，保持5/0/0/5。
- 另一单1000分贡献fee100/reward50，原100分退款98仍只回收原R5、贡献K3。unknown保持另一单fee50及reward50；failed仅恢复原5；显式新号全退100后仍保留另一单fee50、reward50、net900，账本0违规。单独首绿5.359s。
- merchant承担手续费时gross100/net90，全退100缺商户自身10分而拒绝。奖励回收、专属escrow、退款行全回滚，merchant90、promoter5、fees5、上游POST0。单独首绿4.139s。
- 新version2无结果证据：数据库完整事务拒绝，原refund仍unknown、钱包available40/reserved60、没有凭空结果记录。
- 不完整恢复模型：**只在测试随机schema关闭新`refund_requires_terminal_result` trigger**，保留全部账本/身份guard，落入结构平衡但缺结果证据的version2终态后重新启用。版本不能降1，公读、内部补签、同号重放均ErrConflict；父表checker精确报告InvalidProviderRefundResults=1，journals/accounts违规0。
- 全部guard正常启用时，分别构造结构合法provider/internal结果行但使用零HMAC，公读、内部补签和同号重放均拒绝；checker各报告1，未将结构正确误称为密码学验证通过。

上述两个review测试文件为`internal/payment/referral_clawback_review_test.go`和`internal/payment/refund_result_review_test.go`。原58完整SQL伪failed、重复/并发、签验失败、结果审计故障、全账户预锁及merchant FK预锁用其负责agent回归另行验证；本次未修改相关业务文件。
