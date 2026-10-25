# 固定打印网关连接器开发验证

2026-10-24，E:\GOEPAY。此增量没有新SQL迁移；当前部署仍为schema62，外部设备执行未启用。

新增固定Excelsecu HTTPS客户端，复用原PJ/HMAC协议、配置指纹、原打印来源、发送前claim及审计。固定目的地为本地明文Printer.php中的网关；不接受任意URL、环境代理、cookie或重定向。TLS验证开启、最低1.2，10秒总超时，响应最多4096字节；清除GetBody，不授权HTTP内部重放。异常只返回固定未知结果，不泄露设备、AppID、secret或响应。受理不等于实体打印完成。

独立外部worker默认关闭，与本地fake互斥，sandbox禁止外部执行。未启用在连接DB前退出。平台`external_execution_available`仅表示已安装固定连接器，不是worker健康或已启用。Compose新增独立profile，本轮未连接真实设备。错误provider的worker不会消费原queue或send claim；配置已变化的原意图仍被拒绝，不重新构造发送。

## 证据

- 真实本地TLS：固定URL、PJ、原HMAC、POST一次；307/303不跟随；503、坏JSON、重复code、超大响应、断开、取消均不重发。未受信TLS证书导致握手失败，应用handler收到0次。`.tools/printing-transport-client-final.log`通过1.214秒。
- PostgreSQL + TLS：首次POST前原claim与审计已提交；8并发只1次发送；未知和结果审计故障后不重发；claim审计故障0发送；资金不变。错误worker的queued intent及pending queue均保留。真实RED与GREEN日志均保留。
- 原第一份864文件快照漏覆盖已更新的`dispatch_test.go`，运行暴露旧断言不符。该快照及失败日志保留，未修改它或把失败计为通过。修正快照闭集后仍864文件：`.tools/p66-16dfd38e/.tools/checkpoints/66-printing-proof-314ba666-5ce6-430b-90a3-79a113c5ff2d`。
- 修正固定快照完整printing包通过124.128秒，printing-worker通过0.906秒，日志`printing-windows.log`。Linux race另有独立日志，须以最终退出结果为准，不能用启动成功替代通过。
- 前端开发树122单元与97 mock通过，9 live跳过；最终合成源码还需重跑。历史导入与应用授权后续修正未加入本printing专项快照。
- 本地镜像构建在无输出等待期间中止了本次`goepay-printing-proof-66`构建客户端，未作为成功镜像使用；未重启或删除其它项目容器。之后的实际镜像与部署以最终集成记录为准。

所有连接测试均使用httptest合成TLS和本地随机schema；没有真实打印、支付、退款、转账或生产访问。
