# 显式可信代理来源

仅增加独立 HTTP middleware，以 `GOEPAY_TRUSTED_PROXY_CIDRS` 显式配置直接代理来源。默认空，保持只信任 TCP 对端。命中配置的代理必须覆盖 `X-Real-IP` 为单一 IP 字面量，middleware 校验后统一改写内部 RemoteAddr，供支付风控、登录限流、验证码限流和审计消费；保留原对端在 request context。永不读取 X-Forwarded-For/Proto，不信任客户端自报代理链或DNS名字。

非法CIDR启动失败，禁止 /0 全网信任。可信代理缺少/重复/含端口/链/zone/非法IP的头返回400；不可信对端的所有转发头直接忽略。配置应优先使用专用代理的 /32 或 /128，不能用整个包含非代理容器或宿主直连网关的Docker网段。当前本地Compose不自动扩大信任。

TDD覆盖默认关闭、非可信伪造、多值歧义、IPv4/IPv6规范化、端口不参与身份、原transport对端保留、XFF不消费、坏配置及真实HTTP wrapper。
