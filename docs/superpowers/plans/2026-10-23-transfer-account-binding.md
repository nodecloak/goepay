# 44 Transfer Account Binding Implementation Plan

**Goal:** 同AppID换微信付款商户号后拒绝继承原意图，未知仍只查可信原账户且不重发。

**Architecture:** 新增公开TransferAccountIdentity及HMAC固定scope表；新attempt同事务绑定，所有资金外呼入口校验；后台逐动作指纹复用同scope。仅新增44迁移，1..43已部署checksum冻结。

**Tech Stack:** Go/pgx/PostgreSQL；使用现临时TLS/隔离schema测试，不读.secrets、不操作生产。

- [ ] RED真实PG：同AppID更换account scope，普通unknown查询/凭证不调用新账户；批次首claim与结算首claim拒绝且保留unsent资金；同旧scope查询恢复，HMAC表不可改。
- [ ] 加adapter稳定公开身份与表/同事务绑定；以现provider/ApplicationIdentity保留内部模拟范围，缺历史真实绑定不补造。
- [ ] GREEN上述专项，再覆盖现Transfer/Bank/Batch/Settlement及HTTP/动作授权；明确新缺证据错误不被当作失败释放。
- [ ] 管理后台授权指纹绑定新scope，红测预览后账户变更旧grant拒绝。
- [ ] 适当PG/race/前端mock/live/本地升级与备份恢复，更新契约/历史核对边界。
