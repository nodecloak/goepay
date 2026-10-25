# 原独立结算批次84（schema83）

已核对本地release `admin/ajax_settle.php:52..70`，Pro `route/admin.php:315..319`，以及此前逐项读取的Pro `settle-C3-SgkfR.js`、`index-5nUERs6a.js`；不访问生产或实际资金服务。当前`web/src/workflows.ts`把`/settle/batch`映射为普通settlements审批，仓库没有settlement_batches域表；普通transfer-batches属于另一业务，不能替代。

## 原语义与资金边界

原批次有`batch/allmoney/count/time/status`，allmoney累计realmoney净额；创建将待结算记录归组，批次成员仍逐条转账。Pro有batchList/create_batch/deleteBatch/complete_batch/batchTransferList与四类导出。Release complete_batch直接改settle状态并不证明实际付款。

新批次保留这些字段及独立菜单。创建仅对已有、具备可信原结算预留及费用快照的申请归组，不再次冻结、不审批、不调用通道；待审核申请可以归组，付款仍必须先通过原审批。任意重复/跨批分配、旧缺来源证明、已释放/已付款申请不得创建新的付款归组。每申请永久绑定一个批次；删除采用审计归档，保留原成员及资金证明。`complete`只允许所有成员已有可信付款成功，不能凭点击把pending/unknown变成成功；银行卡费用暂存和后续退票仍按原生命周期展示，不发明银行最终性。

付款操作直接复用既有`SettlementPayoutPanel`和原`settlement-payouts/{request}`领域用例：按成员准备、核对、单次claim、独立逐动作资金授权、未知只查原号、未发送取消。批次页面不得自行建立普通Transfer、再次预留、绕过原收款快照/通道身份/费用/资金密码。服务端读取成员时重新验证原批次证明和原申请快照；页面上的归组或隐藏按钮不构成付款授权。

## 存储和接口

新增schema83，不改1..82迁移。三表：`settlement_batches`固定批次头、`settlement_batch_items`固定成员、`settlement_batch_events`固定create/archive/complete审计事件。按独立用途HMAC绑定批次号、稳定external_no、创建者、原业务时间、成员顺序/原申请/毛费净/原资金与受款摘要；不复制或落明文受款账号。批次号使用UTC+8日期前缀及唯一序列后缀，不受原三位随机数日容量限制，仍为不透明字符串字段。

创建1..100个规范申请ID，严格重复键/重复ID/上限，按商户再申请确定性锁序；稳定external_no重放仅返回原批次，内容变化409。创建同事务锁原申请、核验scope、插入头/成员/原审计；任何失败整批回滚，不触发外部调用。成员摘要不包含后续可变的审核状态、付款状态、账户余额或当前收款设置，正常审批/付款/退票不得损坏历史归组。

管理员本人super/operator可看列表和成员，归组/归档按当前业务权限、CSRF和原主体审计执行；敏感受款文件导出按super及既有受款权限核对。商户不能通过批次ID跨户读取。接口采用`/console/v1/admin/settlement-batches`及`/{id}`、`/{id}/archive`、`/{id}/complete`、`/{id}/export`；成员付款继续调用原受保护接口，避免为批次引入第二套出款授权。列表支持batch/status规范过滤和游标分页，成员返回原申请ID、uid、毛/费/净、类型、当前真实状态及受款掩码。

四格式已核对本地`epay_release/admin/download.php:38..113`：common（内部保留summary别名）/wxpay使用UTF-8 BOM，alipay/mybank使用GBK无BOM，均CRLF；字段列顺序、原净额、默认transfer_desc及微信原AppID按原明文实现。任何导出只读取原成员/原金额/原受款快照，限制100条/1MiB，拒绝CSV公式、控制字符及隐藏格式字符，不以当前资料覆盖历史受款。缺少可信原受款资料或无法无损GBK编码明确报错，不补成空值/0，不改写金融收款人；独立导出审计提交后才返回文件，审计仅格式/批次条数/文件SHA256，不含收款资料。

CLI新增invalid_settlement_batches计数，验证头/成员/事件/原审计/孤立记录及原来源；预计32项、public200表，须以实际冻结候选证明。96的1370文件全库/race正在独立运行，84结果不得冒用其测试结论。

## 验收

真实随机PG schema：净额汇总、稳定幂等、并发同请求只能归一批、归组资金零变化、普通代付/跨域ID拒绝、篡改/缺源识别、审批及付款后历史仍有效、unknown不能完成、归档不删原申请或恢复发送权。复用本地TLS钱包/银行和原逐动作授权测试验证成员出款，无真实交易。后台375/1280原字段和独立路由、待审核与已批准动作、错误/未知状态/重复点击/分页/安全导出与无越权。最终完整候选两平台全库/race、PHP/前端/扫描、本地无重置升级和全表恢复。
