# 显式可信代理

API 默认只使用 TCP 连接来源，不采信客户端的 `X-Real-IP`、`X-Forwarded-For` 或 `X-Forwarded-Proto`。使用 Nginx 等代理时，可用环境变量 `GOEPAY_TRUSTED_PROXY_CIDRS` 设置逗号分隔的直接代理 CIDR，优先精确 `/32` 或 `/128`。非法配置或全网 `/0` 会使启动失败。

只有直接 TCP 对端命中该配置，才消费一个由代理**覆盖**的 `X-Real-IP`。值必须为单一 IPv4/IPv6 字面量，不允许端口、zone、逗号链、重复头、空值、未指定地址或组播。地址规范化后统一提供给支付风控、持久登录限流、验证码限流及审计；原连接对端仍保留在 request context，可通过 `server.TransportPeer` 取得。此配置不改变协议、站点 Origin、Cookie Secure 或回调地址配置。

随附 Compose 为后台 Nginx（`web`）建立专用网络 `console_proxy`（默认 `10.203.117.0/29`），并固定其地址为 `10.203.117.2`；API 默认只信任这一个 `/32`（`GOEPAY_TRUSTED_PROXY_CIDRS=10.203.117.2/32`）。`web` 不加入共享的 default 网络，因此数据库、Worker 等其他容器都不能自报客户地址；宿主直连 `8180` 的请求也不经过该地址。随附 Nginx 的所有反代入口均覆盖 `X-Real-IP`。

如需更换网段，须同时设置 `GOEPAY_CONSOLE_PROXY_SUBNET`、`GOEPAY_WEB_PROXY_IP` 与 `GOEPAY_TRUSTED_PROXY_CIDRS`，三者必须对应。不要将包含宿主直连网关、非代理容器的整个共享子网配置为可信代理；否则这些对端也能自报客户地址。

## 宿主机还有一层边界代理时

常见链路是“公网 → 宿主机 Nginx/OpenLiteSpeed → 容器端口”。经 Docker 端口转发后，容器看到的对端通常是该网络的网关地址，而不是边界代理本身：

- 边界代理直连 API 端口：把 API 实际看到的对端（Docker 网关或边界代理地址，取 `/32`）加入 `GOEPAY_TRUSTED_PROXY_CIDRS`，并让边界代理用真实客户地址**覆盖** `X-Real-IP`（Nginx：`proxy_set_header X-Real-IP $remote_addr;`）。
- 边界代理先到随附 `web`：还需在 `web` 的 Nginx 中用 `set_real_ip_from <网关地址>; real_ip_header X-Real-IP;` 接收边界代理传来的地址，否则 `web` 只会把网关地址传给 API。

## 未配置时的表现与告警

未配置或配置不匹配时，请求头仍被忽略（保持安全默认），但同一代理后的所有用户会共用一个来源：登录来源预算（每 15 分钟 300 次）、单账号预算（每 15 分钟 10 次）、验证码与支付风控的 IP 规则都会合并计算，单个攻击者即可让全站暂时无法登录。为便于发现，API 在收到来自私有或回环地址、且带有 `X-Real-IP`/`X-Forwarded-For` 的请求时，会记录一条 `ignored client address headers from an untrusted reverse proxy` 警告（每 10 分钟最多一次，只记录对端地址，不记录头的值）。公网对端发来的转发头视为客户端自报，不告警。

启用配置后须在本地或预发布部署校验：合法代理能得到客户来源；绕过代理直连 API 伪造转发头不能改变身份。单元回归已覆盖 IPv4、IPv6、mapped IPv4、默认关闭、非可信伪造、多值/链/zone/端口及非法配置，不依赖生产环境。
