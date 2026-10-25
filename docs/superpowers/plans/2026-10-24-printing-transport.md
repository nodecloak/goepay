# 固定网关打印连接器 Implementation Plan

> **For agentic workers:** 按测试先行逐项实现。用户已授权连续本地开发和测试，由根任务直接执行，不另行请求阶段确认。

**Goal:** 将已存在的Excelsecu协议与可信打印意图接上有界HTTP客户端和默认关闭的独立worker。

**Architecture:** 复用62的来源、AEAD配置、独立PJ号、发送前claim、终态HMAC及恢复规则。新增客户端固定HTTPS官方地址，不接受控制台URL；发送一次，未知只保留原意图。

**Tech Stack:** Go net/http、现有PostgreSQL队列与printing包、Vue设置页。

**Spec:** 本地 `epay_pro/app/lib/Printer.php` 的固定网关、路径与HMAC协议；`internal/printing/protocol.go` 和现有62打印设计/验证记录。自动出款仍按独立授权设计推进，与本连接器互不授予资金能力。

## Global Constraints

- E:\GOEPAY；禁止生产访问、真实设备调用、真实凭据与真实资金操作。网络测试只使用合成凭据和本地httptest TLS。
- 64完整候选已固定853文件；新改动只在工作源码，不改候选或1..64迁移。
- 默认关闭；本地模拟与外部worker开关必须互斥；sandbox不能启动外部worker。构造、配置、读取状态均0网络调用。
- 外部只允许 `https://saas.excelsecu.com/openapi/v1/iot/printer/print`，无任意地址、重定向、cookie、环境代理或自动重试。
- 响应最多4096字节，超时不超过10秒，错误不含请求/响应内容、设备号、AppID或secret。accepted仍只表示网关受理。

## Task 1：固定HTTPS客户端

Files: 新 `internal/printing/excelsecu.go`、`excelsecu_test.go`；原protocol只更新已经不准确的注释。

- [ ] 写真实TLS测试，断言原固定host/path、JSON/HMAC/PJ号、copies/voice及收到请求次数，accepted不是实体完成；先运行确认缺客户端的RED。
- [ ] 实现 `NewExcelsecuConnector() *ExcelsecuConnector`，使用包内可注入测试client、固定URL、10秒超时、TLS校验、禁止redirect/代理、只POST一次。
- [ ] 验证redirect、非200、坏JSON、重复code、超大响应、断连接、取消、敏感回显均返回固定未知错误，0自动重试；保留原RED/GREEN日志。

## Task 2：来源匹配与worker边界

Files: 修改 `internal/printing/jobs.go`、`runner.go`、`cmd/printing-worker/main.go`；新增测试在上述对应test文件。

- [ ] 写真实PG测试：excelsecu原意图匹配可信连接器、发送前claim与audit已提交、并发仅1POST、未知重领不再发送、错误provider和local fake不能相互代用。
- [ ] Dispatch仅对明确匹配provider的客户端开放；不得只放宽LocalOnly布尔。保持原未知/已claim分支不重发。
- [ ] 新 `RunExcelsecuOne(ctx, enabled)` 使用固定连接器，main严格解析GOEPAY_PRINTING_EXTERNAL_ENABLED；两个模式同时开启或sandbox外部执行均在连接DB前拒绝。
- [ ] 原本地RunOne入口和取消、租约、dead恢复测试继续通过；不改原打印资金来源或claim算法。

## Task 3：真实能力与交付验证

Files: settings view/OpenAPI/运行文档按实际能力更新；需要时新增独立Compose外部profile，默认不启动且本轮不启用。

- [ ] 设置页区分本地假设备、固定网关连接器已实现与独立worker默认关闭，不宣称worker健康或纸张完成。
- [ ] 执行完整printing包、worker/server契约筛选及vet/build；本地TLS/PG计数证明无付款/退款/转账外呼。
- [ ] 保存源码SHA和失败事实；64候选回归不受本轮改动影响。新功能单独固定候选后补Linux race及实际Docker默认关闭验证。
