# 固定68运行代码的独立原生联调与恢复验证

2026-10-24，E:\GOEPAY。本记录只覆盖固定68运行代码的本地沙箱验收。8180/8181原Docker环境仍为schema62；Docker/WSL新网络命名空间阻塞未通过重启共享环境处理。本次使用独立原生API、前端代理和新建GUID数据库，不能称Docker新版本部署已通过。

## 运行边界与候选

后端与前端构建来自885文件固定68候选`.tools/checkpoints/68-app-auth-integrated-4e75afb2-0418-49ca-88f0-086045288444`，数据库迁移66。API为127.0.0.1:8182，静态前端127.0.0.1:5179，工具目录`.tools/native68`。API/CLI二进制、运行元数据和监听进程证明独立保存。

数据库仅`goepay_native68_08f1a396c78cf71672d9d9f007705f63`，位于本地55432；使用新生成的合成账号、固定测试密钥和simulation+sandbox。没有加载导入生产通道或真实凭证，没有外部worker或真实资金操作。原生静态代理只复现现有nginx路由，不属于产品源码变更。

浏览器70候选`.tools/live70-cdca795b/.tools/checkpoints/70-live-fixtures-068c0282-42b3-4dc9-9ed5-b256dfda4136`继承69，只增加外置合成fixture与loopback地址的测试加载能力，产品运行源码、依赖和构建与68一致；PARENT_COMPARISON封闭记录9个测试文件差异。外置fixture不进入源码候选。测试名称中的Docker字样是既有名称，本次实际运行环境明确为原生回退。

## 浏览器结果与失败事实

- 第一次命令因Windows npm转发正则参数产生NoTestsFound，保留`native-live-command-filter-failure.log`；改为直接调用Playwright并明确列出文件。
- 第一轮7项：5通过、2失败。新建商户余额为0，结算与转账正确拒绝资金不足；没有修改资金校验。保留`native-live-zero-balance-fixture-failure.log`和失败trace。
- 使用既有签名`/mapi.php`创建唯一原始10.00元sandbox订单，再经受保护的sandbox-paid接口模拟到账；没有直接写账户余额。稳定订单号允许工具幂等，证明见`.tools/native68/browser-balance-fixture.json`。
- 重新执行7项：**7通过，约1分钟**。包含80个已实现菜单叶子只读巡检、双后台登录/列表/退出、领域CRUD、结算拒绝后恢复、钱包转账、RBAC和公开收款页停用撤销。日志`70候选/native-live-first-seven.log`。
- 另执行资金确认与模板打印2项：**2通过，6.4秒**。明确设置API为8182，原始net只模拟支付一次，资金密码/测试政策按原测试恢复；模板受保护iframe与打印设置维持本地模式。日志`70候选/native-live-funds-template.log`。
- 登录限流未放宽。批次间工具仅将这个新建专属库的两个合成账号、来源127.0.0.1的计数窗口移到16分钟前；保留审计与全局来源预算，不修改产品限制或共享数据库。工具显式`-Apply`且强校验exact GUID库和sandbox环境。

## 实际备份恢复

从EDB官方HTTPS下载Windows PostgreSQL18.6 portable包，只提取客户端与所需DLL；来源、归档SHA与提取文件SHA留存在`.tools/postgresql-18.6-download.json`及客户端provenance中。没有安装服务或修改系统PATH。

`.tools/native68/restore-test.ps1`对上述exact沙箱库的public schema执行custom-format pg_dump，再恢复到本次新建`goepay_restore68_04a9b636786a47b98da6bdebca6e4879`。校验迁移66、迁移checksum、journal平衡、账户累计、原CLI全部20项HMAC/资金对账；**20项均0**，journal/account异常均0。恢复库包含**2笔订单、13笔journal**，不是空库验证。

恢复完成后只删除本次新建GUID恢复库，保留运行沙箱与dump。dump SHA256为`0842262d5af595053174659bed039b979d35bb44dc4ef7fc0a4f4502940c08ac`。RESULT、dump/restore日志、迁移日志与实际CLI JSON均在`.tools/native68/goepay_restore68_04a9b636786a47b98da6bdebca6e4879`。

本次沙箱没有导入旧退款正数据，故旧历史HMAC字段为0不能单独证明含历史正数据的恢复；独立历史正数据演练另记。完整Windows及Linux race仍以固定68最终退出结果为准。之后新增的微信URL Link运行代码不在68内，需要独立集成验收。

## 有界只读性能观察

`readonly-benchmark.mjs`使用同一合成原始管理员会话，仅访问loopback订单首页和dashboard。每个端点分别200次、并发1/8，另有预热，800次正式请求全部成功。此次订单接口并发8约1045次/秒、p95约10.20ms，dashboard并发8约998次/秒、p95约10.98ms；单并发两端点p95约16ms。原始结果、Node版本、CPU数和完整限制保存在`.tools/native68/readonly-benchmark.json`。

这只是2笔订单的小数据本地HTTP观察，共享机器仍运行完整回归；未计TLS/反向代理/远程网络，未比较PHP，也不是生产容量或性能提升承诺。无需为此观察调整连接池、降低校验或禁用审计。
