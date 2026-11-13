# USDT 托管收付验收记录

日期：2026-11-13。代码位于 `E:/GOEPAY`。本轮仅使用合成资金、独占测试数据库和本地 HTTP/TLS 链节点替身；真实资金交易为 0，新建容器为 0。未启用或部署主网收付款。

## 验证结果

| 项目 | 实际结果 |
| --- | --- |
| Go 全量回归（85 个包、3360 个根用例） | 3317 通过，43 跳过，0 失败 |
| Go 并发检测（指定资金与身份相关用例） | 335 通过，0 跳过，0 数据竞争报告 |
| 调度测试时间夹具补验 | 3 根定向普通测试、3 根定向 race 测试、2 根命名检查通过；不修改产品调度逻辑 |
| 补充 opt-in 验收 | 15 根用例、14 子用例实际通过，0 跳过；17 次受保护浏览器子运行 |
| Linux USDT 验证 | 63 个根用例通过，0 跳过 |
| 前端单元测试 | 2055 通过，0 跳过 |
| 浏览器回归（生产构建、本地 API 替身） | 752 通过，0 跳过，0 重试 |
| 静态检查、依赖一致性、OpenAPI 工具测试 | go vet、go mod tidy -diff、git diff --check 通过；文档工具 3 项通过 |
| 构建 | api、crypto-worker、crypto-signer、crypto-keygen 的 Windows/Linux AMD64 共 8 个构建通过 |

Go 普通回归按最多 8 个根用例一批，使用独立进程和独占数据库，3 个并发批次；并发检测使用 `-race` 和 2 个并发批次。每批记录实际通过、跳过及退出状态。完整测试清单与前一清单逐项比较，未丢弃已有用例。此前修复前的失败与主动中断记录保留，没有计为最终通过。

所有结束的测试批次均在确认零连接后删除自己的数据库；没有 FORCE 删除或终止其它会话。Linux 复用现有测试容器，导出日志后删除本轮的独占临时目录。现有预览保持原状。

各阶段的运行代码、SQL、API 逐字节一致。普通全量 Go 通过后，race 曾发现旧测试以一分钟计划假定“同周期”，在 12:43:55–12:44:03 跨分钟后错误地把合法新周期来源记录当作重复；实际请求及转账仍各 1 次。修正仅限一个测试文件重命名、专用单周期夹具和一行命名清单移除；保持严格的一份来源、一笔转账及原 run 身份断言。新增固定时间边界用例后，3 根定向普通/race 与 2 根命名检查通过，再在最终测试快照完整重跑 race。原始失败保留于 `race-regression-failed-clock-boundary`。普通全量没有因这项 test-only 修正重新执行，旧两根测试重命名与新增一根逐项对账，最终清单为 3361 根；没有把旧失败或跳过改记成通过。

原失败临时库已清理；跨分钟的根因判断来自执行时间戳和调度源码，固定时间边界回归验证了对应的周期规则。

前端测试前后摘要一致。最终前端产物另外由 `actual-browser-final` 和 race 的实际数据库/浏览器组合验收覆盖，两者均在最终前端构建完成后执行。普通 Go 回归中的较早浏览器执行记录保留原样，不把旧产物的结果改记为新产物通过。

## 两链业务闭环

TRON 与 Ethereum 分别执行真实协议适配器、真实 secp256k1 签名器与 PostgreSQL 的组合验收：本地节点观察到 10 USDT，商户入账 9.9、平台手续费 0.1；归集 10，退款 1，原链结算 8.9。商户可用与预占余额最终均为 0，原人民币账户保持原值。所有广播均指向本地替身。

三笔出款的模拟实际网络费分别累计为 TRON 1,500,000 sun、Ethereum 3,600,000,000,000,000 wei，独立记录且只入账一次，不与 USDT 相加。Ethereum 另外验证未签名交易取得签名器永久撤销证明后，原空缺 nonce 可安全复用；旧请求永远不能再签名。

| 验证点 | 覆盖内容 |
| --- | --- |
| 收款及异常金额 | 独占地址、少付补款、超付、迟到、历史款、错合约/错网络、失败与未最终确认回执、节点分歧及游标重组保护 |
| 资产账本 | 两链独立，人民币不参与；重复事件幂等；已提交 journal 不可追加；余额不能绕过分录直接修改；跨资产外部订单号不得重复 |
| 出款及退款 | 并发不可超支、退款上限、未入账款只能按原单持有额度退回；失败或撤销恢复原资金类别；未知交易不释放预占或另建替代交易 |
| 签名隔离 | mTLS/SAN、只签固定网络 USDT transfer、真实载荷与字段绑定、签名独立恢复验证、重启幂等、nonce 防复用、USDT/笔数/原生币费用日限额 |
| 归集与任务恢复 | 持久 revision 队列，新到账重新排队；失败有限退避、三次暂停、显式恢复；出款轮转避免前 100 条失败请求阻塞后续请求 |
| 账户与权限 | 商户隔离、超级管理员限制、CSRF、本人密码、持久会话及撤权竞争、代登录不能写资金、地址审批与 24 小时冷静期 |
| 回调及兼容 | 旧接口保留 CNY money，USDT 数量和网络另列；原单报价/网络/地址不随配置变化；通知签名和持久退避；已支付返回链接签名 |
| 前端资金意图 | 发送前保存公开规范输入及请求号；提交响应丢失后刷新只查询原请求；真实本地 HTTP 验证恰好一次 POST、一个出款申请；404 不自动解锁或重试 |
| 收银台和布局 | 375/1440、两链二维码完整居中；过期后继续核对原款项；单独启用 Ethereum 的默认值；不改写已填网络；两种后台/手机/桌面配置列宽与对齐；带地址数据验证手机字段及表格行不会被撑高 |
| 可信支付链接 | origin 缺失或恶意值不消耗地址；不信任 Host/Forwarded；v1/v2/submit 返回可信绝对链接；关闭新报价不改变既有订单 |

## 跳过记录及边界

### 普通 Go 回归

根用例真实跳过 43 项；子用例真实跳过 0 项。

| 包 | 用例 | 原因 / 补充执行 |
| --- | --- | --- |
| `goepay/internal/adapters/payment` | `TestOriginalAPPProductsPHPInterop` | products_php_test.go:18: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/adapters/payment` | `TestOriginalAlipayCertificateSerialPHPInterop` | alipay_certificate_php_test.go:24: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/adapters/payment` | `TestOriginalAlipayOrderCodePHPInterop` | alipay_order_code_php_test.go:19: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/adapters/payment` | `TestOriginalMiniLaunchEncodingPHPInterop` | mini_launch_php_test.go:16: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/adapters/payment` | `TestOriginalProviderPHPInterop` | php_interop_test.go:19: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/adapters/payment` | `TestOriginalScanSignaturePHPInterop` | scan_php_test.go:20: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/adapters/payment` | `TestOriginalWechatBankOAEPInterop` | bank_php_test.go:21: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/compat` | `TestPHPSDKInterop` | php_interop_test.go:26: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/grouppolicy` | `TestOriginalGroupRoutingPHPInterop` | original_php_interop_test.go:19: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/merchantnotice` | `TestOriginalProNotice80PHPTemplatesInterop` | php_interop_test.go:16: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/merchantnotice` | `TestOriginalProNotice80PHPWechatMappingsInterop` | wechat_php_interop_test.go:11: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/pagetemplate` | `TestWriteSanitizerCorpus` | upload_pay_test.go:181: GOEPAY_SANITIZER_CORPUS and GOEPAY_SANITIZER_OUT not set |
| `goepay/internal/pagetemplate` | `TestWriteTemplatePreviewPages` | preview_pages_test.go:26: GOEPAY_TEMPLATE_PREVIEW_DIR not set |
| `goepay/internal/pagetemplate` | `TestWriteUploadSamplePages` | upload_test.go:188: GOEPAY_TEMPLATE_PREVIEW_DIR not set |
| `goepay/internal/payment` | `TestBuyerCharge104OriginalProOracle` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/payment` | `TestBuyerCharge104OriginalProPrecisionOracle` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/payment` | `TestOriginalBuyerCharge104ReadableReleasePHPInterop` | buyer_charge104_php_test.go:21: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/payment` | `TestOriginalGroupSettlementConfigPHPInterop` | group_settlement_php_interop_test.go:17: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/payment` | `TestOriginalSettlementBatches84FourCSVFormatsPHPInterop` | settlement_batch_php_interop_test.go:19: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/riskgeo` | `TestRisk177PinnedOfficialIPv4IPv6OfflineQueries` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestAlipayBrowser239ActualBrowser` | alipay_browser239_actual_test.go:63: GOEPAY_ALIPAY_BROWSER239=1 requires existing local Chromium, Playwright and synthetic PostgreSQL |
| `goepay/internal/server` | `TestBuyerCharge104ExportDisposedActualHTTPFixture` | buyer_charge104_http_fixture_test.go:16: explicit synthetic public HTTP fixture export path required |
| `goepay/internal/server` | `TestCheckoutPollRealBrowserLocalProvider` | checkout_poll_browser_test.go:38: GOEPAY_CHECKOUT_BROWSER=1 requires local Chromium and synthetic PostgreSQL |
| `goepay/internal/server` | `TestCredentials199ActualBrowserPasswordAndMD5Rotation` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestEmail197ActualBrowserHTTPPrivateTLSExistingBindingAndReauthentication` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestEmail197ActualBrowserHTTPPrivateTLSFirstBindingAndReauthentication` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestGenerateMerchantRSAPHPInterop` | account_rsa_test.go:49: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |
| `goepay/internal/server` | `TestLoginCaptcha205ActualBrowserHTTPWithPrivateTLSAndPersistentSessions` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestMailAccounts195ActualBrowserHTTPOriginalSMTPDialogAndPasswordBoundary` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestMailFrequency194ActualBrowserHTTPAndSameKeyEnrollmentConsumer` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestMerchantScan192ActualBrowserHTTP` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestNativeScan202ActualBrowserHTTPPreparesWithoutAnyCharge` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestOrderBatch185ExportDisposedActualHTTPFixture` | order_batch185_fixture_test.go:16: explicit local UI fixture export only |
| `goepay/internal/server` | `TestParentChannel180ActualBrowserHTTPTestPayment` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestPublicTestPay203ActualBrowserHTTPWithPrivateTLSAndNoConsoleIdentity` | public_testpay203_browser_test.go:172: GOEPAY_PUBLIC_TESTPAY203_BROWSER=1 uses only synthetic Go/PG/Chromium peers |
| `goepay/internal/server` | `TestRisk177PinnedOfficialGeoDrivesActualAdmissionAndScan` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestSiteBrand193ActualBrowserHTTPImages` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/internal/server` | `TestSubchannel168BExportSyntheticHTTPUIFixture` | subchannel168b_fixture_test.go:17: dedicated synthetic UI fixture target required |
| `goepay/internal/server` | `TestSystemUpdate183ExportDisposedActualHTTPFixture` | systemupdate183_export_test.go:18: explicit local actual HTTP fixture export only |
| `goepay/internal/server` | `TestWechatBrowser240ActualBrowser` | wechat_browser240_actual_test.go:37: GOEPAY_WECHAT_BROWSER240=1 requires existing local Chromium, Playwright and synthetic PostgreSQL |
| `goepay/tests/contract` | `TestGlobalAccess179ExportDisposedActualHTTPFixture` | globalaccess179_fixture_test.go:18: explicit synthetic frontend fixture export only |
| `goepay/tests/contract` | `TestMiniClient81NodeHTTPInterop` | 默认门禁跳过；已在独立 opt-in 实际执行通过 |
| `goepay/tests/interop` | `TestSequence169ProGlobalParentSequencePHPInterop` | sequence169_pro_interop_test.go:21: actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime |

### 并发检测

根用例真实跳过 0 项；子用例真实跳过 0 项。

默认执行中的跳过记录保留原样；其中 15 项已在独立 opt-in 中实际执行通过，单独列出，没有回写默认记录。旧 PHP 互操作当前缺少已验证、可复用的隔离 PHP 运行环境，仍然跳过，不能据此宣称其运行时验收通过。新增 USDT 的必需业务、签名与链适配器验证使用本地替身执行；Windows 的操作系统专属分支按实际输出记录，Linux 的 63 项验证覆盖签名文件权限和密钥目录检查。

本地验证不等同于主网或钱包 App 实机验收。当前隔离签名器是一钱包一签名密钥，提供 HSM KeyProvider 接口，没有某个商业 HSM/MPC 连接器或链上多签。TRX/ETH 手续费补充与冷钱包到热钱包补资仍由独立钱包管理流程完成；不包含自动法币兑换、跨链或自动补资。

## 证据位置

本机 `.tools/usdt/final-results.json` 汇总严格验证结果；`go-regression`、`race-regression` 保存清单、源码摘要及每批原始 JSONL/清理记录；`linux-custody`、`builds`、`web-regression`、`browser-regression` 保存对应日志与回执；`actual-browser-final` 保存最终产物的独立组合验收、截图及摘要，`browser` 保存 race 组合截图；`optional-regression` 保存 15 项补充验收，较早证据归档保留。运行说明见 [USDT 设计与运行文档](../design/usdt-custody.md)。
