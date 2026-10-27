# 商户密码登录验证码与独立文档策略

Pro 的商户密码入口由 captcha_open_login 控制；管理员可读页面使用本地滑块，不能当服务器身份凭证，也不借用该商户开关。密钥入口保持既有独立权限。邮件/手机验证码登录及支付风控 __defend 是不同消费链，本阶段不把它们宣称已完成。支付模板固定demo验证服务不能作为生产防护。

GET /console/v1/public/login-captcha?role=merchant&mode=password 仅返回 enabled 布尔。POST 同路径/init 严格接收 username，签发独立 HttpOnly/SameSite browser cookie 与封存上下文；不创建或借用TestPay意图。117 独立绑定账号、role、mode、purpose、配置指纹、协议和原期限。V3/V4使用203固定官方TLS验证器和独立密钥库，无明文旧key、弱离线或demo成功回退。

商户原密码登录在密码/MFA尝试前 Consume 一次当前证明；错误密码也不能复用 proof。跨用途 native proof 账本按原生ID去重，摘要不包含用途或wrapper token；同时核对原203旧已用证明账本，保留其原字节。不能用同一V4 lot换新wrapper穿过testpay/login目的隔离。

IssueSession 在同一事务持设置表 SHARE（包含原本缺失的开关）、凭据共享 advisory、当前商户/用户锁，核对原 grant 配置、权限和原期限，再原子创建持久会话及记录消费。最后写入后再次按数据库时钟校验原期限；不会延长grant。设置读取使用普通MVCC；持表 SHARE 后再持设置行锁会与先锁行的管理目录 UPDATE/DELETE 形成40P01锁环，已用真实PostgreSQL回归复现并据此修正。远端验证有明确超时，最终会话事务没有外部IO。

前端仅 merchant/password 消费证明，字段以原版本 flat proof 传原/login；账号、模式、客户端及离页变化清除等待中的结果。传输结果未知时禁止新证明/凭据写入，只读/session核对。原admin/key分支与key未知结果恢复仍分别保留。

独立精确 /merchant/login 文档提供有限的极验 HTTPS 来源；其它商户页仍保持原脚本策略。未登录后台用安全 return 规范到登录页，已有会话或完成登录后离开该文档；主路由跨文档整页 replace，避免SPA继续沿用旧CSP。return只接受已知同源商户路径及普通query/hash，拒绝外源、递归、重复参数及bearer字段；邀请注册内存预填不搬到URL或storage，正在注册/找回时不强制切换。

验证区分实际Go/HTTP/PG/privateTLS与受控浏览器SDK。浏览器SDK方法由本地控制体实现，不冒称在线官方SDK或生产通道验收。CSP采用原始 request_uri，避免try_files转到index.html后失去页面策略。验证码来源依据 [V4部署](https://docs.geetest.com/gt4/deploy/client/web)、[官方域名说明](https://docs.geetest.com/gt4/bypass) 及 [V3参数](https://docs.geetest.com/sensebot/apirefer/api/web)，不采用其异常默认成功示例。完整结果以固定源测试收据为准。
