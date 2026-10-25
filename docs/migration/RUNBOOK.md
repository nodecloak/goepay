# 离线脱敏迁移演练

此工具只读取本地 JSON 快照，不连接原 PHP 系统、原数据库或支付上游。默认只校验；`--apply` 仅接受明确的回环 IP PostgreSQL URL，拒绝远端域名、localhost 名称和 query host/service 覆盖。

## 输入与校验

示例：`docs/migration/sanitized-example.json`。金额必须是元单位字符串，商户 ID 为正整数，订单号保持原字符串。快照明确声明 source（epay_pro/epay_release）、schema_version=1、唯一 export_id、导出时间。

校验包括：重复 JSON 键/未知字段、重复商户/订单业务号/退款号、孤立引用、金额精度和溢出、零日期/无效时间、历史已支付时间、超额退款、非支持状态、冻结余额与 unknown 退款/转账义务总额是否一致。负余额及当前模型不能表达的历史状态应先列入差异台账，工具明确拒绝，不做静默修复。

脱敏快照不允许包含生产 API key、私钥、密码或原始个人证件。导入商户默认禁用，生成随机新 API key 并使用本地 master key 加密落库，不输出该 key。它是账务与历史数据迁移演练；凭据保留、旧商户无缝接入和原密码迁移应使用独立受控流程，不能因为此工具通过而声称零停机兼容已完成。

## 运行

```powershell
# 只读校验，无数据库连接
& D:/GoLang/sdk/go1.21.3/bin/go.exe run ./cmd/import-legacy --input docs/migration/sanitized-example.json

# 仅对已准备的独立本地演练库导入；master key 由本地安全配置提供
$env:GOEPAY_IMPORT_DATABASE_URL='postgres://goepay:<本地测试密码>@127.0.0.1:55432/goepay_migration_rehearsal?sslmode=disable'
# GOEPAY_MASTER_KEY 已设置为该演练库使用的 32 字节十六进制密钥
& D:/GoLang/sdk/go1.21.3/bin/go.exe run ./cmd/import-legacy --input docs/migration/sanitized-example.json --apply
```

`--apply` 自动校验并迁移目标数据库结构。目标不能已有财务流水，且源商户/订单不得与目标冲突。一次导入使用一个事务和全局迁移锁；晚期错误会回滚商户、订单、余额、历史退款/转账和导入记录。序列允许正常出现间隙。

同一 export_id + 相同文件 SHA256 重跑返回 already_imported=true，不增加余额；相同 export_id 内容改变则拒绝。导入文件应冻结，不要在第一次导入后重新格式化再重跑。

## 账务语义

快照可用余额和冻结余额分别建立从平台 clearing 到商户 available/reserved 的期初双边账。历史 paid 订单作为历史记录导入，**不再次入账**，也不生成支付通知任务。历史退款/转账记录不再次扣款，已包含在期初快照中。

unknown 退款/转账保留在途状态与冻结资金，报告明确列出尚需人工映射原支付通道的记录；不自动调用新通道、不重发退款或出款。冻结余额必须逐商户与这些未决义务相等。保证金、跨期结算占用或其他模型未覆盖的冻结项目需要先完成映射。

source=epay_release 设置 release-legacy 兼容画像，source=epay_pro 设置 pro-v1。订单可显式声明 version=v1/v2；未给出时使用 v1。API 凭据与真实通道不能由此自动启用。

## 已执行验证（2026-10-23）

在新建随机名称的本地 Docker PostgreSQL 数据库执行了示例：第一次 applied=true，重复执行 already_imported=true；检查到 clearing=-1284 分、available=1234 分、reserved=50 分。历史 5.00 元 paid 订单未再次增加余额，unknown 0.50 元退款保留。演练完成后只删除了本轮创建的临时数据库。

真实 PostgreSQL 测试还注入退款插入失败，验证导入商户、流水和导入记录全部回滚；验证同 export_id 内容变化被拒绝。测试命令：

```powershell
$env:GOEPAY_TEST_DATABASE_URL='<独立本地测试数据库 URL>'
& D:/GoLang/sdk/go1.21.3/bin/go.exe test -count=1 ./internal/migration
```
