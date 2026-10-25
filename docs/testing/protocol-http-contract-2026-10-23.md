# 协议、HTTP 安全与 PostgreSQL 契约验证记录

日期：2026-10-23。仅记录下列已运行测试，不代表原 PHP 系统全部功能已完成。

## 运行结果

在本机 PostgreSQL Docker（127.0.0.1:55432）上运行；每个契约用例独立随机 schema，结束删除该 schema。原 SDK 互操作使用 Docker php:8.2-cli，网络禁用，不请求真实支付系统。

执行（DSN 从本地环境配置，不写入本文）：

```powershell
$env:GOEPAY_TEST_DATABASE_URL='<本地隔离测试数据库 DSN>'
$env:GOEPAY_PHP_INTEROP='1'
& D:/GoLang/sdk/go1.21.3/bin/go.exe test -count=1 ./internal/compat ./internal/platform/httpclient ./internal/server ./tests/contract
& D:/GoLang/sdk/go1.21.3/bin/go.exe vet ./internal/compat ./internal/platform/httpclient ./internal/server ./tests/contract
```

Go 命令根据 go.mod 自动选择 Go 1.24。最终执行四包全部通过，go vet 无输出、退出码 0。签名与 HTTP 策略的首批用例先观察失败，再完成实现；JSON 尾随垃圾、SSRF callback、PHP 布尔签名及分页回归均先重现失败后修复。

## 验证范围

- 表单：保留原金额字符串、中文/emoji/0/前后空格，拒绝重复键和 PHP 名称归一化/数组歧义。
- 签名：MD5 独立固定向量；SHA256WithRSA PKCS#1 v1.5；错误密钥/金额篡改/商户 RSA 降级拒绝；时间戳前后 300 秒边界、超大输入拒绝。
- 原 SDK：直接从项目原 SDK.zip 和 SDK_2.0.zip 提取类，在 PHP 8.2 执行私有签名方法并验证 Go 签名；V1/V2 各三组，覆盖中文/emoji/0/空值/空白/扩展字段/金额格式/URL。RSA 双方使用本轮随机生成的 2048 位密钥，签名结果一致；未使用虚假 PHP 模拟器。
- HTTP 出站：私有/保留/映射 IPv6/IPv4 地址拒绝、DNS 混合结果拒绝、DNS 解析后固定 IP 拨号、重定向拒绝、代理禁用、总超时。
- 旧支付 HTTP：MD5 下单/查单、相同幂等号重试相同订单、变更金额冲突、V1 材料字段约束。
- V2 HTTP：RSA 下单与成功响应验签、RSA 商户拒绝 MD5 降级、PHP 布尔和空白输出签名规则。
- 双后台：真实 PostgreSQL 用户登录、HttpOnly/SameSite cookie、CSRF 拒绝与有效退出、跨角色 cookie 拒绝。
- 列表：订单两商户各 25 笔，真实总数/第二页/limit、大小写无关搜索、跨商户搜索不可见、SQL 字符串按字面值处理；退款、转账、账本分页与商户隔离；管理员商户名称搜索。
- 就绪检查：orders/console_users 存在时正常；缺 console_users 返回 503，不仅判断数据库 TCP 连接。

## 原 SDK 证据

- V1 SDK.zip/EpayCore.class.php SHA256：`4f675d7bde66565aa2d372c5cee41c3dd7dcd12a636fa746b36aa06497bdcb77`
- V2 SDK_2.0.zip/EpayCore.class.php SHA256：`161fffe3c68998f539bf4253d4cd274e69aaf1f4b1df30bfe8578eac4d1c94b5`
- 本轮 php:8.2-cli 镜像 digest：`sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50`

## 限制

Pro Payment.php 与 SDK 对纯空白的判空规则确有源码差异；服务端请求使用严格空值 profile，面向 SDK 的响应使用 trim 空值 profile。未对加密控制器取得黑盒样本。RSA 小于 2048 位和畸形歧义请求被主动拒绝，这是安全边界变化。上游退款、代付、进件、投诉、分账和全部页面按钮尚未因为这些测试而完成验收；不可把沙箱通过当成原支付通道全兼容。

## 追加回归：账号撤销与限流

后台会话绑定精确登录用户名、角色和主体 ID，每次请求重新查询账号 enabled，商户角色同时校验 merchants.enabled。分别禁用 console_users 和 merchants 的 PostgreSQL 回归先重现会话仍返回 200，修复后返回 401 并移除会话。

登录限流按直连来源 IP（不信任 X-Forwarded-For）、角色、用户名散列分组，源端口不构成新身份；同一反向代理后的不同账号独立计数。每组 15 分钟最多 10 次，存储过期清理且限制最多 10000 组。测试验证账号/角色分组、源端口不能绕过、过期恢复和硬容量限制。
