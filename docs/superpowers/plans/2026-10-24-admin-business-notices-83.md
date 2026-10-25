# 原管理员业务提醒83 Implementation Plan

> **For agentic workers:** 在当前会话连续执行，用户已授权本地实现/测试/部署；不调用当前不可用的子代理。

**Goal:** 四个原核心业务来源通过正确uid0开关和永久发送claim提供管理员提醒与只读记录。

**Architecture:** 独立businessnotice领域负责受保护事件/claim/结果，payment/enrollment/backoffice原事务提供可信来源；复用已验证的纯模板和运输器。PostgreSQL jobs，不新增Redis依赖。

**Tech Stack:** Go1.27.3、PostgreSQL、AES-GCM/HMAC、Vue、Vitest、Playwright。

**Spec:** `docs/design/admin-business-notices-83.md`

## 2026-10-25 专项执行状态

已实现选择器/严格设置、四个原事务来源、三张不可变表、AEAD/HMAC及原job验证、永久claim和结果、独立QQ账户绑定、默认关闭worker、只读HTTP/后台、中文原模式选项及三项CLI计数。源/并发/unknown/模式变化/晚ACK/工厂篡改/投影故障/历史父状态变化/真实HTTP/缺ACK/替换job/事件篡改/关闭后不补发已使用随机PG schema验证；第一次篡改测试的多语句参数夹具错误保留，拆分后通过，没有据此修改产品行为。

相关4包回归证据`.tools/settlement-auto73-business83-final-affected-regression.log`，后补篡改/关闭专项分别保留独立日志；复用运输协议2包完整测试4.936/14.252秒退出0。前端最新71文件318单元通过，手机/桌面新增2浏览器及全166模拟浏览器通过（另9个部署用例跳过）；类型检查和生产构建通过，保留589KB包体警告。实际375px截图已查看。

原商户admin_class默认operator造成通知设置403的问题，由额外94部署验收发现，真实PG/HTTP回归先失败后修复通过。94快照保持不变；该修复与81/82/83一起进入后续固定95验证。下面完整验收检查项在95全库/race/原PHP/本地升级及恢复实际结束之前保持待验，专项结果不能替代它们。

95固定候选在源码扫描的全包加载中发现CLI匿名嵌入两个同名IntegrityCheck造成编译失败；95长任务已明确取消，保留失败证据和原数据库，不能计为全库通过。新增CLI输出合同测试先复现编译失败，以不同嵌入名保留两个独立JSON命名空间后通过，实际断言31字段互不覆盖；所有cmd构建及全37包/测试二进制仅编译检查退出0（不把空选择器称全量测试）。后续完整验收转入修正的固定96，95原快照不可修改。

## Global Constraints

- E:\GOEPAY；不访问生产、实际支付/消息服务或真实资金；其他业务插件暂缓。
- 固定94与82阶段快照不可修改；1..81旧SQL内容保持，新增schema82。
- uid0模式0off/1SMTP/2Robot/4TG/5QQ，internal_transfer使用msgconfig_domain；不能沿用risk数值或邮件回退。
- 任意外呼前持久claim；unknown/缺ACK/改配置不重发；不以消息故障回滚资金事实。

### Task 1: 原模式与严格设置

**Files:** 创建 `internal/businessnotice/selection.go`、`selection_test.go`、`settings.go`；修改后台设置catalog。

**Interfaces:** `ModeFor(scene string, value string) (merchantnotice.Transport,error)`，value0返回空运输；`SettingKey(scene string) (string,error)`固定四场景映射。选项只接受规范0/1/2/4/5，缺失由读端显式默认0。

- [ ] RED：regaudit1必须SMTP、domain2机器人、apply4TG、5QQ、0无运输，internal_transfer与domain同key；risk同数值不参与；3/00/空/未知scene拒绝。
- [ ] 实现纯选择器/严格单MVCC设置读取，重复/类型错误失败；原mail_recv/mail_name显式空值不回退默认。
- [ ] GREEN：纯测试和真实随机schema设置读取，零凭据读取/零网络。

### Task 2: 四个可信新来源与不可变job

**Files:** 新增 `db/migrations/000082_admin_business_notices.up.sql`、`internal/businessnotice/store.go`、`sources.go`、`events.go` 与对应测试；新增payment业务事实桥，修改原注册/域名/申请事务。

**Interfaces:** `NewStore(pool,key,RequestFactsReader)`纯构造；`CaptureRegistration(ctx,tx,auditID)`、`CaptureDomain(ctx,tx,auditID)`、`CaptureRequest(ctx,tx,eventID)`只接原ID；`RequestFactsReader.BusinessNoticeRequestFacts(ctx,tx,requestID)`返回验证过的原kind/merchant/recipient/gross/net/type/hash。

- [ ] RED：真实注册pay2/域名新提交/商户手动提现及站内转账各一源；pay1、自动/管理员来源、旧事务调用均无新job。
- [ ] 三表+原可空元数据；按用途派生AEAD/HMAC，固定原job ID，原事务源重查和独立审计。
- [ ] 在原领域事务加SAVEPOINT桥，注册在证明消费后发布；后续正常审核/支付/域名改名删除不更改原输入或重复源。
- [ ] GREEN：重放/跨人/源篡改/缺源/可选投影故障、旧列及旧行保留，真实PG测试。

### Task 3: 永久claim、结果和默认关闭worker

**Files:** 新增 `internal/businessnotice/delivery.go`、`work.go`、测试、`cmd/worker/business_notice_runtime.go`；修改原worker/queue/compose/.env.example。

**Interfaces:** `Handle(ctx,job,Resolver)`验证原job/租约；Resolver纯构造返回受保护Account与merchantnotice.Sender。每event一个claim与独立result，保存unknown不得提供重发路径。

- [ ] RED：8并发只一次；模式改变永久not_sent，claimed未知/失ACK/新任务ID/配置变化零二次发送；晚ACK可保存；工厂不能改封存模板。
- [ ] 首外呼前提交claim+审计+scope/recipient/source proof；结果有限WithoutCancel；Transport/QQAppID独立验证。
- [ ] Runtime只在独立true开关加入job kind，其余不读凭据；复用严格SMTP/Webhook/TG/QQ适配器，无隐式回退或消息重试。
- [ ] GREEN：域测试、真实本地TLS/SMTP协议及worker队列选择；部署默认关闭。

### Task 4: 原设置交互、只读历史与完整性

**Files:** 创建管理员业务通知HTTP/前端组件/解析与单元/browser测试、OpenAPI模块、CLI完整性检查。

**Interfaces:** GET `/console/v1/admin/business-notices`，before_id规范非负字符串、limit1..100；无发送/重试入口。

- [ ] RED：商户/越权/坏分页拒绝，列表没有目标/正文/凭据；原设置中文选项正确，375/1280可用。
- [ ] 实现只读来源/job/claim/result验证和三项CLI计数，历史读不依赖当前可变父状态，不自动修复。
- [ ] GREEN：全前端、相关包、原14模板互操作；固定最终候选两平台全库/race/源扫描、无重置升级、实际本地浏览器与全表恢复。94和82阶段结果只证明其各自快照。
