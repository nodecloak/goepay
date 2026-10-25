# 显式可信代理

API 默认只使用 TCP 连接来源，不采信客户端的 `X-Real-IP`、`X-Forwarded-For` 或 `X-Forwarded-Proto`。使用 Nginx 等代理时，可用环境变量 `GOEPAY_TRUSTED_PROXY_CIDRS` 设置逗号分隔的直接代理 CIDR，优先精确 `/32` 或 `/128`。非法配置或全网 `/0` 会使启动失败。

只有直接 TCP 对端命中该配置，才消费一个由代理**覆盖**的 `X-Real-IP`。值必须为单一 IPv4/IPv6 字面量，不允许端口、zone、逗号链、重复头、空值、未指定地址或组播。地址规范化后统一提供给支付风控、持久登录限流、验证码限流及审计；原连接对端仍保留在 request context，可通过 `server.TransportPeer` 取得。此配置不改变协议、站点 Origin、Cookie Secure 或回调地址配置。

当前 Compose 传递此可选变量，但默认空，不自动信任 Docker 网段。不要将包含宿主直连网关、非代理容器的整个共享子网配置为可信代理；否则这些对端也能自报客户地址。应为专用代理使用稳定独立地址，并让边界代理覆盖该头。随附 Nginx 的 console、collect、healthz 代理入口均覆盖 `X-Real-IP`。

未配置时，Nginx 后的请求仍按代理地址合并来源预算，这是明确的默认行为。启用配置后须在本地或预发布部署校验：合法代理能得到客户来源；绕过代理直连 API 伪造转发头不能改变身份。单元回归已覆盖 IPv4、IPv6、mapped IPv4、默认关闭、非可信伪造、多值/链/zone/端口及非法配置，不依赖生产环境。
