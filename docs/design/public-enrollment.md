# 商户邮箱注册与密码恢复

## 源码依据与边界

Release `user/ajax.php:213,244,363,394` 分别为注册验证码、注册、恢复验证码和找回密码；`includes/lib/VerifyCode.php` 明确邮箱60秒冷却、滚动24小时邮箱7次/IP11次、失败5次及一次作废。Pro `route/user.php` 有 sendcode/reg/findpwd，Auth 控制器加密；Pro `app/common.php:136` 邮件转交 mail 插件。这里实现新后台邮箱流程，不声称恢复加密细则。

保留一次消费、邮箱/IP持久限流及失败次数。安全调整：验证码有效10分钟而非Release一小时；使用密码学随机7位码，存储域隔离HMAC，不保存/返回明文码；Argon2密码至少12字节；找回请求对未知邮箱同形响应。首阶段不实现手机号短信、付费注册、注册审核及OAuth登录绑定；这些不因此计作完成。

## 接口与授权

新包 `internal/enrollment`：`New(pool, store, masterKey, sender)`；`Sender.SendVerification(ctx,email,purpose,code)` 是显式同步发送接口。默认sender=nil，发送返回503；不写普通jobs，不伪造送达。测试只注入本机内存sender。`NewHandler(service, publicOrigin)` 返回可独立挂载的 `http.Handler`；部署发送器和服务器挂钩由父任务接入，不能自动连接真实SMTP。

独立 HTTP Handler 计划挂在 `/console/v1/public/`：

- GET `capabilities`：注册是否开放、邮箱发送是否配置、是否必须邀请码、有效期；没有秘密。
- POST `verification-code`：`purpose=register|recover,email`；响应opaque `challenge_id,expires_at`，没有code。来源仅使用连接IP，不信任客户端头。
- POST `register`：`challenge_id,code,email,username,name,password,invite_code`；成功仅返回商户ID/用户名，不登录或返回API密钥。
- POST `recover-password`：`challenge_id,code,email,new_password`；已证明邮箱所有权后统一成功响应；不创建登录会话，不清除TOTP。

跨站写入拒绝不匹配Origin，JSON严格解码/请求体限长；HTTP handler是独立可挂载对象，不擅改共享Server/main。

## 持久模型与事务

36增量只增加注册验证挑战、限流计数、已验证唯一邮箱及邀请码使用记录。挑战绑定purpose、邮箱HMAC、来源HMAC、固定恢复目标及发码时auth_version（未知邮箱为空）、到期、发送状态、失败计数和消费时间。验证码HMAC还绑定opaque挑战ID和用途，不能跨用途/邮箱/挑战使用，完成验证必须使用同一连接IP。失败更新和来源限流提交，不因返回无效码而回滚。发送计数采用首次请求起固定24小时窗口（邮箱7次/IP11次），验证采用15分钟窗口（邮箱30次/IP100次）；拒绝也占计数。与Release逐条统计滚动24小时的算法不是逐字等价。

已验证邮箱独立于可编辑的商户 `profile.email`。不自动信任历史资料、支付OAuth身份或邮箱字符串。已有商户若未完成可信邮箱绑定，不能仅因管理员资料填写邮箱就被公开恢复密码。首阶段已验证绑定由成功注册建立，旧商户的迁移绑定另行设计。

`registration_open` 缺省false且发送和完成注册都检查；`registration_invite_required` 缺省false。邀请码启用与 `max_uses` 在同一事务锁行计数；不使用前端“已验证”标志。用户名遵循现有大小写敏感主键，邮箱规范化为小写并唯一。

注册事务：检查注册策略→锁/验证挑战→业务savepoint→Argon2密码及唯一商户ID→创建加密随机API凭据及零余额账户→登录用户及唯一已验证邮箱→锁/消费邀请码→审计→最终条件消费挑战→提交。任一步失败不产生半个账户、不消耗邀请码或验证码。通过Store现有商户创建逻辑的事务helper复用加密，不复制/公开主密钥。

到期校验不能使用锁等待前的statement_timestamp：取得挑战行锁后另发SQL检查clock_timestamp，所有业务写入和审计完成后再条件消费，条件包含仍未到期且恰好更新1行。业务变更位于savepoint内，等待邀请码/账号后若到期，回滚业务但提交外层来源/邮箱限流；无效码失败计数同样提交。实际审计先于最终条件消费，失败与过期都不能留下成功记录。

恢复事务：锁/验证挑战→匹配发送时绑定的已验证身份和auth_version→密码更新及auth_version变更→删除该商户全部会话→消费挑战→审计→提交。未知邮箱只消费挑战并统一响应；版本已变的旧证明同样不能覆盖新凭据。禁用商户不通过恢复重新启用，MFA秘密保持原样，下次仍正常验证MFA。不能以恢复凭据创建会话。重发同邮箱同用途验证码时，原未消费证明一并失效。

发送状态先提交sending再调用sender，成功标sent；失败标failed并返回503。进程在发送前/后中断的sending不能验证，也不自动重发；60秒后可新申请。响应或日志不透出验证码、密码、发送器内部错误。

## 本地验收

真实PostgreSQL + fake sender：并发冷却/邮箱IP限流、跨实例失败5次、过期/用途/邮箱绑定、验证码不落库、nil/failing sender失败关闭；注册关闭、邮箱/用户名唯一、邀请max_uses并发一次消费、零余额和加密API密钥；恢复不枚举、不接受可编辑profile邮箱、旧会话撤销、MFA保留、一次性并发与事务回滚；HTTP严格字段/连接IP/发送器503与不回显秘密。通过后附实际命令结果，不以本设计文档代替完成记录。

## Server / cmd 接入清单

1. 复用已验证的 `GOEPAY_MASTER_KEY` 32字节值和现有PostgreSQL pool、payment Store，调用 `enrollment.New`。不增加另一个隐式默认密钥。
2. 为Server提供 `http.Handler` 字段，在 `/console/v1/` 通用认证路由之前，将 `/console/v1/public/` 转发给 `enrollment.NewHandler(service, trustedPublicOrigin)`。publicOrigin来自受控公共地址配置，不能取请求Host或X-Forwarded头。不开放管理员注册。
3. 默认传 `nil` Sender；GETcapabilities明确email_enabled=false，POST发码503。`registration_open=true` 不能替代发送器启用。此阶段没有SMTP host/password环境变量被自动消费。
4. 启用发送时必须显式提供实现 `Sender` 的对象，遵守传入context的10秒超时；只将验证码交给指定收件邮箱，不日志、不放HTTP响应、不存普通outbox。首阶段验收只使用本机fake sender；真实SMTP/TLS连接和凭据加载仍是单独待完成的送达适配器。
5. 可由后台设置 `registration_open`、`registration_invite_required`；均缺省false。邀请码沿受限资源创建，实际使用记录不可改写，不能通过修改资料中的email建立恢复身份。
