# 本地订单打印核心：schema62 验证记录

日期：2026-10-23。源码工作区 `E:\GOEPAY`。本记录覆盖打印核心、可信支付事实钩子、本地协议和默认关闭的本地执行器；HTTP、Vue、菜单、OpenAPI、CLI、restore 接线和全库/race 验证由父任务独立汇总。

## 固定源码与验证结果

证据目录：`.tools/checkpoints/62-printing-core-evidence-513dae82-291e-4654-9d58-151824dfb48b`。`source/` 保存本次21个文件，包含 schema62、printing 包、独立审查测试、本地 worker、支付来源文件及出生/入账钩子。`BEFORE_SOURCE_SHA256.json` 与 `AFTER_SOURCE_SHA256.json` 的21项全部一致。该目录是打印范围的证据快照，并非独立可构建的全库候选。

- `go test -p 1 ./internal/printing ./cmd/printing-worker -count=1 -v`：真实退出0，打印包20个测试函数全部通过，93.785秒；worker测试0.831秒。日志 `full-printing-fresh.log`。
- `go vet -p 1 ./internal/payment ./internal/printing ./cmd/printing-worker`：真实退出0，日志 `vet.log`。
- 专用测试环境变量缺省、仅设置普通 `GOEPAY_TEST_DATABASE_URL` 时，真实 PostgreSQL 来源回归通过，3.927秒；未被跳过。日志 `standard-env-fallback.log`。
- 每个数据库测试使用随机独立 schema 和正式 `db.Migrate`，包括迁移校验记录。独立本地容器 `goepay-test-printing`，PostgreSQL17，端口55437；仅合成数据和合成凭据。没有修改 PostgreSQL 锁配额。

以上是专项证明，不替代固定完整候选的全库、Linux race 或部署验收。

## 实现及边界

- 平台与商户配置为追加版本、CAS当前指针、AEAD加密及HMAC审计证据；普通读取只返回凭据存在标志和设备/应用标识脱敏值。当前头必须与最新原始版本一致，损坏恢复中的旧签名回指也不能重新变成有效配置。
- `CreateCollectionOrder` 在订单出生事务内封存真实收款码来源；普通订单、充值、聚合来源不能通过重放互换，不能从 `out_trade_no` 前缀或商品名猜测来源。
- 仅本次验签成功的支付入口封存可信已支付事实。普通 `MarkPaid` 或裸数据库 paid 状态不能创建打印权限。打印事实与 prepare outbox 共同封入支付事务的子保存点；任何打印专用写入失败时整体撤销可选打印投影，支付的收据、账本及原支付 outbox 仍提交。之后重新验证该旧支付只可建立历史重确认事实，不能补自动打印。
- 支付时冻结打印配置标识和模式；可选配置异常冻结为不可用状态。后续启用配置不会倒灌到旧订单。自动打印仅处理新鲜可信普通支付/聚合订单，充值自动打印语义未确证，本期明确抑制。
- 小票来自商户本人可信已支付订单，显示原支付毛额，不带支付凭据或通道买家身份。所有可变文本进行长度与控制字符检查，并作 XML 转义。
- 自动意图按订单唯一；手工意图按商户及 external_no 幂等。每个意图固定一个 `PJ` 出站号。手工意图先存在时抑制迟到自动意图；排队或阻塞的原意图存在时拒绝创建另一张手工意图。
- 只有持久化唯一 claim 及同事务审计后才可调用 connector。受理仅为 `accepted`，没有物理打印完成状态。超时或丢失响应保持 `unknown`；重放原意图绝不重发。核心支持显式关联原 unknown 的风险确认新意图；当前 UI 由父任务选择仅开放原号恢复。
- 耗尽且从未 claim 的 send 任务显示 `blocked/task_dead`。`Recover(ctx,mid,id,expectedTaskID,actor)` 使用旧 task ID 唯一不可变恢复记录，同一请求丢失响应后重放返回原 replacement，不再次消耗恢复额度、不产生新打印意图。每个意图最多5次显式恢复；旧 dead 任务保留。缺失任务不开放恢复。prepare任务耗尽不会被自动清零恢复预算；该情况没有完整自动恢复页面，不宣称已自动处理成功。
- `RunOne` 严格解析单字段、规范正整数字符串和大小上限，拒绝重复JSON键；任务期限20秒，队列租约60秒，结果及队列收尾各有独立5秒预算。取消发送后原 claim/result 仍保留未知事实。
- 本地 worker 默认关闭，只有显式 `GOEPAY_LOCAL_PRINTING_FAKE_ENABLED=true` 且 `GOEPAY_SANDBOX=true` 才启动。默认关闭在连接数据库和读取密钥前退出。只提供 `LocalFake`；`excelsecu` 的外部执行能力始终为 false，执行时明确拒绝且零网络调用。
- 已知启易付请求格式仅实现纯 codec；用真实 `httptest` 本地 TLS 服务独立核对原 HMAC、时间戳、biz、份数及编码。没有提供外网 URL 配置、真实外部网关客户端或生产试打动作。

## 完整性检查

`printing.Store.Check(ctx) (int64,error)` 在一个 RepeatableRead 只读快照内，每次最多100条扫描原始父表和子表：订单来源、支付打印事实、平台/商户历史配置与最新头、打印意图、claim、result、恢复链和打印队列。验证HMAC、同主体审计、原订单身份与金额、source→prepare任务、intent→send任务、恢复链原dead任务和固定payload/event key。缺失投影不能因 inner join 消失；缺失send/prepare任务也报告异常。

返回值是坏记录计数，同一损坏链可能涉及多个父表，不是不同坏订单的去重数量。打印故障后合法不存在的可选事实不会从 paid 订单反推为损坏，更不会补造成功或自动任务。

## 红绿与故障证据

- 新协议、runner及严格payload接口先有编译RED，随后实际本地TLS/PG绿色；保留 `62-printing-protocol-red.log`、`62-printing-runner-red.log`、`62-printing-runner-safety-red.log`。
- 独立审查复现两种重复打印竞争，各产生两意图、两次本地发送；修复后原断言不变。原RED由独立审查快照 `.tools/checkpoints/62-printing-review-1c9edc57-34a2-47d2-b174-f69039c82b0f` 保存。
- 同一独立审查注入打印事实表或 prepare job 写入故障，原实现回滚验签支付的实际RED7.615秒；保存点修复后原支付账本、可信收据和支付 outbox 均正常提交，打印事实与任务均不残留，重新验证也不补自动打印。
- 配置最新头损坏恢复旧指针：真实RED5.52秒，增加读时最新头验证后GREEN8.031秒；正常数据库头CAS guard的拒绝断言一直保留。
- 并发8次恢复同旧任务只产生一个replacement；5次预算耗尽不能绕过，旧请求仍幂等返回原replacement。
- claim审计失败零发送；result审计失败已有claim继续保持发送中/未知且不重发；配置作用域变化与外部provider分支均零发送。
- 完整性回归11个场景：9类证据损坏、缺send任务、缺prepare任务。损坏恢复测试仅在独立随机schema临时降级对应打印家族的不可变触发器，恢复触发器后再运行公开读/Check；未禁用生产约束或支付账本约束。

未连接生产、读取真实支付/打印凭据、调用外部打印服务或发起真实资金交易。
