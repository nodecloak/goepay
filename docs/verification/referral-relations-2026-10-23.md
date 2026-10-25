# 注册推广关系56验证

仅本地 PostgreSQL `127.0.0.1:55432` 的独立随机schema和合成邮件发送器。未访问生产、导入凭据或真实支付。56只建立推广关系，默认不发奖励；奖励57及退款撤销58独立交付，不能据本文宣称全量兼容或全库通过。

## 行为与证据

- `rp_` 加32字节随机数的推广令牌，默认90天有效。HMAC索引，AEAD保存，原owner/version/header/cipher/expiry独立HMAC封存。发行和轮换只在该显式响应提供一次明文；普通读取和列表不返回令牌、邮箱、OTP、cipher或proof。
- 严格商户owner actor发行/轮换/撤销，expectedVersion CAS与原actor审计同事务。失去发行响应只能显式轮换新版本，不能以重放重取旧明文。12并发相同版本只成功一次；审计失败不会撤销先前有效令牌。
- 新关系与新merchant、console account、已验证email、原register OTP消费、准入InviteCode使用、原public actor审计在同一注册事务出生。PromotionToken和准入InviteCode独立；不接受caller指定promoter_id，不从后端query参数自动绑定。
- 不允许旧商户补绑/重绑、自身或环、禁用推广人、过期/撤销/不存在/伪造令牌。merchant出生身份不可变，关系不可变。历史关系可继续验证其原令牌版本，不因当前令牌轮换、撤销或推广人后续禁用而抹除。
- 注册持原OTP及准入锁后创建不可见新merchant，再graph advisory→token current SHARE→promoter SHARE；令牌变更owner advisory→merchant SHARE→current UPDATE，无graph反向等待。数据库递归图guard保持启用。
- `ReferralRelationTx` 接受同一`referralReader`（Query/QueryRow）并验证原注册结构、历史token AEAD/HMAC、关系HMAC，供57原单快照复用。SQL约束只检查结构和审计，不能宣称验证HMAC。真实资金路径必须使用可信Store读取。

## 实际红绿

- owner token发行行为测试先真实红4.029s（未实现），绿4.097s。
- 注册显式promotion及无效token回滚先红7.080s（字段被忽略、假token被接受），绿6.736s。
- 过期token绑定先红4.223s，增加PG时钟expiry检查后组合payment10.167s/enrollment7.226s绿。
- 56支付guard专项24.155s绿：并发CAS、owner隔离、token审计回滚、历史关系、伪HMAC关系拒绝、自身及双向环数据库guard。
- 完整新增注册专项14.565s绿：关系审计故障发生在OTP消费后仍全回滚、原OTP和准入码可重试；8并发同凭据仅一个merchant/关系/审计且零journal；撤销或禁用推广人不烧OTP；HTTP严格JSON、显式body绑定、query不绑定、不回显token、不自动登录。
- 完整`go test ./internal/enrollment -count=1` 80.029s，exit0。
- `go vet ./internal/enrollment ./internal/payment`，exit0。

专项命令环境设置`GOEPAY_TEST_DATABASE_URL`到上述测试PG，使用本机Go工具链和独立缓存。最终支付专项与迁移重入结果见后续追加；Windows/Linux全库及race由根任务在完整56–58固定候选上另行验证，本文未声称这些结果。

最终重跑：`go test ./internal/payment -run '^TestReferral(Token|Owner|Expired|Relation|Historical|Database)' -count=1` 23.050s，exit0（同次db因正则无匹配测试不计迁移证明）；随后独立完整`go test ./db -count=1` 7.933s，exit0，实际验证迁移重入及checksum guard。
