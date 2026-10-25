# 五列批次文件导入预览增量验证

2026-10-23，`E:\GOEPAY`。只使用本机文件、假受款资料、Go httptest、随机schema PostgreSQL和开发版浏览器；没有访问生产或读取真实密钥，没有真实资金/打印外呼，没有重部署当前Docker镜像。此文仅覆盖本增量，不代替全项目候选验收。

## 实际行为

`POST /console/v1/{admin|merchant}/transfer-batch-imports/preview` 验证登录、当前角色、CSRF、商户归属、严格平面JSON；管理员必须规范merchant_id，商户不能指定他人。每账号+IP最多10次/15分钟，JSON704KiB、原文件512KiB。文件不写磁盘、不创建批次、不预留资金、不消费资金授权、不建立任务、不调用provider。

支持 UTF-8（可带BOM）CSV和XLSX，原标题严格为“序号、收款账号、收款人姓名、转账金额（元）、转账备注”。序号唯一，最多100条，金额严格元定点→整数分，长账号和前导0保留。序号占位的原空模板行可跳过；非空错误行整份拒绝，错误仅返回source_row和固定code，不回原单元格值。旧XLS暂不支持，页面明确说明。

XLSX使用标准库archive/zip、encoding/xml：压缩文件512KiB；最多64条目，每条1MiB/总解压4MiB；XML深度24/每part token30000；共享字符串最多1024且单格文本4096；首表最多101物理行/5列。检查真实ContentTypes/workbook/首sheet关系及坐标；拒公式、外链（含未使用part）、宏/嵌入、DTD/非XML指令、路径穿越、重复ZIP路径/单元格坐标/XML属性、多XML根、坏CRC/格式、数值账号。数值账号一律要求改为文本，不猜已丢失的精度。

响应的`rows`仅掩码姓名/账号和备注是否填写；`draft_items`含完整受款资料，只有相同已认证浏览器当前内存可使用，`Cache-Control:no-store`。UI先展示掩码，显式选择当前能力通道/方式，再“应用到当前草稿”；应用不创建批次。之后仍须原有整批核对/预留、资金授权和独立执行。切换商户、文件、取消编辑、离开页面会中止请求并清除预览；不使用localStorage/sessionStorage。文件不带通道，微信银行代码须应用后逐行补填。

## 红绿及固定证据

- CSV/XLSX正向测试首先对not-implemented stub失败；随后绿色。新增多XML根、重复属性、未使用外链、坏ContentTypes均先在真实解析器上失败，再修复通过。
- HTTP纯handler测试先因未实现hook失败，再验证CSRF/角色/owner/方法、重复/未知字段、XLS/坏金额、大小与限速，以及错误不回PII。
- 前端草稿转换首先因stub失败；错误行号显示先失败再通过。浏览器首先因无导入控件失败，随后3项通过。

最终命令及结果：

| 检查 | 结果 |
| --- | --- |
| `go test -p 1 ./internal/transferimport ./internal/server ./tests/contract -run 'TestTransferBatchImport\|TestCSV\|TestXLSX\|TestTransferBatchConsoleTLS' -count=1` | parser 0.990s、server 0.493s、真实PG合同11.832s，全部exit0 |
| `go vet ./internal/transferimport ./internal/server` | exit0，无诊断 |
| `npm test` | 30文件、84测试通过，2.05s |
| `npm run build` | TypeScript检查与Vite构建通过 |
| `npx playwright test e2e/transfer-batch-import.spec.ts e2e/transfer-batches.spec.ts --workers=1 --timeout=20000` | 3项通过，4.9s |
| `node docs/tools/build-openapi.mjs` | 94 paths / 109 operations，引用与operationId检查通过 |

真实HTTP合同确认：未配置ProviderFlow也能预览；CSV及通过首sheet关系定位的XLSX可用，原Pro空XLSX诚实报空；无cookie401、缺CSRF403、跨商户403、管理员缺owner422、停用owner401。前后transfer_batches/transfers/journals/ledger_requests/jobs/backoffice_audit及accounts快照完全一致。原批次TLS合同另验证prepare/execute/unknown/query/cancel/owner保持原行为。

浏览器验证：上传/掩码预览/应用草稿均0prepare、0execute；显式原批次确认后才1prepare、0execute；未知预留不换号。XLS在客户端拒绝且0上传；服务端坏CSV显示安全的第3行且不覆盖已有草稿；取消清除控件；375px预览无横向溢出；浏览器持久存储不含假私密账号。

原始命令输出保存在 `.tools/verification/transfer-batch-import-2026-10-23/`：go-tests.txt、web-tests.txt、web-build.txt、browser-tests.txt；新增文件清单和SHA256见同目录source-hashes.json。原格式与Pro/Release差异见 `docs/design/remaining-console-evidence.md`。本增量未把原PHP后台加密RPC或所有Excel格式标成完全兼容。
