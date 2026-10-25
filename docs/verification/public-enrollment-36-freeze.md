# 邮箱注册/恢复36阶段冻结记录

2026-10-23，仅本机真实PostgreSQL随机schema + 注入内存Sender，无真实邮件、生产数据或资金调用。Server/cmd挂钩由父任务接入；本文证明本阶段领域与HTTP行为，不是所有后台Auth动作或邮件送达适配器全部完成。

## 实现

`internal/enrollment` 公开 `New(pool,store,key,Sender)`、`NewHandler(service,trustedPublicOrigin)`。路径 `/console/v1/public/{capabilities,verification-code,register,recover-password}`。验证码7位、10分钟、HMAC存储、来源/邮箱持久配额、5次失败、单次消费；发送器缺失/失败503，未知邮箱恢复同形响应。注册与邀请码、唯一已验证邮箱、Argon2用户、加密随机API密钥和零余额账户同TX；恢复与auth_version、删除全部商户会话、消费证明和审计同TX。保留MFA、不自动登录、不返回API密钥。重发和凭据版本变化使旧证明不可再授权。

商户创建的原事务API保留，通过 `CreateMerchantInTx` 复用原加密/零账户逻辑，未直接修改资金余额。后台布尔配置 `registration_invite_required` 默认false；`registration_open` 默认false。已验证邮箱独立于可编辑profile邮箱。

## 红绿及验证

初始领域红测返回 `verification delivery unavailable`；HTTP初始红测返回503；新增重发红测确认旧OTP仍能恢复，修复后全部转绿。独立复核随后发现锁等待跨过期P2，已重开冻结并补受控PG红绿：register/recover等待挑战行锁、注册等待邀请码行锁，旧实现三项均错误成功；每项均先用 `pg_blocking_pids` 确认阻塞，再由持锁连接 `clock_timestamp()>=expires_at` 证明过期后释放，未使用宿主时钟推算数据库到期。

修复后先锁挑战，再独立SQL检查数据库墙钟；最终消费也使用 `clock_timestamp()` 过期条件并要求恰好更新1行。业务在savepoint内执行，若邀请码/账户/审计等待导致晚过期，回滚账户/邀请或密码/会话/成功审计，外层仍提交来源/邮箱限流。新增恢复账户行锁跨过期回归确认旧密码、auth_version与会话保留。验证码错误次数持久化和正常并发一次消费已有回归仍通过。

```text
go test ./internal/enrollment ./internal/identity ./internal/backoffice ./internal/payment \
 -run 'TestEnrollment|TestRegisterVerified|TestVerification|TestPublic|TestMerchant|TestCreateMerchant|TestPersistent|TestSettlementMinimum|TestT1' -count=1
ok goepay/internal/enrollment 37.666s
ok goepay/internal/identity    1.793s
ok goepay/internal/backoffice 10.040s
ok goepay/internal/payment   19.323s
go vet ./internal/enrollment ./internal/payment ./internal/backoffice ./internal/identity
exit 0
```

17个enrollment顶层测试（其中锁等待含register/recover子用例）含：并发发码仅1次、邮箱/IP配额跨请求生效、错误次数跨实例保留、用途/邮箱/IP/过期绑定、默认关闭和配置变更、发送器失败、邀请码并发max_uses与回滚、唯一身份、12并发同证明只注册一次、加密API密钥/零余额、旧会话撤销/MFA不变、profile邮箱不能授权、未知邮箱同响应、重发及auth_version撤销旧证明、注册/恢复审计故障全部回滚、strictJSON及不回显秘密、挑战/邀请码/账户持锁跨过期拒绝且限流保留。

## 冻结摘要

| 文件 | SHA256 |
| --- | --- |
| db/migrations/000036_public_enrollment.up.sql | 27A33EB8AEA66CB609FF898EEEA6DD8634B425105DD0CD03BD9646F28B4AABC8 |
| internal/enrollment/service.go | F3326D6FCC265C25783F2B800BCC70E6D103726594E990991C26503C6FBC64CD |
| internal/enrollment/http.go | 69854EA2E3E77931792C8D07E6D3E9126E87F52A63A2118ED67CDC3550908D94 |
| internal/enrollment/service_test.go | D3C7B45D0FF9412FBBAD10B9B1DE8A5226A15D090A36A464FABB69932F2F880B |
| internal/enrollment/http_test.go | 58C35E0734E699D4FE3F54B62BE90E34FCD2E96721D71B4C06A0BED49000AD28 |
| internal/enrollment/expiry_test.go | 3CA2AB58A79F862E8EFAF6328B1209C37639471AB33EEB284C248A86B0FCCCCE |
| internal/payment/backoffice_money.go | C3402D9E68F4A35A9CD4F719F5B024BFC524C191E176A9EF075F6CC70BB5AF65 |
| internal/backoffice/enrollment_settings.go | A518C294A6B0E949F69DB6CAF7F5294911C81BB7B6A64E4F1C14D4E721B1772D |
| internal/backoffice/enrollment_settings_test.go | B6C94945EB1F1E140067F018AAB71340452B90F943F70FDEE237F863A56D99AC |

`service.go`（backoffice）仅增加注册邀请码布尔设置校验，仍由父任务维护其它动作，未对共享文件整体冻结。

本次P2独立复核已关闭，未发现新P1/P2。复核者校验上述service/迁移摘要一致；独立重跑同四包完整定向通过：enrollment32.975s、identity1.871s、backoffice10.600s、payment19.353s，四包vet为0。复核确认锁后数据库墙钟、最终消费条件、savepoint业务回滚/外层配额提交及错误OTP计数均正确；迁移36没有变化，现重新冻结。

## 保留缺口

默认运行配置没有真实邮件Sender，发码明确503；测试内存Sender不是上线送达实现。旧商户需要另行实现已登录、当前密码/MFA保护的可信邮箱绑定，不能自动导入profile邮箱。手机号短信、付费注册/审核、原Pro加密规则、外部社交登录绑定没有因此完成。配额是首次请求起固定24小时窗口，完成验证码绑定同一连接IP；这些是明确新规则而非声称逐字复制旧Pro。
