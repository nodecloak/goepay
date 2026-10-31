# 整体回归测试（2026-10-31）

状态：本轮测试执行、独立核对与资源清理均已完成。原始 Go 全量命令 exit 1，不能判为全绿；唯一失败已完成取证，属于旧契约断言需同步，原库尚未修改。

本轮测试对象为 `E:\GOEPAY` 当前工作区，HEAD `ab158e5` 加现有未提交修改。测试前固定的 2,819 个核心源码与测试文件哈希为 `e924be924b22625d6cbedb2dddd94d32f98cfaa6ab5ad1c75807f5f6f34e5772`。该清单没有囊括顶层 `tests/` 及前端支撑文件；它们另以初始/当前 Git 状态无改动、逐文件等于 HEAD 的 SHA 核验增补证明，见 `backend/extra-tests-source.json` 和 `coverage/support-source-verification.json`。没有修改业务源码，也没有访问生产环境或发起真实交易。

独立核验实际 Go 编译及嵌入输入 2,221 个，全部被核心清单与增补的 191 个文件覆盖，没有缺失或变化；增补文件逐字节核对原 HEAD。`go list -deps -test`、`go mod verify` 均通过。Windows 未包含的仅为已单独完成 Linux 验证的 sandbox 架构文件。详见 `.tools/overall213/coverage/go-input-provenance.json`。

## 已确认的问题

### P2：普通资源弹窗没有接管或归还键盘焦点

真实本地浏览器复现：打开“新增商户”后焦点仍在背景按钮；按 Tab 跳到背景的“导出筛选结果 CSV”。“新增授权域名”同样跳到背景“刷新”。取消后焦点落到 BODY。作为对照，“新增用户组”会聚焦表单，Tab 保持在弹窗内，取消返回触发按钮。

原因：`web/src/components/ResourcePage.vue` 的 `trapGroupFocus` 仅在 `isGroupEditor` 成立时执行，普通资源弹窗没有相应的初始聚焦、焦点约束和恢复流程。

证据：`.tools/overall213/browser/focus-probe.json`、`browser/screenshots/focus-merchant-list.png`、`focus-domain.png`、`focus-group-list.png`。

### P2：375px 下添加黑名单弹窗被截断

手机浏览器打开“添加黑名单”后，右侧关闭按钮与底部操作按钮超出视口，正常点击无法滚动到目标。外层隐藏溢出使 `document.scrollWidth` 不能单独发现这个问题。

原因：`BlacklistAddDialog.vue` 传入固定 `width="520px"`，`KitModal.vue` 通过内联 width 覆盖默认响应式宽度，没有限制到可用视口。共享组件还有其他显式宽度调用，应在修复时一并检查。

证据：`.tools/overall213/browser/screenshots/mobile-final-admin-BlackList-failure.png`、`browser/findings.json`。

### P2：OpenAPI 生成器和现有规范不一致

在本轮工具目录独立执行生成器，原规范未被覆盖。现有规范为 313 个路径、384 个操作，重生成结果为 298 个路径、368 个操作，丢失 15 条原有路径，包含短信设置、投诉处理和分账统计。直接执行 `scripts/test.ps1` 中的生成步骤会产生该差异。

此外，当前规范与重生成规范都没有 `/console/v1/{role}/workflows/{kind}/lookup`，也没有说明 `can_confirm_receipt` 能力字段。接口的运行与授权测试通过不能替代规范同步。

证据：`.tools/overall213/coverage/openapi-audit.json`，包含文件哈希和完整差异清单。

## 已完成的验证

| 范围 | 新运行结果 | 收据（项目根目录下） |
| --- | --- | --- |
| Go 全包、真实独立 PostgreSQL | 67 包全部终态：51 通过、15 无测试文件、1 失败；2,758 个顶层通过（含子用例 4,562 通过）、1 失败、34 条件跳过；命令 exit 1，无超时或其他失败 | `.tools/overall213/backend/full.jsonl`、`full-result.json` |
| 完整前端单元测试 | 1,458 通过，0 失败，0 跳过 | `.tools/overall213/frontend-unit.json` |
| 完整模拟接口浏览器测试 | 576 通过，0 失败，0 不稳定；13 项显式本地集成测试默认跳过 | `.tools/overall213/frontend-browser.json` |
| 显式本地 Go 后端浏览器流程 | 核心 9/9、补充 4/4 全部通过，无跳过；逐项核实覆盖上述 13 个默认跳过项 | `.tools/overall213/live-core.json`、`live-nodes.json`、`coverage/frontend-independent-audit.json` |
| 显式 Go/Chromium 条件用例隔离副本补验 | 12 个顶层、含子用例 29 项全部通过，0 跳过；仅副本适配旧定位器并增加不改变请求及断言的诊断 | `.tools/overall213/backend/browser-overlay.jsonl`、`browser-overlay-result.json` |
| TypeScript 与前端构建 | 通过 | `.tools/overall213/frontend-build-result.json` |
| 所有 Go 包 vet、build | 通过 | `.tools/overall213/backend/build-vet-result.json` |
| 当前源码 Linux 关键 race 专项 | 7 个命名顶层测试全部通过、未发现数据竞争；包含并发入账/退款预留、组合退款单次 claim、未知不重发和资金授权事务单用；不是全包 race | `.tools/overall213/backend/linux-race.jsonl`、`linux-race-result.json` |
| Linux 架构专属检查及构建 | `TestNetworkAndExecutionPolicyIsComplete` 1 个顶层测试通过，内部包含架构及网络/执行 syscall 多组断言；当前后端所有包 Linux 编译通过；不是 Linux 全包测试 | `.tools/overall213/backend/linux-sandbox.jsonl`、`linux-sandbox-result.json`、`linux-build-result.json` |
| Linux 普通文件内的操作系统分支 | 四个文件中的 11 个顶层、含子用例 31 项全部通过，0 跳过；包括实际风控 CLI、系统更新签名包与取消/CAS | `.tools/overall213/backend/linux-os-branches-summary.json`、`linux-os-branches-result.json` |
| 原 PHP 实际互操作 | 16 个顶层、含子用例 42 项全部通过，0 跳过；原 `testphp.Require/Run` 未修改 | `.tools/overall213/backend/php-complete-summary.json`、`php.jsonl`、`php-sequence169.jsonl`、`coverage/php-runtime-status.json` |
| 仓库 Node 小程序客户端实际 HTTP 互操作 | 1 个顶层、微信与支付宝 2 个子用例，共 3 项通过，0 跳过；运行真实客户端、本地 Go/PG 与夹具 TLS 上游 | `.tools/overall213/backend/mini-client.jsonl`、`mini-client-result.json` |
| 支付材料契约独立补验 | 仅副本更新一份旧契约测试；2 个顶层测试各运行 2 次，共 4 次通过，原主套件失败仍保留 | `.tools/overall213/backend/contract-overlay-summary.json`、`contract-overlay.jsonl`、`contract-overlay-diff.patch` |
| 模拟通道收款与部分退款 | 通过真实 Go 服务及数据库入账，1 分部分退款成功，浏览器仅提交一次 | `.tools/overall213/final-real-refund-result.json` |
| 原申请响应丢失恢复 | 已提交后丢弃响应、刷新恢复原号；同号探测返回同一记录，没有额外预占 | `.tools/overall213/final-real-workflow-result.json` |
| 真实本地 HTTP 幂等负例及并发 | 坏签名和重复参数不建单；10 并发同号下单只生成一单；10 并发模拟回调只入账一次，余额严格增加 3 分 | `.tools/overall213/browser/http-idempotency.json` |
| 账务核对 | 33 项异常计数全部为 0 | `.tools/overall213/reconcile.json` |
| 管理端与商户端逐节点巡检 | 63 + 33 路由，1440px/375px 共 192 页；确认上述两项交互问题 | `.tools/overall213/browser/summary.json` |
| 只读并发和权限负例 | 80 次 GET、并发 4，0 失败；未登录 401、跨付款商户 403、重复参数 400 均符合，JSON/no-store/nosniff 检查通过 | `.tools/overall213/coverage/read-smoke-result.json` |

逐节点巡检覆盖页面打开、筛选、刷新、选择、菜单、详情、表单打开与取消、页签、键盘观察和布局。每个宽度的 96 路由中，4 个隐藏辅助页、4 个关闭功能、4 个菜单未开放入口及 1 个实名认证暂缓页分别记录。空态没有当作复杂业务数据、真实插件或写入场景验收。最终巡检没有非预期 HTTP、API、脚本或网络错误；这不表示交互全部通过。

只读延迟的共享工作站观测 P95 为：管理仪表盘 21.29ms、订单 25.28ms；商户仪表盘 27.89ms、订单 45.38ms。样本少且与其他测试并行，不作为生产容量或优于 PHP 的基准。

另在当前源码的独立 Linux 副本与合成 PostgreSQL 上执行 3 个数据库基准，每项固定 10 次：创建订单 13.553696ms/op、查询订单 0.336122ms/op、并发支付记账 19.693567ms/op。夹具准备已排除计时，并发记账后验证余额与账务不变量。此结果只表示共享环境的小样本数据库操作，不含签名、TLS 或真实支付上游延迟，不能换算为生产容量或 PHP 性能结论。收据：`.tools/overall213/backend/linux-benchmark-summary.json`。

## 后端与条件用例

Go 主套件使用实际独立 PostgreSQL 数据库，命令为 `go test ./... -count=1 -p=2 -parallel=1 -timeout=150m -json`，于本地时间 2026-10-31 03:04:48 以 exit 1 正常结单。67 个实际 Go 包均有终态，支付包完整通过，唯一失败为下述契约测试。本轮复用了既有合成测试池，没有新建容器。迁移类测试较慢，已调整为包并行 2、测试并行 1，并限制本轮进程的 CPU affinity，使 pgx 默认连接预算受控；没有降低 PostgreSQL 的同步提交或 fsync 设置。授权过期场景保留原 60 秒等待，未缩短窗口以规避测试。

此前两轮全量启动因配置及调度调整中断，`attempt1-full-result.json`、`attempt2-full-result.json` 的 exit -1 和原始日志均保留，不计为通过。本报告的主套件数字仅取最终完整结束的这一轮。

原始主套件的 `TestV2WebPreservesHTMLAndQRMaterialWhileJumpStaysProtected` 已失败：支付宝的 `pay_type` 仍为 `html`，但返回 HTML 与持久化 attempt 的材料逐字不同。旧夹具没有返回地址，当前逻辑为原生表单增加平台保护的 `return_url` 并重新签名；两次取证中实际差异仅为新增 `return_url` 与改变 `sign`，时间戳和 UTF-8 charset 表单前缀未变。

独立副本仅更新这一份契约测试，原仓库未改；核对 3,139 个文件后确认业务文件无差异。新增断言独立验证原始与返回材料的 RSA2 签名、返回地址的订单绑定 HMAC、商户/订单/应用/产品/金额/subject、其他签名输入的原字节，并从真实 adapter `Create` 返回时捕获材料，证明首次展示没有改写持久化 attempt。一个订单只有一个 attempt，没有额外通道请求；微信二维码原字节、SDKv2 响应签名、受保护 jump 和已支付后隐藏材料的原断言均保留。两个顶层测试各执行两次全部通过。结论是旧字节等式断言与已核实的返回地址刷新不一致；原主套件仍记失败，后续需同步正式测试。补验第一次字段集合断言失败和进一步字段取证记录也完整保留。

显式 Go/Chromium 隔离副本完整补验已结单通过，原 Pro 离线 oracle 和固定官方地域数据也已核验文件哈希后启用。不能仅因打开环境开关就把这些用例记为通过。

最终按原始 JSONL 的 `Package + TestName` 精确核对 34 条默认跳过：16 条原 PHP、12 条真实浏览器、1 条 Node 小程序客户端，合计 29 条均有实际执行的补验收据；其余 5 条为独立夹具导出工具，仍记跳过，不记通过。没有无法说明的跳过，也没有因为数据库缺失而跳过业务测试。浏览器补验使用了下述单独证明的测试脚本副本，不能冒充原脚本全部通过。15 个没有测试文件的包也没有当作执行过测试。

PHP 互操作先保留原环境缺失导致的跳过，再实际补齐固定官方 PHP 8.2.35 和同版本官方源码编译的 BCMath 扩展。执行器仅在本轮独占副本调整普通文件 stdio 与变量应用阶段，未改仓库 `Require/Run`、sandbox 或网络/exec 拒绝规则；15 个原参考文件哈希仍严格复验。新 READY 经真实版本、UID65534、seccomp 和 nonce proof 验证后生成。先完成 15 个顶层用例，再经精确收据映射发现并补跑顶层 `tests/interop` 的原 Pro 全局父级序列用例，合计 16 个顶层、42 项通过。共享网络命名空间不等于独立网络命名空间，报告没有声称后者。环境修复的原始失败、执行器 diff、源/产物 SHA 及独立复审均保留。

Linux race 在复用的现有 runner 内，本轮独占目录执行。2,060 个后端文件复制后逐 SHA 核对，没有使用旧 `/src` 代码冒充当前版本，也没有修改旧快照。另对普通文件内的 `runtime.GOOS` 路径补跑系统更新与风控 CLI 共 11 个顶层、31 项用例，实际编译执行 Linux CLI，全部通过，前后源哈希一致；测试库已零连接删除。单个迁移用例的 Linux 对照未明显快于 Windows，因此没有再启动一轮 Linux 全包；Linux 全包未执行，不作通过声明。

独立收据审计纠正了早期对 Linux sandbox 的口头计数：原始日志实际为上述 1 个顶层测试，并非 2 项测试；首次按 2 项预期执行的审计拒绝记录保留，没有测试失败被改记为通过。

## 测试工具和证据边界

初始逐页工具曾把说明浮层按 Tab 自动关闭误判为等待失败，并误把业务开关当列表选择。后者的一次写意图被浏览器守卫拦截，没有发送到 API。后续仅修正工具、保留初始收据并重新巡检；最终巡检守卫的业务写入和外部请求均为 0。后台详情异步出现的工具竞态也保留失败，并在明确等待对话框后单页重查。

原始显式 Go/Chromium 批次有 12 个顶层测试，9 个通过、3 个失败（包含子节点为 20 通过、9 失败）。原因分别是 `credentials199-http-browser.mjs` 的旧密码标签、`login-captcha205-http-browser.mjs` 仍查找旧退出按钮，以及 `parent180-test-http-browser.mjs` 没有先展开显示列设置。原始失败保留，不算通过。

随后建立独立执行副本，逐文件确认后端及 `web/src` 业务文件与冻结源完全一致，在副本中适配上述三个脚本，另增加不改变原请求、返回或断言的测试诊断。差异保存到 `.tools/overall213/backend/browser-overlay-diff.patch`。最终完整补验 12 个顶层、含子用例 29 项全部通过、0 跳过；原仓库没有修改，因此这三处测试维护问题仍需后续同步。

隔离副本的一次邮箱首次绑定场景中，资料查询曾返回非成功 HTTP；原脚本没有记录响应状态和正文，不能事后断言根因。追加的诊断保持原请求及断言不变，两轮窄复验、完整组补验和单独首次绑定/1440px 五轮复验累计 17 次资料查询全部为 200（31.80–41.35ms），其中同原失败条件累计 8 次成功。实际连接池 MaxConns=4，取消获取为 0，累计等待最高 148ms。该事件仍记为未归因、后续未重现，不能写成已修复或直接归因于环境。完整过程见 `.tools/overall213/backend/browser-verification-summary.json`。

## 资源清理与最终证据

本轮没有新增或删除容器。仅使用本轮拥有的合成测试数据库；Go 主库删除前连接为 0，一次普通 DROP 自然完成，耗时约 115 秒，随后从管理库只读复核目标库已不存在、连接为 0。没有使用 FORCE、终止连接或修改其他数据库。Go 全量原始 exit 1 在清理收据中保留，未被清理命令的成功覆盖。

UI 测试库的前两次客户端分别在 15 秒、60 秒超时，原错误收据保留；数据库端等待 checkpoint 后自然完成，后续独立只读核对证实该库不存在、连接为 0。这里记录的是删除后状态核验成功，没有将客户端超时重写成 exit 0。Linux 测试库的两次自有生命周期也均在零连接后清理。

本轮前端 5334/5335 与 API 8215 均已停止，没有遗留监听。原本地预览指针保持不变，`http://127.0.0.1:8187/admin/dashboard` 最终返回 HTTP 200；本轮没有升级该预览。源代码副本、准备好的 PHP 执行器及原始日志保留用于复现，不作为持续运行服务。没有访问生产环境，也没有发起真实资金交易。

主要终态收据：

- `.tools/overall213/coverage/complete-independent-audit.json`：执行完成 `true`、原测试全部通过 `false`；逐包/逐用例核对、来源和失败保留。审计命令 exit 0 只说明收据核对成功，不覆盖测试命令 exit 1。
- `.tools/overall213/backend/final-summary.json`：后端原始全量、补验、构建、Linux 与环境证据汇总。
- `.tools/overall213/backend/cleanup.json`、`cleanup-status-latest.json`：Go 自有库零连接删除及只读复核。
- `.tools/overall213/sandbox-cleanup.json`、`backend/linux-cleanup.json`、`backend/linux-os-cleanup.json`：UI 与 Linux 自有库清理状态。
- `.tools/overall213/final-preview-and-services.json`：原预览及本轮服务退出状态。

需要后续修复的事项为两处产品交互问题、接口规范的三处同步差异，以及正式库的一条旧支付材料契约断言和三个旧浏览器测试脚本。邮箱查询的一次非成功 HTTP 仍未归因，后续复验没有重现。本轮保留业务源码不变，完成的是整体测试和取证，不能据此宣称以上问题已经修复或所有功能全绿。
