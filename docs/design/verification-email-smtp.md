# 邮箱验证码的 SMTP 提交

本增量为现有邮箱注册、密码找回提供 SMTP 发送器，复用 `internal/enrollment` 的挑战、持久限流、一次性消费和会话撤销逻辑。没有新增 outbox 或自动重发。它不代表原 Pro 的 `testMail` 管理接口、业务消息订阅、短信或 QQ 插件已经实现，也不代表已启用生产邮件服务。

默认 `GOEPAY_ENABLE_EMAIL_VERIFICATION=false` 或未设置时，API 向 enrollment 传入实际的 nil Sender。能力响应保持 `email_enabled=false`，申请验证码返回 503；开放注册设置不会替代邮件配置，更不会伪造发送成功或返回验证码。

显式启用须同时配置以下 API 进程环境变量；启用后的缺失或非法配置使启动失败，错误日志使用固定脱敏消息。配置构造过程不进行 DNS、网络或凭据文件读取，不发现历史导入配置。

| 环境变量 | 要求 |
| --- | --- |
| `GOEPAY_ENABLE_EMAIL_VERIFICATION` | 仅 `true`、`false` 或空；默认关闭 |
| `GOEPAY_SMTP_HOST` | 明确的 DNS 主机名或 IP；不接受 URL、用户信息、路径或端口拼接 |
| `GOEPAY_SMTP_PORT` | 规范十进制整数，1–65535 |
| `GOEPAY_SMTP_SECURITY` | 明确的 `implicit` 或 `starttls`；通常分别对应 465、587，安全模式不按端口猜测 |
| `GOEPAY_SMTP_USERNAME` | 非空、长度受限且无控制字符的认证账号 |
| `GOEPAY_SMTP_PASSWORD` | 非空、长度受限且无控制字符的认证凭据 |
| `GOEPAY_SMTP_FROM` | 单一 ASCII 邮箱地址，不接受显示名称、列表或头注入 |

SMTP 凭据仅交给 API 的验证码发送器，不能因为部署共享环境配置而传给不使用邮件的 worker。凭据字段不进入普通 JSON，发送器不记录邮件、验证码或认证协议内容，返回错误也不包含服务端原始回复。

`implicit` 在读取 SMTP greeting 前建立 TLS；`starttls` 要求服务端声明 STARTTLS 并成功升级，不能降级为明文。两种模式均要求 TLS 1.2 或以上、可信证书链和配置主机名校验。运行时使用系统 CA；代码中的显式 `RootCAs` 信任钩子用于本地合成 TLS 测试或明确的私有 PKI，不提供跳过验证选项。只有验证 TLS 后才使用服务端声明的 AUTH PLAIN 或 LOGIN。传输和协议使用 Go 标准库。[Go SMTP 文档](https://pkg.go.dev/net/smtp)、[RFC 8314](https://www.rfc-editor.org/rfc/rfc8314.html) 是协议参考，原 Pro 的低端口明文回退没有被复制。

每次调用只有一个 TCP 会话、一个收件人和一次 DATA 尝试；拨号最多 5 秒，整个调用最多 10 秒，并服从更短的传入 context。取消会关闭套接字。累计入站握手和 SMTP 回复限制 64 KiB，固定验证码 MIME 消息限制 4 KiB。邮件主题和正文由固定模板构造，仅允许现有 `register`、`recover` 两种用途及原七位验证码；不接受任意邮件正文、附件、额外收件人或 SMTPUTF8 地址。

仅在 DATA 最终收到 250 后返回成功。这表示 SMTP 服务器接受了提交，不能证明收件箱送达。已经接受后不等待 QUIT 回复，因此 QUIT 的连接丢失不会把一次已接受提交误报为需要重发。拒绝、超时或最终确认丢失均返回固定错误，不重连、不查询、不重发。已有 enrollment 流程尝试将该挑战标记为 failed 并返回 503；即使测试服务器在丢失确认前已经读到邮件，该挑战也不能用于注册或恢复。数据库状态写入失败或进程中断留下的 sending 同样不可消费；用户只能依照原持久冷却和配额明确申请新的挑战。

本地验证使用 127.0.0.1 临时端口、合成账号和合成证书，实际完成 TLS、EHLO、STARTTLS、AUTH、MAIL/RCPT、DATA 及最终回复；没有连接真实 SMTP 或发送真实邮件。协议测试覆盖缺少升级或认证、错误 CA/主机名、认证和收件人拒绝、DATA 拒绝、回复超限、最终确认丢失、取消与期限、头注入及不重试。配置测试验证默认 nil、严格开关、非法启用配置和构造零 I/O。

真实独占本地 PostgreSQL 与假 SMTP 的 HTTP 闭环已验证：验证码只在测试进程内从邮件取得，API 不返回验证码或会话；注册产生零余额商户；恢复更新密码和 auth_version、撤销旧会话，证明只能消费一次；未知邮箱保持同形响应；未配置或不确定提交返回 503 且证明不可消费。Mailer、原 enrollment 相关回归、整个 cmd/api 测试及三包 vet 通过；另一次独立审阅和本地 TLS/config 测试通过。开发证据索引为 `.tools/smtp-verification-delivery.md`、`.tools/smtp-verification-regression-green.log`、`.tools/smtp-independent-review.json`，这些记录不构成生产激活或真实送达证明。
