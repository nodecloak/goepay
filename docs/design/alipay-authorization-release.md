# 支付宝授权解除（迁移 25）

此增量为迁移 23 的预授权增加全额解冻业务入口。范围是本地明文 `Payment.php::alipayUnfreeze` 和 `AlipayTradeService::preAuthUnfreeze/preAuthQuery` 已知协议；没有读取真实配置、生产访问或真实资金操作。撤销 API 仍只有适配器，没有开放业务入口；部分已扣款授权、部分解冻不在本次范围。

## 资金与互斥

买家授权不是平台商户余额。成功解冻只更新授权解除状态并关闭未支付订单，绝不新增商户余额、退款、预占返还或资金账本分录。晚到的真实支付成功仍可通过统一不可变 `PaidReceipt` 入账，释放成功不会封死实际支付恢复。

每个订单只有一条 `payment_authorization_releases`，业务请求号固定为 `UA{platform_order_id}`。订单、通道、授权号、金额和请求号不可修改；不能改号重建意图。创建解除意图与核对任务后立即阻止后续自动 capture，事务先锁订单再锁授权；迁移 25 的数据库触发器同时拒绝在解除意图后重新进入 capture processing。即使解除失败，也不会违背操作者的解除请求重新自动扣款。

只有已签名查单确认原 `FREEZE` 操作、相同订单/请求号/授权号/买家、全额金额、`AUTHORIZED` 且明确 `rest_amount` 等于原金额，才可能首次发出 unfreeze。交易查询还必须确认没有已成功支付：已认证的明确交易不存在只适用于本地 capture 从未发送或已明确失败；本地 capture processing/unknown 还需要已认证 `TRADE_CLOSED` 终态，不能靠交易不存在或剩余额全额猜测在途扣款失败。证据不足保持 `reversal_pending`。查到实际支付成功则先统一支付入账，解除请求失败，不伪造解冻或退款。

## 意图状态

| 状态 | 含义与允许动作 |
|---|---|
| checking | 已保存解除意图，正在只读预校验；capture 已被阻止 |
| reversal_pending，sent=false | 尚未发送，证据不足或预校验失败；只读查询不会解冻；显式再次请求可用同一 UA 重新校验后竞争首次发送权 |
| processing，sent=true | 首次发送边界已提交；即便进程在 I/O 前崩溃，也不重新发送 |
| unknown，sent=true | 发送或验签/持久化/确认结果未知；只查询原 UA |
| succeeded | 原 UA 的 UNFREEZE、金额与授权号已确认，且明确剩余额为零；关闭未付订单 |
| failed | 明确认证拒绝，或核对确认实际已支付；不重新发送、不增加商户余额 |

`MarkAuthorizationReleaseSent` 在订单锁下原子执行 `WHERE sent=false AND state='checking'`，并重新检查订单未付和 capture 未成功，只有一个调用方能取得发送权。`sent=true` 不能回退。首次只读校验失败不能冒充未知外部写：`sent=false` 的显式后续操作仍能得到第一次发送机会。

同步 unfreeze 成功响应也不会单独把授权标记 released；随后查询原 UA，要求 `operation_type=UNFREEZE`、`status=SUCCESS`、精确订单/请求号/授权号/金额及明确 `rest_amount=0`。缺失剩余额、操作类型不符、notfound、网络错误、坏签名均保留待核实。`authorization.release.reconcile` Worker 只调用 `QueryAuthorizationRelease`，不取得发送权，也不发 unfreeze/cancel。

## 后台接口

共享会话、管理员权限与 CSRF 检查后，仅允许订单所属商户或有权管理员访问。两接口均为 POST，JSON body 为 `{}`，金额和商户身份从订单读取：

- `/console/v1/{admin|merchant}/orders/{trade_no}/release-authorization`：发起解除或核对原记录。
- `/console/v1/{admin|merchant}/orders/{trade_no}/query-authorization-release`：只读核对。

外部 I/O 前必须成功保存操作者 intent audit；审计失败阻断连只读上游请求在内的所有 I/O。结果使用独立短超时上下文写 audit；数据库故障导致结果审计失败会记录服务错误日志，原持久资金状态不会因此被撤销或重发。审计仅含平台订单/商户/UA/状态及 sent，不记录授权号、买家身份或密钥。

后台详情包含 `authorization.release_state`、`authorization_release`（`state/sent/request_no/amount` 等）和 `can_release_authorization`。HTTP 202/code 0 表示持久意图待核实；200 的 `succeeded/failed` 仍需按状态解释；503 为停写/能力不可用，409 为状态冲突。外部写开关同时约束 Flow 与配置包装；停写不影响原 UA 只读核对。

成功解除后商户 query/list 不再返回“已授权待扣款”的状态 4，也不再把授权号当作支付流水；真实支付后仍优先返回可信支付流水。普通 close 始终不能代替解除授权。
