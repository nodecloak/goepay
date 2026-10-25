# 40 settlement funding local verification

迁移：000040_settlement_funding.up.sql。SHA256 `6D98783AA1E5595D5DDE121A8EB5122AB92CDEE521C361B3CD49F6B5AC5FA9D7`。1..39未修改。GOTMPDIR/GOCACHE位于E盘.tools；所有PG测试使用127.0.0.1:55432临时独立schema，无真实上游/资金/secret访问。

真实红绿：

- 首个funding测试修前relation settlement_fundings不存在；修后独立账户、余额汇总、12并发拒绝和旧内部转账/审批通过。
- recipient API缺失编译红；实现后AEAD配置与独立request AAD、版本CAS、旧同号保持原快照、显式换版本冲突、缺配置兼容、审计失败回滚通过。
- backoffice新endpoint修前404；接线后申请/列表/拒绝显示实际资金和masked版本。
- 原plaintext Create修前成功、schema宣称write=true；修后generic Create/Update/Delete拒绝且schema写权限false，专用endpoint审计不含账号姓名。
- scoped账户普通外来journal在commit被deferred证据检查拒绝；局部损坏restore模型在journal仍平衡时被InvalidSettlementFundings检测。历史unscoped申请不被接管，仍从原池拒绝返还。
- parent新增授权等待跨日测试实际红：created_at落前日23:59:59；移动授权guard到取operationAt之前后，与账户等待跨日两例绿4.413s。

定向命令：

`go test ./internal/payment ./internal/backoffice ./tests/contract -run 'TestSettlementFunding|TestSettlementRecipient|TestSettlementTerms|TestSettlementMinimum|TestSettlementDay|TestInternalTransfersAndSettlement|TestLedgerCheck' -count=1`

结果payment33.365s、backoffice5.617s、contract3.293s，全PASS。后续guard日期两例4.413s PASS；`go vet ./internal/payment ./internal/backoffice` exit0。未自行运行全库/race，按root协调留给统一验证。

前端使用ui-ux-pro-max和webapp-testing指导，保持既有控制台布局与键盘label。新增helper单测1例，连原settlementterms helper共2例PASS；vue-tsc exit0。真实浏览器先红（缺快照列/缺配置panel）后绿：recipient3例4.7s，terms2+funds3+recipient2合计7例4.0s，之后新增原菜单专例也通过。测试覆盖修改清fee确认、原版本43显式提交、内存PII不进storage、缺配置禁提交、409无自动重发，以及原菜单不调用plaintext resource。App路由hook由frontend完成，API和全局41modal没有在40重复修改。

HTTP契约通过真实PG检查会话/CSRF/租户、原请求号与配置版本、防明文响应、reserved摘要，以及0 transfer rows/0 transfer jobs。40没有支付机构操作，因此没有编造TLS上游调用。本地live-expanded脚本仅在缺配置时建立明确synthetic接收资料，不覆盖已有配置；实际live统一验收由root安排，本轮未运行live。

边界：当前所有申请/审批external_payout=false。收款字段校验不是支付机构身份认证；AppID/渠道/产品须43首次绑定再核实。缺快照历史不自动接管，原plain records不自动删除。41二级凭据/授权由root与其它代理持有，不以40配置操作冒充资金出款授权。
