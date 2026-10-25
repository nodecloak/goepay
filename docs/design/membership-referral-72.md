# 余额会员购买推广奖励（72实施设计）

2026-10-24。仅实施原Go已经存在的余额会员购买。冻结83、普通订单奖励/利润成本71、旧迁移及既有证明域均保持不动。所有开发与验收只使用合成资料、本地PostgreSQL和本地HTTP；没有厂商请求或真实资金交易。

## 原代码证据与范围

Release `user/ajax2.php:837–880`：余额购买先扣购买价P并更新会员；使用购买者原配置，再让推广者组配置覆盖，`invite_open=1`且`invite_groupbuy_rate>0`时按`round(P*rate/100,2)`奖励推广者。Pro用户路由`route/user.php:95`存在groupbuy，后台有groupbuy_rate字段，但实际控制器编码，因此不能声称恢复其全部隐藏规则。Release外部tid4在`includes/functions.php:659–674`另有已付款处理路径。

当前Go `membership_deposit.go`已有购买唯一键`(merchant_id,external_no)`、扣可用余额、原子会员延期和资金动作校验，没有外部会员支付关联、会员退款业务或可信的tid4接收模型。本地检索上述Pro/Release会员路径未找到明确的会员退款动作；本阶段不增加没有证据的退款按钮、伪造厂商unknown或发送流程。普通订单退款、普通邀请奖励撤回和其unknown屏障均不能作用到本家族。

本阶段完整闭环：独立会员推广政策 → 原余额购买事务固定关系/政策/P/R → 原购买收入专属账户 → 同事务一次奖励 → 重放与只读证明检查。免费购买不奖励；原会员延期、并发购买与管理员基础组恢复行为保留。

## 明确的新安全合同

- 政策单独版本化，默认关闭，不复用普通订单fee/gross/profit政策。允许global、group两个scope；继承为global → 购买者购买前的有效组 → 推广者当前有效组，后者覆盖，nil继承而false/0明确覆盖。此继承与Go现有普通推广合同一致、与Release“原购买者配置后推广者覆盖”可说明，但不宣称Pro编码执行器逐项相同。
- rate为0..10000整数BPS，R按P乘rate后half-up到整数分，商余数计算避免溢出；所以0<=R<=P。这个上限是新账本可出资合同，原公开代码未证明相同限制。不从另一普通订单手续费、平台clearing池或其他会员收入补贴。
- 购买前锁定原有已验证邀请关系；不接受HTTP传入promoter/rate/成本/奖励金额。政策版本、原购买者/推广者有效组、关系ID及证明、P、R、原购买组/期限/价格快照在同一事务固定，后来换组/改政策不追发、不重算。
- 推广商户不可用时本次记录明确不奖励，不能改派他人；购买者不可用仍按原购买逻辑拒绝。有效组基础来源不明确时按现有组安全规则拒绝，不能猜组0。
- 任何required source/HMAC/audit/journal不完整时整体失败。缺少新事实不能回退旧清算池购买，不得根据marker是否存在来决定家族。

## 原购买收入资金链

72仅新增`membership_income`账户kind，owner=0、scope_id=原purchase_id、balance>=0，不能借用owner0 clearing负数例外。账户仅由本次原余额购买出资。

```text
原buyer.available --P--> membership_income(purchaseID)
membership_income(purchaseID) --R--> 原promoter.available
最终本purchase专属余额 = P-R
```

两笔journal、原purchase、原会员更新、源事实、奖励和审计全部在同一个PostgreSQL事务提交；第一笔沿用确定性`membership:<purchaseID>`业务键，明确新目标账户，第二笔使用独立purpose业务键。没有与clearing的中转，也没有伪造收入expense。P0不建收入账户、不做journal，但仍有明确无奖励源事实。R0或政策关闭时P仍进入本purchase专属收入账户，合法零奖励与缺证明不同。

账户集合在第一次post前完整建立并按ID排序一次锁定，包含buyer.available、本purchase收入账户以及可能的promoter.available。商户行也按ID排序锁定后重新读取关系和当前启用/有效组，避免buyer先锁、随后跨推广者造成反锁。原资金动作`membership`在锁后与最终政策快照重新验证；审计失败、余额不足或资金授权拒绝均不得留下purchase、会员延期或奖励。

## schema与证明家族

仅新增`000072_membership_referral.up.sql`，不修改1..71。

1. 原`membership_purchases`增加显式source_version：升级前现有行为为0，新Go出生为1。原表的immutable触发器继续有效。历史0只允许读取原结果，不回填新source/收入账户/奖励。新1必须有对应的独立源事实；本purchase的新收入账户也必须有同scope事实，避免删marker降级。新事实只能在原purchase出生事务生成，禁止给历史购买补奖。
2. `membership_referral_policies`、heads、audits：不可变版本及actor/HMAC，scope/revision CAS，global必须明确enabled/rate，group可继承。政策读取必须校验原版本审计与proof，不能将任意groups JSON当可信奖励配置。
3. `membership_referral_sources`：purchase主键，source_version1、buyer/promoter/relation、原两组、原政策版本数组、P/R/reason、收入账户ID、购买journalID、原purchase快照及source_hash。P0时账户/journal必须NULL；P>0时必须非NULL且exact金额、owner/kind/scope一致。SQL检查所有可空分支显式处理NULL，不依赖UNKNOWN通过CHECK。
4. `membership_referral_rewards`：purchase唯一键，原promoter、R、source_hash、确定性奖励journal和独立reward_hash。仅R>0允许存在；R0时必须不存在。不是普通`referral_rewards`表的假订单，也不使用ordinary PolicyIDs。
5. 独立不可变events：purchase/source/award各自的canonical actor、eventkey、subject hash和proof。全事件分页核对真实父事实，检测多余、孤立及合法HMAC但错误subject的事件。

新HMAC域为`goepay/membership-referral/<purpose>/v1\x00`，固定字段顺序；绑定原purchase的ID、external_no、buyer、购买组、P、天数、expires_at、actor、source_version和原关系/政策/账户/journal/金额。金额/ID用int64参与canonical proof；HTTP显示为十进制字符串。旧普通referral的v1/v2证明函数体及域永远不改。

## 幂等与历史

- 原merchant+external_no同group重放只读取并验证原购买/新证明，不读当前政策补奖；换group冲突，不能再次扣款。
- 同一购买并发只允许一条purchase、一次扣P和一次奖励R；不同external_no按原会员期限顺序延期。
- 旧source_version0保留原clearing journal与会员历史。不存在“升级补奖励”、从旧收入批量转入新预算或current policy重定价。
- 新source_version1少source、少award、少收入账户、错journal、错original relation/policy或多余event均由checker发现。source_version被改成0仍留下的新scope账户/新journal家族必须作为孤儿拒绝；不能只查存在的sources。

## 权限、HTTP与资金确认

购买继续原`POST /console/v1/{role}/workflows/memberships`，body只含既有merchant_id/group_id/external_no，不能设置奖励或成本。保留cookie会话、CSRF、tenant/admin权限、delegated限制及原资金确认。原`membership`资金预览摘要必须加入完整独立政策/关系/推广者/原P/R快照；推广者也纳入相关商户revision指纹，原确认不能在更改推广政策或关系后复用。

独立政策接口仅原super管理员本人的有效会话，使用新purpose资金动作和完整canonical body/CAS；不复用ordinary referral政策授权。拟导出typed Core API：`MembershipReferralPolicy`、`SetMembershipReferralPolicy`、`FundsMembershipReferralPolicySnapshot`及`FundsMembershipReferralPurchaseSnapshot`，具体签名在测试前与root确认。已有会员购买响应可增加安全的奖励状态/金额摘要，绝不返回source HMAC或原审计私有字段。

## 退款和外部tid4边界

本阶段没有外部调用，因而不会创造“厂商unknown”的假状态，也不提供没有Pro明文证据的会员退款入口。专属收入不能被普通订单refund/recharge/sharing/deposit路径选中，普通unknown退款不能释放或撤回会员奖励。

将来若原证据支持会员退款，必须独立设计：在原purchase锁内从其P-R剩余收入与该原奖励R合并全额P；推广者已消费导致R不足时整体阻塞；权益必须匹配原purchase并防后续续费回退；未知外部结果不能释放原预占。不能用普通fee资金或另一个purchase垫付。

外部tid4需先新增typed membership-payment intent、唯一order↔purchase关联、购买对象/价格/期限与真实签名PaidReceipt原子绑定、未付/unknown不授会员及不奖励、晚到/重复receipt幂等，并设计独立退款闭环。当前原Go没有此可信模型，因此本阶段不把普通MarkPaid或param当作tid4证明，不新增外部会员支付入口。

## 检查器与验证计划

在原CheckLedger只新增一个窄调用，复用InvalidReferralTerms/Rewards/Clawbacks及InvalidAccounts/Journals计数的相应含义；不添加无意义常零counter，不宽免原家族。独立新SQL结构视图和Go HMAC验证完整分页，核所有version1购买、所有收入账户、所有sources/rewards/events及原关系/原政策，不仅扫描有marker的记录。旧版购买按原预算语义保留。

实际TDD顺序：

1. 数学/政策：0/10000/半分/最大金额/非法率、nil继承与显式false/0，独立super/CAS/审计回滚。
2. 原子出生：原关系与三层组快照、P/R同源、缺政策关闭/零价/零奖励、收入不得负、原available不足整体回滚。
3. 幂等/并发：相同购买多并发一次奖励、不同购买期限累加、换group重放拒绝、政策变更重放不追发。
4. 证明负例：缺source/award/原policy/关系、复制HMAC、改版本/账户scope/journal、孤儿及尾页event、SQL NULL/错误金额，checker必须实际变红。
5. 兼容：原membership/deposit/expiry/baseline修复、原资金guard/普通referral+daily、冻结83旧账本20counter、无外部请求/无普通order reward串线。
6. HTTP真实契约：own/delegated/CSRF/RBAC、数字与重复/未知JSON字段、cost/rate/promoter注入、政策确认摘要锁后变化、原会员购买一次扣款一次奖励。
7. 独立审查后冻结新总候选，再以Windows/Linux完整测试、race/vet/build、schema71→72升级和本地UI验收收口。没有全量结果前不宣布全项目完成。

独占PG50新membership GUID DB与其余owner分离，统一共享两个DDL测试slot、单包-p1；不碰PG46/48或82/83独立容器。先完成设计由root审查，再进入runtime实现。

## 本轮实际增量验证

Root完成新独立政策资金target/normalize/resolve、购买private snapshot与推广商户revision绑定，以及后台dispatch。准确三字段单元实际RED后GREEN；真实PG/HTTP两顶层10.442秒通过：不同家族/变化body不能复用grant，变更会员政策后旧购买grant拒绝，重新授权300购买价只扣一次、150奖励一次、专属收入余150，重复原请求不扣款。委托merchant购买曾真实RED为201，修后403；本人merchant购买仍201。默认无政策GET全局id/revision0且nullable字段保持原API语义；UI允许精确未配置形状，写入仍要求明确bool/int。

完整TestMembership关联实际111.288秒通过，包括旧会员/保证金/到期/基础组、72真实历史迁移、并发、锁等待跨到期、审计故障回滚、余额不足、不借其它收入、SQL NULL/预算边界、来源缺失/版本降级、三层继承false/0以及102购买跨页及孤儿audit。前端60文件275单元、类型和构建通过；12本地mock浏览器含本政策及profit两时机、订单字段，9.5秒通过。首轮浏览器3失败来自新测试getByLabel选择器和错误sidebar类名，已修为实际combobox/console-sidebar；保留各失败快照。固定83未修改，最终新候选全量/部署仍待完成。
