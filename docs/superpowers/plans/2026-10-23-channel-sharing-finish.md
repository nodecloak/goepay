# 独立渠道完结 PF 实施计划

本计划只使用本地合成密钥、TLS上游与临时PostgreSQL；不访问生产、不发起真实交易。

## 已确认协议

- 微信直连V3有独立 unfreeze 提交和原PF号查询。提交受理或FINISHED本身不代表释放；原交易、原PF号、渠道单号、唯一收款方为原MchID、金额等于原gross减已成功分账，结果SUCCESS且查询FINISHED后才记账。
- 支付宝原PS显式royalty_finish=true结合全部可信接收结果可证明约定完成；独立空PF的终态证明尚未证实，因此不开放其自动放款。
- remaining=0不能单独证明某个未发送的PF成功。不得用事后可分余额倒推出已提交或已释放。

## 持久状态与互斥

使用新55迁移。原支付必须有正向分账声明、可信支付凭证、有效专属reserve。PF仅允许未提交PS、或原PS已有明确未受理结果；pending/claimed/unknown的PS禁止换成PF绕过核对。已可信autoFinish完成则只读返回原结果。

以原订单锁串行化PF/PS/退款/站内分配；每单最多一份非failed的PF意图，原PF号固定120bit随机值，保存原支付账号与gross/net/已成功分账额/expectedUnsplit的HMAC。资金授权、意图、审计、只读任务同事务提交，只有首个claimant允许提交一次；未知结果不换号、不重发。仅全部历史PF均已验证为可信failed后，操作者可用新externalNo、新授权、新PF号显式重试；新意图封存previous_finish_id。旧externalNo始终只读原失败结果，不重新占用授权或重发。

查询通过原账号和原请求核验。可信成功后的release journal仅从原专属reserve到原商户available；平台费不补贴给商户。结果HMAC在读取及后续追认前复验。

## 统一资金完成视图

建立统一原单释放投影，汇总现有PS autoFinish完成和可信PF完成，两种方式必须互斥。更新资金invariant、活动receiver引用、普通退款/站内分配/收入冻结资格和T+1来源判断，禁止仅因历史存在funding就永远阻塞，也禁止PF完成后跳过当日收入限制。PR回退独立增加原单可用收入，不增加PF的上游未分余额。

## 必须测试

真实本地TLS+PG覆盖：无PS独立PF、明确拒绝PS后PF、部分成功autoFinish的只读结果、pending/unknown PS阻塞PF、PF受理遗失仅查原号、并发一次授权一次提交、上游空结果/错Mch/错金额/非终态不释放、审计失败事务回滚、重放无二次释放、费用gross/net区别、原账号切换前后无错误通道I/O、退款与T+1/站内分配兼容。专用HTTP的RBAC/CSRF/资金授权和双后台操作随后接入，不用普通资料CRUD代替。
