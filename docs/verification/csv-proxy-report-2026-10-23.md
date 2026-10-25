# CSV、代理及退票统计专项

日期：2026-10-23，目录 E:\GOEPAY。只使用回环专用测试 PostgreSQL、随机 schema 和本地假上游；没有访问生产或真实资金操作。本文不是全项目最终回归。

## 筛选结果导出

真实红测：27条属于商户1、另一条属于商户2；`format=csv&limit=1&page=2` 原实现仍返回JSON分页。实现后返回27条本商户记录加标题，带UTF-8 BOM、附件和no-store；原金额123分保持字符串，公式文本加单引号，逗号/引号/换行按CSV编码。报表导出同一筛选得到27笔、33.21元，不受page影响。

另覆盖跨商户403、商户读取管理员商户表403、重复/未知format400、审计不可用不返回附件、50,001行拒绝而非截断、8MiB编码上限及两并发满载429。CSV先完整缓冲，再审计，再响应；下载审计包含主体/类型/行数/摘要，代登录同时记录真实管理员ID。

```text
go test ./internal/server ./tests/contract \
  -run 'TestCSVExport|TestReports|TestConsoleList|TestIncomeHoldProjects' -count=1
server 1.022s PASS; contract 6.776s PASS
go vet ./internal/server: exit 0
```

前端代理完成独立源码审阅，未发现CSV新增P1/P2；独立实际PG专项server1.061s、contract9.892s通过。前端38项unit/component、54项mock browser通过；7项真实Docker浏览器待统一部署后执行，不算本专项通过。

## 银行退票统计

独立审阅发现既有P2：transfer报表只查历史 `transfers.status=succeeded`，没有使用银行有效状态。真实回归在 `BANK_FAIL` 缺少退费证据的 reversal_pending 阶段复现：成功笔数仍1、金额1.00元。

现三个transfer报表共用生命周期投影，reversal_pending/reversed优先于历史succeeded，排除成功金额及成功扣款汇总。JSON与CSV均测试 SUCCESS→reversal_pending→reversed，实际历史账本状态仍succeeded，退票另记凭证，不改历史。该报表是当前有效成功出款统计，不是所有费用现金流或损益报表。

```text
go test ./tests/contract ./internal/server \
  -run 'TestBankTransferAPIAndConsoleDiscloseReturn|TestCSVExport|TestReports|TestTrustedProxy' -count=1
contract 5.942s PASS; server 0.692s PASS
go vet ./internal/server ./cmd/api: exit 0
```

## 显式可信代理

NewTrustedProxy默认关闭；严格CIDR、直接对端、单一代理覆盖X-Real-IP和原连接context。原测试补充mapped IPv4未指定地址后真实红测复现0.0.0.0被接受，规范化提前后已修复。纯单元通过；真实PG集成证明可信代理下不同客户各自限流，不可信来源轮换伪造头仍受同一预算限制，审计记录正确来源；签名买家IP不能绕过代理还原的客户来源黑名单。

```text
go test ./tests/contract ./internal/server -run '^TestTrustedProxy' -count=1
contract 3.983s PASS; server 0.688s PASS
```

详见[可信代理部署要求](../design/trusted-proxy.md)。本地Compose默认不扩大信任，不将整个Docker共享网段视为可信代理。
