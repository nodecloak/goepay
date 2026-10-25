# 支付宝人工全额扣取授权

本入口补充原明文 `epay_pro/app/lib/Payment.php::alipayPreAuthPay` 的后台操作：只读查单恢复的完整未消费授权不会自动扣款，操作者可以明确提交第一次全额扣取。仅实现 `capture_state=none` 且 `release_state=none` 的完整授权，不提供部分金额、换订单号重试或多次扣取。

## 入口与结果

订单详情提供 `can_capture_authorization`。这是纯本地状态、产品能力、当前商户/渠道权限及写开关判断，不向上游发请求。页面不能仅凭状态4展示可扣取按钮。

`POST /console/v1/{admin|merchant}/orders/{trade}/capture-authorization`，JSON `{}`，复用控制台登录、管理员权限、商户归属与CSRF。金额、渠道、买家、授权号和付款请求号均取自原不可变订单及授权；接口不接受这些字段覆盖。

| HTTP | 含义 | 后续动作 |
| --- | --- | --- |
| 200，code0，capture_state=succeeded | 已确认扣取结果，或独立可信查单发现此前已真实付款 | 刷新原订单 |
| 202，code0，capture_state=processing/unknown | 原唯一请求可能已经被受理 | 只查询原订单 |
| 409，code-1 | 原发送意图已存在、失败、释放冲突或状态不再允许首次扣取 | 不重发扣取，按原单查单/释放能力处理 |
| 503，code-1 | 停写、前置证据/产品不可用或审计故障 | 刷新原单；不能据此断言未扣款 |

恢复未知结果继续使用原有订单 `POST .../orders/{trade}/query`，不能重新调用capture换请求号。失败也不能重新领取发送权。若前置只读核对失败且授权仍为none，尚未领取发送意图，后续一次明确操作可以重新核对。

## 资金与权限约束

人工操作必须先有可信持久授权及原preauth attempt。第一条新鲜交易查询只有精确、已认证的 `40004:ACQ.TRADE_NOT_EXIST` 才能进入下一步；网络、验签错误、其它notfound、WAIT_BUYER_PAY或closed交易均不能代替。独立查询若确认真实付款，则核对订单、金额、人民币、支付流水和相同buyer后走统一receipt恢复，不再扣取。

第二条新鲜授权查询必须同时匹配原订单/请求号、授权号、buyer、金额，且 `SUCCESS`、`AUTHORIZED`、`FREEZE`、rest_amount已知并恰等于原全额。缺字段、部分剩余额、FINISH或不同操作均拒绝。

随后与自动通知共用 `BeginPreauthorizationCapture`，先锁订单/授权，将capture改成processing并提交只读核对任务，再允许上游写。release状态任何非none都不能领取capture；capture已领取时解除授权的发送边界也会阻断不确定扣取，二者不能同时发送。该processing本身就是发送屏障，进程即使崩在实际I/O之前也不能自动重发。

上游使用原tradeNo、原auth_no、原全额和 `auth_confirm_mode=COMPLETE`；产品码沿现有适配器，不额外制造资金语义。`preauth_capture_settlement.go` 为自动通知和人工操作共用发送后处理：相同订单/全额/CNY/buyer/成功状态/真实支付流水全部核对后才进入统一 `PaidReceipt`/markPaid。授权号永远不能当支付流水。响应未知或证据不匹配保存unknown，明确业务拒绝保存failed，均不重新发送。自动通知只对已持久的provider失败沿用原ACK语义，数据库失败不能ACK成功。

操作者intent审计必须成功写入后才允许任何provider查询或扣取。查询后、领取发送权后紧邻写请求前，再次核对当前AllowLive、非simulation/sandbox、HTTPS origin、Alipay preauth+Query能力、商户enabled、商户渠道白名单、原授权release状态及现有支付风控。风控使用可信buyer、已有原clientIP（若历史订单已保存）、原通知/返回域名；不会把operator当买家。领取后权限失效则永久记failed且不发送。

结果审计同步写入，失败返回503并保留已确认资金状态；不能为修复审计而回退真实支付或恢复capture发送权。前端需查询原单。审计只记操作者、原订单、金额与结果，不记录私钥/通道secret/raw provider body。

## 范围

本地已覆盖真实PG并发、释放竞态和真实TLS临时签名响应。没有真实授权、真实设备、生产密钥、上游产品权限或真实资金验证。原PHP动作可使用剩余金额，本次按明确需求只开放未消费完整授权；不会把部分授权误当全额。复用迁移23/25及账本，无新增迁移，冻结SQL不变。
