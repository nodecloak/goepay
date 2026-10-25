# 固定98本地集成验收

项目 `E:\GOEPAY`。源码快照 `.tools/checkpoints/98-settlement-batches-historical-schema-7db2c0f7-fa4e-48f7-9832-88d55f43e4de`，1394文件，manifest SHA256 `ea80c78c98ce842405cfbca5241b8cde3a080a89764279e74acf71cfbe880da3`。本文仅记录实际本地证据，其他业务插件依用户指示暂缓；未访问生产、导入真实密钥或发起真实资金交易。

**最终状态：2026-10-25 10:31:29（UTC+8）全部本地验收门槛完成，连续流程实际退出0，ACTUAL_ACCEPTANCE已记录。** 当前[管理后台](http://127.0.0.1:8185/admin/dashboard)、[商户后台](http://127.0.0.1:8185/merchant/dashboard)运行固定98；启动方式及本地合成账户位置见[本地运行说明](../local-operation-98.md)。这不是原Pro全部功能完全兼容或生产可替换声明。

## 完整固定源码验证

Windows 和 Linux 各自完整执行37包，各2094个通过事件、0失败，test/vet/build退出均0；Linux实际启用race，容器正常退出、源码只读、独立内部网络与全新数据库。Windows结束2026-10-25 10:10:26（UTC+8），Linux10:12:45。1394源码前后SHA一致。两平台均真实执行微信/支付宝原生JavaScript控制器→Go HTTP→随机PostgreSQL→本地TLS互操作，不借用父候选的后端结果。

每平台完整命令中的12项PHP opt-in跳过均在单独固定PHP容器、network none环境实际执行通过，覆盖原V1/V2商户SDK、通道签名/APP/付款码/银行卡OAEP/证书/小程序编码/支付宝订单码、RSA、原通知模板与微信映射，以及四种结算CSV逐字节对照。使用11个未改动原PHP参考文件；不运行原PHP服务，不连接真实数据库或机构。

固定前端fresh install后72文件322单元、typecheck/build、168项mock浏览器通过，0失败/0重试，9项实际部署opt-in单列。双npm audit为0。保留约602KB分块提示。Windows/Linux原生控制器各19项通过；这不包含厂商IDE或真机编译、发布。

Go源调用图扫描及模块校验退出0，未发现已导入包或可达符号命中。保留未引用OpenPGP模块的模块级GO-2026-5932通告，漏洞库日期2026-10-21；不作绝对无漏洞保证。[本地数据库基准](performance-98-local-2026-10-25.md)已实际完成18组观察；没有同条件PHP基线，不能据此承诺生产QPS或性能提升比例。

## 本次核心增量

原独立结算批次菜单已实现，不再映射普通审批：保留原批次号、净额、笔数、时间、状态、原受款快照及历史；创建批次不再次预留或出款，只有原可信出款证明才能完成。四种原CSV的列序、CRLF及编码逐字节核对：通用/微信UTF-8 BOM，支付宝/网商GBK。敏感导出权限、CSRF、不可变审计与完整性检查一并落实。详情、筛选、原出款核对、确认操作及375px布局沿用现有Pro后台样式与交互。详见[84专项](settlement-batches-84-2026-10-25.md)。

固定后台OpenAPI为3.1.0，实际解析173个路径、204个HTTP操作。角色权限、商户隔离、CSRF、金额分字符串和运行时产品能力按服务端契约执行；此数量不是原后台全部加密动作的覆盖率。主工作目录的1394个纳入快照的文件也逐一SHA核对一致，证据`.tools/current98-source-post-deploy-check.json`；后续运行报告和状态文档在该固定源码范围之外。

97完整回归曾发现实际76→77旧结构检查失败，原失败与取消证据保留，97未发布。98修复区分真实旧结构与新版本缺表/原审计或迁移标记存在的损坏情况；两平台完整回归均通过这些历史及损坏测试，原1～82迁移未修改。

## 实际部署及验收

实际98/schema83已完成本地96→98增量升级与metadata/compose切换，196张原业务表所有原列、行哈希及82条原迁移记录均不变。仅新增83迁移的三张批次表，合计200张public表，32项完整性计数全0；8个Linux二进制和3个实际静态资源SHA核对一致。API8184、后台8185、PostgreSQL55448均仅本机；只运行原拥有的api/web/postgres，所有真实外部能力和自动worker/调度保持关闭。

实际镜像：runtime `sha256:75ede6ca4f8bc84bf28301534ccfa93c7d0fd4c73a5336b70581b33d7ad90060`；web `sha256:8467113cd53d296e324a7d6fef69941722b15197bee038061d996992e3dbb2ff`。使用原已核实本地基础镜像，验证层祖先及非root运行，未拉取替换基础镜像。旧8180/8181未操作。

微信/支付宝两通道本地模拟均通过create、checkout、重复callback只入账一次、paid query及幂等本地退款。部署后General7、Funds1、Printing1实际浏览器均通过，共9项，0失败/0跳过/0重试，固定源码前后不变。密集登录后等待原15分钟窗口自然恢复，没有重置或绕过限流；随后额外验收全部通过：

- 商户通知设置：CSRF/本人隔离、密钥读取脱敏、假目标清除、登录事件保留、模式0无发送任务、375px无页面错误或外部请求。
- 支付审核与域名来源：pay2仍可登录，新签名支付请求被拒绝且无新订单；恢复pay1及原设置，删除自己的合成域名后原独立通知事件仍保留，模式0不排队。
- 结算批次：CSRF/商户越权拒绝、稳定请求幂等、原净额和收款快照、未付款完成409、原格式实际下载、375px/1280px交互；归档保留原申请历史，再拒绝自己的待审核合成申请释放预留，未准备或发送出款。

实际额外验收证据位于`.tools/live98-77a185006dd14a9087cb1ce7b60b309e`，含原始结果、批次持久意图及两种宽度截图。只读HTTP观察全部成功，计量288次、另12次预热，无资金操作；详细数值与小样本限制见性能记录。

完整自定义备份已实际导入全新GUID数据库，200张public表的全部行数/内容哈希、83条逐名迁移checksum完全相符；33订单、208凭证、416分录，32项完整性计数全0。原库在dump/restore前后内容不变，1394源码SHA不变，临时恢复数据库已清理。归档1,391,255字节，SHA256 `8ab3c3b9ae519c90b9f955416bffc61ab99834e96038ac9bcf72d19c188590fb`。证据及原归档位于`.tools/restore98-prep-5412d9f0eeea486bad1c61716736afa2/run-bd1f41e5a74e4a3f9e643885fa8407e8`。严格record核对实际镜像/完整回归/模拟/浏览器/额外验收/归档后写入`ACTUAL_ACCEPTANCE.json`及本地部署`RESTORE98_VERIFIED.json`。

第一轮连续流程在镜像准备前被部署工具遗留的97目录前缀校验阻断，未创建候选构建目录、未停机或迁移。原流程、日志及工具保留于`.tools/complete-local98`。精确修正98路径及停机凭证文件名，应用源码与测试证据未改变；另在`.tools/complete-local98-resume-after-path-fix`重新核对全部发布证据后连续执行后续步骤。原流程失败不改写为成功。

## 证据入口

- 完整后端：`.tools/full98-ac2241fbc11e4ad0a28555648aa36c19/{windows,linux}/FINAL_VERIFICATION.json`。
- 前端：`.tools/frozen98-frontend-a384083953274e6893792c6a71c084b7/summary.json`。
- 原PHP：`.tools/php98-evidence-path.txt`指向的原始执行记录。
- 原生控制器：完整后端目录下`NATIVE_VERIFIED.json`。
- 安全：`.tools/security98/VERIFIED.json`。
- 发布门槛：`.tools/release98-verification.json`及`.tools/release98-approval.json`。
- 连续部署：`.tools/complete-local98-resume-after-path-fix/RESULT.json`及每阶段原始日志。
- 实际验收：`.tools/live98-77a185006dd14a9087cb1ce7b60b309e/ACTUAL_ACCEPTANCE.json`。
- 完整恢复：`.tools/restore98-prep-5412d9f0eeea486bad1c61716736afa2/run-bd1f41e5a74e4a3f9e643885fa8407e8/RESULT.json`。
- 最终只读复核：实际镜像、18项布尔安全开关、当前/固定1394源码SHA、健康/就绪、归档SHA及临时恢复库确已不存在，见实际验收目录`ROOT_FINAL_VERIFIED.json`。

原Pro大部分后台控制器加密，原菜单或局部协议通过不等于全部动态错误、服务商/电商产品、隐藏政策、真机或真实机构权限已证明完全兼容。后台使用`/console/v1`新规范，商户支付兼容接口另行保留；不声称PHP后台RPC路径逐字兼容。未做真实历史全量导入、生产切换或真实资金联调。
