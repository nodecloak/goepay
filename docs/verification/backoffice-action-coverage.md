# 后台动作覆盖核对

核对日期：2026-10-23。范围为本机源码和现有实现；没有生产访问、真实资金操作或读取 `.secrets`。这份文档记录初期只读核对及父任务随后授权的最低结算金额/邮箱注册恢复增量证据，不是“所有阶段完成”声明，也不以菜单数、页面数或 CRUD 数计算功能覆盖率。

## 证据口径

- `docs/baseline/routes.csv` 有管理员 344 条、用户后台 173 条，共 **517 条后台静态路由**。其中路由清单标记 503 条控制器加密、14 条明文；14 条明文均属于用户 `ProfitSharing`。API 22 条和 app 33 条不计入这 517 条。
- 加密标记针对入口控制器；部分逻辑、服务、Release 版本和付款插件仍可读。路由存在只能证明动作入口存在，不能证明所有参数、权限、状态规则或响应已经恢复。Release 的可读行为也不能直接证明与较新 Pro 完全相同。
- 本文按动作族对照，517 条逐条原始入口仍以 CSV 为索引。没有逐条完成独立契约验证的入口，不因同族已有页面而计作完成。
- 新后台使用 `/console/v1/{admin|merchant}`（下文记作 `C`），并非原 PHP 后台 RPC 路径的逐字兼容服务器。支付商户 API 的兼容验收是另一份契约，不应与后台传输协议混称。
- 实现和测试文件是可复核依据；本次只读核对没有重新执行全量测试。后续修复应另附实际执行结果。
- `reference/server-pro` 目前提供 schema/channel 清单及支付宝、微信明文付款插件，没有可用于恢复全部注册、结算、后台控制器规则的额外明文服务端。

## 可证实的实现与边界

| 原动作族/路由索引 | 当前实际路径或能力 | 实现/界面证据 | 已知边界 |
| --- | --- | --- | --- |
| ADMIN-R001..007；USER-R007..009、018 | 登录、会话检查、退出、持久会话失效、限流及 TOTP | `internal/server/server.go`、`security.go`、`login_throttle.go`；`web/src/App.vue`、`TotpSettings.vue` | 不是自助注册、找回密码或平台登录绑定。 |
| ADMIN-R264..281、318..326 | 管理员创建/编辑/停用商户和后台管理员，角色授权、可选商户登录凭据、审计 | `internal/backoffice/special.go`、`merchant_login.go`、`schema.go`；`ResourcePage.vue` | 商户/管理员删除以停用替代；没有恢复原全部设置键或支付二级密码。 |
| USER-R129..157 的部分账户动作 | `C/account/profile`、`api-info`、`password`、`api-key/rotate`；当前密码校验、加密密钥、会话失效 | `internal/server/account.go`；账户设置界面 | 仅已登录用户；资料字段、绑定、订阅、语音/打印、机器人设置不因账户页存在而全部完成。 |
| ADMIN-R130..142；USER-R057..059 | 用户组和邀请码资料，余额购买会员、有效期与独立管理员基线分组 | `backoffice/schema.go`、`business.go`；`payment/membership_deposit.go` 及基线恢复相关文件；`CommercePage.vue` | 不是邀请返佣、所有阶梯价格或原版全部组扩展规则。 |
| ADMIN-R143..169；USER-R093..107 | 订单/退款列表、详情、查单、关闭、持久退款意图、未知结果保留冻结、核对任务 | `server/order_operations.go`、`payment/provider_flow_operations.go`；`OrderDetails.vue` | 有证据才改变支付结果；没有随意修改真实流水、付费状态或删除账务的按钮。 |
| ADMIN-R158；USER-R099 | 已支付订单重发通知，稳定请求编号、运行任务合并、冷却及审计 | `server/notification_resend.go`、`notification/handler.go`、`platform/jobs`；`NotificationDeliveryPanel.vue` | root 的 32 增量已补；通知目标取保存的订单 URL，不能由按钮任意覆盖。不是短信邮件通知。 |
| ADMIN-R162..163 | 支付宝预授权扣款、查询扣款、持久解冻意图与只读查解冻 | `server/preauth_capture_console.go`、`preauth_release_console.go`；`OrderDetails.vue` | 仅绑定的支付宝授权身份；**不是已支付订单收入冻结/解冻**。 |
| ADMIN-R244..263；USER-R113..123 的单笔部分 | `C/transfers`、`capabilities`、`/{id}`、`/{id}/query`、`/{id}/proof` | `server/transfer_http.go`、`payment/provider_transfers.go`；`TransferPage.vue` | 微信零钱/银行、支付宝余额/证书银行按明确能力启用；未知不重发；银行晚退票走各自证据闭环。没有原批量提交、红红包代付等全部动作。 |
| ADMIN-R282..284、USER-R124..126 | 站内转账申请、资金冻结、管理员批准/拒绝、接收方账本入账 | `payment/ledger_requests.go`、`backoffice/workflows.go`；`WorkflowPage.vue` | 余额、T+1、收付双方状态和幂等统一约束；不是对外代付。 |
| ADMIN-R220..225 | 本地内部分账规则、基于已付订单执行、分账上限、冻结和审批 | `backoffice/profitsharing.go`、`payment/profit_rules.go` | **不是支付宝/微信渠道分账接口**，不应混算。 |
| ADMIN-R226..243；USER-R108..112 的申请/审批部分 | `C/workflows/settlements` 创建、列表、批准/拒绝；最低金额及后续政策快照 | `backoffice/workflows.go`、`payment/ledger_requests.go`、`settlement_terms.go`；`WorkflowPage.vue` | 批准后钱仍冻结；接口明确 `external_payout=false`。39正在补手续费/留存/日次数与确认快照，尚未统一部署验收；没有实际银行结算、批次生成或自动结算。 |
| USER-R088..092 | 支持的浏览器支付场景充值，可信付款回调入账；保证金持有、申请释放/审批 | `backoffice/business.go`、`payment` 的充值/保证金实现；`CommercePage.vue` | 本地预校验先于建单；不以未知或仅生成付款材料当作充值成功。 |
| ADMIN-R313..317、097..103 等 | 商户域名、买家/IP/CIDR 黑名单及明确配置的支付策略实际执行 | `backoffice/policy.go`、`cases.go`；资源页 | `risk-records` 等资料不代表自动风控模型；未配置策略默认不推断原隐藏规则。 |
| ADMIN-R060..129；USER-R019..054、063..073 的本地资料部分 | 入网/投诉本地表单、状态转换、不可覆盖历史评论、订单归属、审计 | `backoffice/cases.go`、`extended.go`；`CasePanel.vue` | 没有据此声称已上传厂商资料、签约、同步远端投诉、完成渠道退款进度或自动投诉任务。 |
| ADMIN-R167..169、298..301 等统计动作 | `C/reports/{daily,merchants,channels,provinces,buyers,transfer-accounts,transfer-channels,transfer-merchants,complaints}` | `server/reports.go`；`ReportPage.vue` | 页面 CSV 是当前页导出，不是原全部筛选结果导出。未逐一证明与原所有统计口径相同。 |
| ADMIN-R017..045、170..204、302..312 中资料动作 | 受限 schema 的设置/公告/渠道/支付类型/应用/收款码样式 CRUD；真实收款链接创建支付 | `backoffice/schema.go`、`extended.go`、`server/collection.go`；`ResourcePage.vue` | 通用资料保存不等于配置已被运行时消费、文件上传、远端测试或安装执行。 |
| ADMIN-R046..050、318..328 的运行维护部分 | 登录/操作日志、已知任务列表和白名单重试、系统信息、到期会话/限流数据清理 | `server/operations.go`、`cmd/worker/main.go`；`OperationsPage.vue` | worker 只处理明确通知/支付/退款/代付/银行/授权核对任务；保存 cron 资料不等于执行任意 cron。没有删除财务流水。 |

上述界面文件在 `web/src` 下；可能位于其 `components` 或其他子目录。接口可访问仍需正常认证、角色权限、商户启用检查及写操作 CSRF，并不向未登录用户开放。

## 明确不能计作完成的动作

以下包含同支付宝/微信插件内的缺口。用户“其它插件先不做”不能扩展解释为这些动作全部暂缓。

| 缺口 | 原版可读证据/静态入口 | 当前差异、下一步约束 |
| --- | --- | --- |
| 自助注册、短信/邮件验证码、付费注册、找回密码 | USER-R015..017；`epay_release/user/ajax.php:213` sendcode、244 reg、314 注册支付建单、337 免费注册、363 sendcode2、394 findpwd；`includes/lib/VerifyCode.php` | 初期只有管理员创建商户/已登录改密。随后36增量已补邮箱证明注册/恢复及 `registration_open` 实际消费，见 `public-enrollment-36-freeze.md`。运行默认Sender未配置，发码503，不能算真实邮件送达完成；旧商户邮箱绑定、短信、付费注册/审核和隐藏Pro细则仍有缺口。 |
| 管理员 SSO/以商户身份进入 | ADMIN-R344；`epay_release/admin/sso.php` 明文校验管理员、查商户、签发商户 cookie 并跳转 | 35增量已完成并独立关闭：super管理员60秒单次授权、同一发行会话消费、15分钟商户会话、发行者撤权/退出关联失效、真实管理员审计；实际PG锁等待跨过期回归通过。比Release收紧权限与寿命；原Pro隐藏权限细则未证明相同。见 `design/audited-sso.md`。 |
| 已付订单收入冻结/解冻 | ADMIN-R156..157；`epay_release/includes/lib/Order.php:6` 从商户扣 getmoney、置冻结状态，23 恢复；`admin/ajax_order.php:352`、357 | 33增量已完成并独立关闭：按原支付及成功退款双边凭证核实剩余净收入、独立持有账户/周期、冻结幂等号与原hold解冻、资金和审计同事务；对外投影status3、T+1不重复受限。当前held期间新增退款被安全阻断，仍有与Release直接冻结退款分配行为的差异。见 `design/order-income-holds.md`。 |
| 真实手工补单 | ADMIN-R160；`epay_release/admin/ajax_order.php:379` 直接置已付并 processOrder | 已验证查单能补齐真实渠道成功，本地 sandbox-paid 仅模拟；没有生产手工记已付功能。任何新增须有渠道凭证或明确受控账务调整，不能复制直接造收入。 |
| 批量代付 | ADMIN-R260、USER-R123；`epay_release/admin/ajax_transfer.php:198` batch_submit | 37增量补齐1–100项持久批次、整批事务预留、逐项单次claim发送、原号核对与未发送取消、费用及加密受款快照/审计、两后台页面。实际PG/TLS及本地Docker页面已验证。仍没有文件导入、红包/账簿或结算绑定；原Pro原子性和所有错误契约未证明一致。见 `design/durable-transfer-batches.md`。 |
| 结算生成批次、批量外部付款、自动结算/自动出款 | ADMIN-R236..243、R046..050；`epay_release/admin/ajax_settle.php:52` create_batch、67 complete_batch；`epay_release/cron.php:19` settle、351 auto_settle_money | `/settle/batch` 当前只映射普通结算审批；页面批选循环审批不是持久代付批次。worker 没有消费此自动结算配置。需结算受款账户不可变快照、资金预占与代付绑定、未知不重出、到款/退票和批次汇总。 |
| 结算费率、最低/最高费用、留存余额、日申请次数等完整规则 | `epay_release/user/apply.php:34..86`；`cron.php` | 当前 T+1 和申请冻结真实执行，但不能把全局支付手续费视为结算手续费，也未证明全部组级/商户级覆盖优先级。最低申请金额单项另行补齐，不代表其余规则完成。 |
| 轮询/权重运行时路由 | ADMIN-R191..197；`epay_release/includes/lib/Channel.php:361` 解析轮询、399 权重随机、404 更新游标、423 random_weight | 34增量已完成并独立关闭：global/merchant作用域、RR/weighted/first/priority、固定订单分配、通道与组金额/日/频率配额原子预占，集合及充值走浏览器产品白名单。未知不换通道、申请配额保守不返还；旧无scope规则不自动执行。见 `design/provider-policy-routing.md`。 |
| 订单名称替换规则及未落地的渠道额度/子通道策略 | ADMIN-R273、R289..297；`includes/functions.php:852` ordername_replace；`includes/lib/Plugin.php:58,143`；Pro `app/lib/Payment.php:186..209` | 38补齐super范围策略、六个字面变量、随机候选及非重叠精确金额规则，模板/规则独立按channel>merchant>global继承，首次不可变上游快照；原name/签名/通知不变。PG并发、四发送点及真实TLS验证通过。组级与Pro重叠规则优先级未恢复；额度路由见34，不把子通道资料等同完整策略。见 `design/upstream-subjects.md`。 |
| 短信、邮件渠道、测试邮件、消息订阅送达 | ADMIN-R031、R033..040；USER-R134..135、141..146；`VerifyCode.php`、`MsgNotice.php:8,170,239,275` | 当前 HTTP 商户付款通知完整度不能替代 SMTP/SMS/订阅通知。没有查到这些送达 worker 或 testMail 实现。QQ 机器人等外部插件可暂缓，但核心验证码/邮件行为不能一并当作其它插件。 |
| 支付宝/微信**渠道分账**的接收方、提交、查询、完结、回退 | ADMIN-R205..219；USER-R074..087 明文控制器；Pro `app/service/ProfitSharingService.php:231,367,380,393,406`；Release `includes/lib/ProfitSharing/Alipay.php`、`Wxpay.php` | 这些明确调用支付宝结算/微信分账服务，是同两支付插件能力。当前只有内部账本分账，不具备远端接收方身份、远端意图/查询/回退闭环。核对初期 `web/src/deferred.ts` 将此笼统归为其它插件，随后已更正为明确“尚未接入”，并说明站内分账不能替代。文案修正不等于分账实现。 |
| 远端投诉/进件全流程 | ADMIN-R060..129、USER-R019..054、063..073：submit/query/contract/upload/bank/status/feedback/complete/task 等 | 本地人工资料/评论和统计已实现；远端合同签约、银行卡校验、渠道入网、投诉同步/证据上传/自动答复没有逐动作闭环。属于同两插件的实际产品应逐项辨别，不能把全部进件/投诉都算其它插件。未具备官方产品权限时也不能伪造提交成功。 |
| 二级支付密码、部分密钥/绑定安全动作 | ADMIN-R011、R233..235；USER-R132、147..150 等 | 当前登录密码/TOTP/CSRF 是不同控制；没有原二级支付密码配置/验证，商户 RSA 密钥对生成或登录绑定流程也未全部恢复。具体较新 Pro 参数受加密限制。 |
| 全量导出、订单批量动作、打印、子订单 | ADMIN-R145、R151、R287；USER-R094、R097、R100 等 | 已补按当前筛选导出订单/退款/代付/账本及统计全部结果，复用租户与过滤，每次50,000行/8MiB，超限拒绝且审计，独立PG与前端mock通过。仍非原版无界导出、批量修改/删除、打印和子订单；财务删除被有意限制。见 `verification/csv-proxy-report-2026-10-23.md`。 |
| 自助邀请返佣、模板图片上传、收款码全部选项、商城更新器 | 对应 Invite、Template、Onecode、Updater、AppStore 路由 | 邀请码 CRUD、链接创建或元数据不是完整工作流。第三方商城/插件安装可按其它插件暂缓；同核心收款码/邀请业务仍须辨明规则。禁止为复刻 PHP 动态安装而执行未审查代码。 |

## 加密或缺样本导致不能确认的细节

1. Pro 注册/找回密码的验证码时效、失败次数、邀请码消耗时点、付费注册退款及 SSO 权限范围，不能只靠 Release 版本断言等价。
2. Pro 批量代付是否整批预占、允许何种上传格式、失败重试/撤销语义，结算费用/组配置优先级、自动结算时区和重复执行规则，需要可读实现、合法导出的契约或本地样本。
3. 路由名 `operation`、`op`、`setStatus`、`advanced` 等不能证明允许状态集；通用 CRUD 不应接受未经恢复的任意动作字符串。
4. 风控判定、轮询异常回退、单通道/子通道限额和订单名称规则的 Pro 扩展，没有样本不能承诺一一相同。
5. 已实现字段、远端产品方法和菜单入口都不是全规则恢复证据。后续逐动作验收至少要覆盖权限、输入、状态、重复/并发、未知上游结果及界面响应。

## 自主补齐建议及修复记录

优先补齐可由可读源码和现有资金模型明确证明、无需生产凭据的规则：最低结算金额执行；再对轮询、收入冻结、短时审计 SSO、注册验证码建立独立设计与红绿回归。批量代付/自动结算需更大的持久出款闭环，不能只包装单笔循环。渠道分账应作为同两支付插件的单独领域集成规划。

最低结算金额：核对时 `schema.go`/`service.go` 已允许整数分 `settlement_minimum_cents`，但 `RequestFunds` 尚未读取。原版 `user/apply.php:56..64` 明确要求申请额达到最低。父任务授权后已补齐：统一资金申请入口先检查历史幂等，再校验全局非负整数分（0..100000000000），最后按现有 T+1 额度冻结。缺配置保留最低 0；非法持久配置拒绝新申请；策略行共享锁持续到预占提交。管理员代提交没有豁免。配置调高不改变历史请求，金额不同的同号请求仍冲突。未新增迁移或共享 Server 挂钩。

真实 PostgreSQL 红测先复现：配置 101 分时商户申请 1.00 元仍返回 201 并冻结，而预期 422。修复后 2026-10-23 重新执行：

```text
go test ./internal/payment ./internal/backoffice ./tests/contract \
  -run 'TestSettlementMinimum|TestT1|TestInternalTransfersAndSettlementReservedApproval' -count=1
ok goepay/internal/payment   15.942s
ok goepay/internal/backoffice 3.197s
ok goepay/tests/contract      2.903s
go vet ./internal/payment ./internal/backoffice
exit 0
```

覆盖缺配置/零阈值、整数分边界、非法/溢出配置、低于阈值无冻结、商户与管理员 HTTP 提交、恰好达到阈值、配置改变后的幂等、20 并发请求一次预占、不影响内部转账/分账、与 T+1 同时生效，以及已有 T+1/审批回归。该证据关闭最低金额单项；33/34/35/36及CSV的后续状态见各行与独立专项，不据此关闭其它动作。
