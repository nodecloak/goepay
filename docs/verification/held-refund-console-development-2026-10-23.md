# 冻结订单退款：后台接入专项记录

此记录为持续开发证据，不是63/64最终候选全库验收。目录为 `E:\GOEPAY`，只使用本地合成凭据、隔离 PostgreSQL、真实本地 TLS 和本地浏览器，没有生产访问或真实交易。

后台新增 `POST /console/v1/admin/orders/{trade}/adopt-income-hold`，严格接受原 `hold_id` 和 `external_no`。只允许本人超级管理员，拒代登录；即使全局资金确认关闭，也需要原会话的逐动作资金密码授权。采用、授权消费与审计由核心在同一事务完成，原号重放只读取原结果。授权快照包含原冻结来源、历史证明和旧池守恒数据，不能由客户端声明可信金额。

`POST /console/v1/{role}/orders/{trade}/preview-refund` 只允许所属商户或管理员，CSRF、严格JSON并拒绝query。预览展示本订单冻结款、商户可用余额、平台手续费拆分，不创建退款、不移动资金、不调用支付机构、不回显HMAC。实际预留仍由核心锁定原订单及资金后重算。

解冻授权改用同一个数据库读取器下经过真实HMAC验证的剩余款与退款历史快照；任何关联退款仍pending/unknown时不展示可解冻，即使该笔只占手续费、冻结预留额为0。界面确认解冻只显示原批次剩余本金；历史来源迁移网络响应丢失时保留原批次和原请求号，不换号自动重发。

冻结订单详情只有服务端明确标识专用来源已核实，才开放退款。冻结退款先展示来源，修改金额或请求号会清除旧预览；客户端以整数分核对 `held+available+fee=本次退款额`，不会用浮点计算。原未准备历史冻结款仍不能绕过来源核对。

实际运行的证据：

- 资金目标与必需字段：Go测试先因缺强制授权定义编译失败；前端精确动作拦截实际断言失败，补齐后均通过。日志 `.tools/63-adopt-funds-{go,web}-{red,green}.log`。
- 组件真实红→绿：原解冻确认错误显示1000而非余款600，且缺历史迁移按钮；修复后通过保留原号重试与商户拒绝等测试。日志 `.tools/63-income-ui-{red,green}.log`。
- 真实PG/HTTP预览：原入口501，新增后成功从原1000冻结款预览400；断言退款表仍0、journal数量不变且内部hash不回显。日志 `.tools/63-refund-preview-{red,green}.log`，绿色5.054s。
- 资金/冻结/预览相关完整契约筛选通过24.779s，`.tools/63-funds-contract-regression.log`。不能据此宣称整个contract包已复测。
- 实际Chromium模拟HTTP流程先因旧冻结订单入口没有进入退款窗失败；修复后退款来源预览、改金额重新核对、保留原冻结/解冻请求号与代登录等5项通过4.9s，`.tools/63-income-browser-next.log`。
- 新客户端整数守恒/资金授权/冻结组件共10项通过，类型检查通过；当前未声明整个前端候选回归已完成。

CLI已增加 `invalid_order_income_fundings`、`invalid_refund_fundings` 非零退出；恢复脚本增加原hold及refund的父记录结构校验，并继续对本次GUID恢复库执行完整HMAC/账本CLI。当前仅语法验证完成，实际63恢复与CLI仍待最终候选部署后执行。OpenAPI已描述新增两条路径与强制授权条件；64接口后续独立补齐。

核心资金生命周期、历史真升级、并发、锁等待、损坏HMAC和出生版本降级的独立证据由核心/审查记录提供。本文件不替代这些证明，也不替代最终固定源码的 Windows/Linux 全库与本地Docker验收。

2026-10-24 接续核验：核心29文件与 `.tools/checkpoints/63-income-core-evidence-b3a1756c-4a4f-4afd-bc05-3ce0b3116a09/SOURCE_HASHES.json` 逐项SHA一致。核心相关财务回归原日志 `.tools/63-financial-regression-final.log` 已结束，通过508.796s；独审完整HTTP合同重跑日志 `63-income-review-cd3e8dcc-9305-48cb-ad9e-9c062b7e5452/final-full-contract-retry.log` 已结束，通过411.739s。这两项针对当时源码；后续身份64及共享接线已固定为853文件的新候选，正在重新运行完整两平台回归，不将先前结果冒充新候选通过。
