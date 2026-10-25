# 运行中 Pro 通道盘点与移植记录

日期：2026-10-23。用户提供SSH访问，范围为读取当前Pro部署与微信/支付宝通道。本轮未修改远端文件、配置或数据库，未发起真实支付、退款、关单及其他资金操作。

## 已核实的实际部署

- Pro项目：`/www/p.ma3fu.com`，PHP CLI 8.2.35。
- 支付通道1：`alipay` 插件1.4，启用电脑网站/手机网站支付（apptype 1,2），RSA2公钥模式。
- 支付通道2：`wxpay` 插件1.2，启用JSAPI/Native（apptype 2,1），APIv2。
- 两个插件为明文，已下载到本机 `reference/server-pro/plugins/payment/`，记录SHA-256。
- 真实数据库表前缀为 `pay_`，35张表；相比本地38张安装表，缺少 `psinternal`、`internal_transfer`、`onecode`。不能将安装SQL当真实schema。
- 查询仅取schema、白名单通道元数据和这两个通道的配置；数据库会话设置为只读，没有读取订单或用户明细。

## 凭证保护与离线检查

生产配置和微信客户端证书以AES-256-GCM加密保存到 `.secrets/imported-channels.enc.json`。密钥通过本机 `GOEPAY_MASTER_KEY` 经HKDF-SHA256派生，认证上下文为 `goepay:imported-channels:v1`。私钥、API密钥和证书内容不写入源码、报告、样本和控制台输出。

离线检查结果：支付宝应用私钥及支付宝公钥均可解析为2048位RSA；微信APIv2密钥长度为32字节，客户端证书与私钥匹配，证书有效期2026-10-02至2031-10-01。**格式和匹配检查不证明上游产品权限或线上支付成功。**

SSH客户端核对并固定了本次首次读取的主机密钥指纹；临时连接密码文件已删除。`reference/`、`.secrets/`、`.tools/` 均加入忽略规则。本机主密钥丢失后无法解密导入文件，不能在原数据仍需使用时重置主密钥。

## 已新增Go代码

- `internal/channelconfig`：加密导入解密、离线校验、按实际apptype构建通道、mTLS配置和默认关闭的真实写操作保护。
- `internal/adapters/payment`：微信APIv2 Native/JSAPI、支付宝Page/WAP、查单/关单/退款/退款查询、回调校验；JSAPI返回完整二次签名材料。
- `internal/platform/httpclient.NewMutualTLS`：保持URL/DNS/重定向防护并装载客户端证书。
- `db/migrations/000003_provider_attempts.up.sql` 与 `internal/payment/provider_attempt.go`：先持久化调用权，24路并发仅一个调用者；相同配置复用，不明结果不盲目重试。
- `internal/payment/ProviderFlow`：事务外调用通道、保存支付材料或unknown、已验签回调幂等入账。支付宝通知成功态归一后才进入统一资金流程。
- CLI `check-channels`：只读离线检查与能力摘要，不执行上游请求，不需要数据库。

检查命令（先安全载入本机主密钥环境变量）：

```powershell
go run ./cmd/cli check-channels
```

输出仅包含通道ID、插件、方法和能力声明，并明确 `live_operations_enabled=false`。

## 实测和仍未完成的接入

测试使用临时测试密钥和本地模拟上游；原PHP供应商SDK的MD5/HMAC/RSA2互操作在禁网Docker执行。已测签名、响应原始JSON验签、回调错误身份/金额、重定向阻止、XML歧义、未知结果、并发调用权、重复回调及账本效果。生产配置只用于上述离线校验。

本机API和Worker镜像已重建并执行migration3；公开支付HTTP路由当前仍保持沙箱逻辑，尚未装载这些真实凭证执行支付。还需完成通道绑定和费率快照、公开回调入口及ACK、收银台展示、查证任务、退款任务接线和真实渠道权限验收。此处不能标记“已经能生产收款”或“全阶段完成”。
