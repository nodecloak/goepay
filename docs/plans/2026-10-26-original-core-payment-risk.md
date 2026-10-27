# Original Core Payment Risk / Check Implementation Plan

> For agentic workers: execute the following isolated tasks using the existing local TDD and verification workflow; no delegation, commits or deployment are authorized. User-directed continuous implementation overrides any optional execution-choice prompt.

**Goal:** 真正消费原核心风控字段，保护新付款准入并提供可执行的Check检测器，保留已受理订单恢复和原资金/证明。

**Architecture:** payment Store拥有策略CAS/私有不可变来源/准入锁与检测处置，纯riskpolicy负责原字段规范化和离线地区读取；server只从真实request/既有受信代理与原身份绑定构造来源。现有provider first-attempt事务实际消费准入，旧attempt重放先返回；原回调/查询不走新准入。Check实际入口由独立本地执行器和最终171 scheduler直接调用，不用cron资料或noop替代。

**Tech Stack:** Go1.27.3/pgx/PostgreSQL17，Vue3/TypeScript，Vitest/Playwright；只离线依赖、合成PG/本地HTTP/TLS，无真实上游/资金。

**Spec:** 本地`.tools/riskcheck177-source-contract.md`与两个source-evidence JSON的原SHA/行/JS偏移；发布只保留证据摘要，不复制原Pro/Release专有源码。

## Global Constraints

- 父冻结1751935文件，manifest12d5d5ab670e618ab8226a511f413d157ecf83fe7ef4b76317d56adf1d7398a6逐字节校验复制；独立work-riskcheck177，171 v2原字节不动。
- migration102保留本任务；不得改98..101或其它冻结/主目录/候选。PG仅本任务新建127.0.0.1:64967，标签riskcheck177/synthetic_only。
- Pro可读buyer计数/金额是当日status>0 count>=limit、sum(realmoney)>=limit才拒下一笔；不发明sum+本笔硬cap，不用商户净额替代实际支付额。
- 原IP白名单完整地址匹配，不能沿用字符串包含漏洞；可信代理仅显式部署CIDR+覆盖单个X-Real-IP。既有mapi声明IP与传输peer分别保留来源。
- 规则默认关闭；未知加密逻辑明确界限，不把Check核心归入用户暂缓插件。投诉/进件分支才是明确暂缓外部插件。
- 原order/attempt/资金幂等、回调、查询、unknown恢复保留；新政策不重价历史。旧成功数据仅使用可核实来源，不把丢失事实回退成未配置。

## Task 1 — Source contracts / real policy HTTP RED

Files: new internal/server/riskcheck177_policy_test.go; new internal/payment/core_risk_policy.go and internal/riskpolicy/policy.go; new db/migrations/000102_core_payment_risk.up.sql; new internal/server/core_payment_risk.go.

Interfaces:
```go
type CoreRiskSettings map[string]string
type CoreRiskView struct { Configured bool; Revision int64; Fingerprint string; Settings CoreRiskSettings }
func (s *Store) CoreRiskPolicy(context.Context, MerchantCredentialActor) (CoreRiskView,error)
func (s *Store) UpdateCoreRiskPolicy(context.Context, MerchantCredentialActor, CoreRiskSettings,string) (CoreRiskView,error)
```

- [ ] 真实持久super会话GET `/console/v1/admin/payment-risk`期望无政策/修订0/原空字段/全关闭/no-store；先观察父175实际404。PATCH原精确settings+expected_fingerprint后期望修订1，旧fingerprint409；角色/代登录/CSRF/重复字段/未知/null/溢出严格负例。
- [ ] 纯字段规范化：日次数为整数，秒/天为整数，金额十进制元→整数分；空/0关闭，不接受浮点JSON或隐式负数，白名单规范IPv4/IPv6完整地址、去重，地区有限UTF8词列表。
- [ ] 真实事务锁、MAC与独立已提交审计修订计数/原会话最后写后DB时钟；缺表/行或完整旧合法政策回退拒绝。generic settings不能绕过真实政策。
- [ ] GREEN后再开始准入TDD；配置页面不能先宣称业务已消费。

## Task 2 — Immutable subject / successful-payment facts

Files: new internal/payment/core_risk_sources.go, core_risk_success.go, riskcheck177_sources_test.go; minimal changes store/paid_receipt and provider-attempt entry points.

Interfaces:
```go
type CoreRiskRequest struct { PeerIP, BuyerIP, Source string }
func WithCoreRiskRequest(context.Context, CoreRiskRequest) context.Context
func (s *Store) admitCoreRiskTx(context.Context,pgx.Tx,Order,int64,string,string,string) error
```

- [ ] 先写真实PG负例：已确认原付款/真实buyer+IP日界，旧未验证记录不伪作成功；birth/准入/success双事实丢失及旧head回退failclosed，无secret/租约/原buyer明文返DTO。
- [ ] 在新订单/第一次真实attempt事务冻结可信peer/独立声明buyerIP与原payer身份承诺；同订单重放不改来源、不二次记数。新来源与已提交原审计互证。
- [ ] 原确认付款同事务追加真实已验证金额/身份/成功时间，回调不因规则关闭或开启被阻断。旧可核实成功来源采用明确版本与保留来源锚点，不凭当前资料补造历史。

## Task 3 — New-payment admission / concurrency

Files: new core_risk_admission.go/riskcheck177_admission_test.go; minimal provider_attempt/provider_identity/provider_flow/scan/preauth/mini/recharge/collection entry-point edits.

- [ ] 每IP日笔数与间隔先写真实PG/HTTP RED，再连接真实first-attempt事务；同IP多订单并发、同订单重复、白名单精确匹配、IPv4映射IPv6、policy关闭交叉、等待跨日/间隔边界。
- [ ] 买家每日count与realmoney累计阈值保留原“已达才拒下一笔”；有可信身份的支付宝/微信流程实际消费，未知buyer场景不冒称覆盖。并发占位加固明确单列，不重写原资金预算。
- [ ] 配置生效只限制新付款；已存在attempt/unknown原重放先返回，所有query/callback和原授权unknown恢复positive。最后所有等待/写后复核DBclock、当前policy revision和原期限。
- [ ] 本地fake/TLS provider验证被拒绝0Create、许可1Create；规则变化/重放不能再Create；金额、历史费用/原MAC不变。

## Task 4 — Offline geographic rules / trusted address UX

Files: new internal/riskpolicy/region.go, region_test.go; runtime loader and server risk policy capability projection.

- [ ] 合成IPv4/IPv6 xdb真实读取正/负例，无外网；不下载原IP数据库，不读取原runtime/file/cert。country/province/city完整值/substring原契约分别注明。
- [ ] region/oversea开关真正消费本地结果；缺来源清楚拒绝启用/不能假装有效，未知原加密行为注明。原扫码IP地区一致性为独立0/1/2语义，不混成全局禁付。
- [ ] untrusted XFF/CF/X-Real-IP负例、可信CIDR正例、无效/重复头拒绝；原signed mapi IP独立检查，不能覆盖传输peer。

## Task 5 — Real Check detection / durable actions

Files: new core_risk_check.go, core_risk_actions.go, riskcheck177_check_test.go, cmd/risk-checker; original records projection and protected worker notice consumption when configured.

Interface:
```go
func (s *Store) RunCoreRiskCheck(context.Context,int) (CoreRiskCheckResult,error)
```

- [ ] 按可证明的Release来源先actual RED：IP时间窗总数>=N且0成功封5天；买家成功速度>=N封days（0永久）；通道最新N笔0成功停该原路由；商户round(success*100/total,2)低于阈值且总数>=N关闭新pay。投诉数据不伪造。
- [ ] 政策修订/观察原源/DB时钟、原状态、有限边界与同周期幂等、永久/较长期限不缩短、规则关闭不采用旧处置；恢复读/回调不被误封。
- [ ] 源/动作/独立审计/private head持久化与MAC防回退；原处置列表/风险记录真实显示；可选通知必须由真实受控原worker消费，不留假job。默认不运行、不自动发信。
- [ ] 单独CLI默认disabled不连DB/读凭据，合成-once真实执行；最终171 check集成只调用RunCoreRiskCheck，另交最小集成补丁，不改171 immutable候选。

## Task 6 — Original settings / actions UI

Files: new web/src/core-risk-policy.ts, components/CoreRiskPolicyPanel.vue, tests/riskcheck177-*.test.ts, e2e/riskcheck177.spec.ts; minimal App/settings integration; OpenAPI module/docs/sourceallowlist.

- [ ] 使用实际Go/PG HTTP返回构造合成fixture，原字段/默认/条件参数/单位一一匹配；只读操作员、本人super写、取消/焦点/忙碌/late GET/POST/原地CSRF/CAS/unknown禁止自动重发。
- [ ] 真实处置记录完整ID/时间/count/money/expiry；明确人工记录与自动处置的来源，不将资料可编辑当已执行。
- [ ] Vitest模型/组件actualRED→GREEN、桌面/375实际浏览器/目视、无溢出和大ID；仅vue-tsc --noEmit，无在线依赖或生产build。

## Task 7 — Immutable delivery / integration gate

- [ ] 实际migration102登记/二次Migrate保持所有旧行列/原schema SHA/time；开发父175暂缺100/101，root最终98..102完整登记链另测，不部署gap分支。
- [ ] 原新付款/回调/query/unknown、原IP/买家统计/blacklist、自动停渠道、有限资金及广前端影响回归；保留全部失败，准确分根/子项/skip与sourcehash。
- [ ] 父175/171-v2未动、候选全path SHA、allowlisted差异、原证据摘要、不包含secret/config/cert/runtime，交独立review。只附加owned synthetic资源cleanup，不触他方容器。

## Core scope extension: group inheritance and QR entry protection

The strict payment `info` map remains limited to the two existing payment types. A separate strict risk `config` whitelist owns the original `pay_block_oversea` group override (empty or `_` inherits; `0`/`1` overrides). Configuration requires the original super session, global/group CAS and independent committed revision anchors; it must not silently activate imported descriptive group JSON. Current effective membership is read in the admission transaction, its expiry is frozen, and the final DB clock check follows all new writes. Group changes never recalculate old order fees or modify old financial proofs. The group editor must show the actual override and inherited effective value, including group 0 and full integer IDs.

Readable Release `Plugin.php` (SHA 2b9e3fd888d23a731d10c7302004a0e827d409c5152a3eadb011ddcab8065f32) lines68–89 implements `checkpay`: compare original order IP with the current scan HTTP IP, then reject (`1`) or render warning (`2`) before continuing to the stored private pay URL. Lines112–132 compare province+city; missing library/lookup/exception originally permits continuation. `Payment.php` lines110–115,193–198,276–281 wrap HTTP(S) QR/H5 URLs and retain the original URL. `functions.php`1533 accepts HTTP/HTTPS only. This is an intermediate customer navigation step after payment material creation, not a callback, receipt verifier or new payment send.

Input provenance: Release ordinary submit uses global `clientip` (`Pay.php`10/156; `common.php`80–81 → `member.php`2 `real_ip`), while mapi assigns signed request `clientip` at292, requires/validates it at318/321 and stores it at422. Therefore the API customer-IP declaration is the comparison reference and must be labelled as a declaration, not replaced by the merchant-server TCP peer. Only the current scan IP is obtained from canonical HTTP RemoteAddr / explicitly trusted proxy. Pro public UI exposes0/1/2 and states only Alipay/Wechat scanning scenarios with correct IP acquisition; its internal implementation remains encoded.

Implementation sequence:

1. Complete current policy/admission/Check/action/notice safety gates and preserve existing red/green receipts. Reject new geographic enablement when configured local IPv4/IPv6 lookup capability is unavailable; runtime lookup failure does not affect authenticated callbacks or original query recovery.
2. Add the separate, protected group risk source and dedicated UI; test global vs override0/1/inheritance, effective membership expiry after the final origin write, no-policy compatibility, no historical reprice, group-row/config rollback and full IDs.
3. Add original-born provenance and a frozen QR policy version for new HTTP-born orders. Preserve the signed customer-IP declaration vs direct HTTP source label. Retain old orders/materials; never retroactively turn an existing attempt into another first send or rewrite its fee/financial MAC.
4. Freeze the original QR material classification/hash with the first provider result and safe independent source audit. Public response/PNG/checkout views use a platform intermediary only for supported HTTP(S) QR material. Original private provider material stays unchanged. A missing/tampered required source cannot fall back to exposing a direct URL.
5. Add signed scan-entry and explicit warning-continuation capabilities bound to the original order/source, short expiry and actual scan peer; continuation redirects only to the original validated private material. No caller URL or new payment intent is accepted. Query/return/notification callbacks remain outside the region gate. Test spoofed headers, original source declaration, same/different province+city, unknown region/library failure,0/1/2, explicit continue/cancel, forged target/expiry/context, already-paid receipt recovery, and old-order compatibility.
6. Publish only self-authored code/evidence summaries. Original proprietary source remains outside release allowlists. Integrate the real detector into171 with a minimal patch and original same-transaction lease/current generation guard before and after all effects; leave the reviewed171 candidate unchanged.


## 2026-10-26 接手后完整性修订
实际字段已由初始24扩展至25，补原 check_pay_regoin；group.config两项均可继承/覆盖并真实消费。扫码来源、H5/native/WAP中转、签名继续、库错误、原Query/Return均有实际HTTP证据。私有facts增加精确canonical形状，真实大小写shadow tamper RED保留；3个组件的真实ApiClient双编码RED与375横溢RED已修。独立CLI默认off/显式once/同key恢复真实进程验证。184自有整合树包含98 child新准入与四Check中的子通道分支、只读配置/实际风险、管理员通知及171同Tx租约最终门；两项业务插件仍按用户要求暂缓。详见新增compatibility/integration文档及最终测试索引；未与180/185最终累计。
