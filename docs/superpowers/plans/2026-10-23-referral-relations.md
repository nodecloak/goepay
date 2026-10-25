# 注册推广关系56实施计划

仅使用本地随机schema和合成数据。冻结55及payment fees/store/reservations不变，奖励由后续57/58独立实现，默认不开奖励。

1. 新referral_tokens不可变历史+referral_token_current可撤销版本指针；256bit随机rp_令牌（默认90天有效，服务端PG时钟核对，撤销/轮换即时失效），HMAC索引，AEAD保存，header/cipher keyed proof。严格merchant owner actor与expectedVersion CAS，发行/轮换只返回一次明文，读仅masked/owner/version，审计无明文或hash/cipher。
2. 新referral_relations唯一invited_id，promoter/token/version/原register challenge和HMAC封存。绑定只在新merchant/enrollment email/console/已消费有效OTP/register audit同一事务出生。不可补绑、重绑、自身或环；历史关系不随当前token撤销和推广人后续禁用而抹除。
3. 注册顺序保留原OTP验证、准入InviteCode、账户创建及proof消费；消费成功后追加promotion_token绑定，任一步失败都回滚关系/账户/邀请码/OTP消费。JSON只接受promotion_token，永不接受promoterID；不从URL query直接在后端绑定，不回显token。
4. 锁顺序：注册既有OTP/InviteCode锁→新生merchant账户→关系graph advisory→token_current SHARE→promoter SHARE；令牌变更owner advisory→merchant SHARE→current UPDATE。新生merchant未对外可见，token变更不等待graph锁，避免反向图锁链。DB递归检查环并冻结merchant出生时间。
5. 公开同包可信接口ReferralRelationTx(ctx,tx,invitedID)返回ID/InvitedID/PromoterID/TokenID/TokenVersion/ChallengeID/CreatedAt/ProofHash，无关系ErrNotFound，验历史token及关系keyed证据供57快照使用。
6. TDD覆盖令牌熵/AEAD/CAS/审计回滚/禁用和伪造，注册输入/OTP邀请码独立/假token回滚/并发同proof仅一关系/旧账户补绑自环SQLguard，以及源证据HMAC篡改。注册完整验证在enrollment包；payment提供正确出生事务fixture供57/58测试复用。
