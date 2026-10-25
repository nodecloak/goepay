# 持久批量代付

迁移37增加不可变批次、逐项发送边界、事件、未发送取消证明。管理员和商户后台新增独立“批量付款”入口；不把它等同自动结算或原加密Pro批次的全部语义。

`POST /console/v1/{role}/transfer-batches` 一批1–100项，传 `external_no` 和 `items[{channel_id,type,account,name,amount,remark,bank_code}]`；金额为元字符串，管理员另传 `merchant_id`。同商户原请求号绑定规范化后有序整份请求的域分隔HMAC，包括通道、AppID、产品和收款身份。改变顺序、金额或身份冲突。收款详情继续用已有AEAD加密意图，批次响应和审计不返回原账号姓名。

准备阶段复用单笔代付事务函数，执行商户/平台开关、产品、手续费、单笔及收款人日限额和T+1校验。全部本金和费用一次性预留；任一失败或审计失败则整批回滚。此阶段没有上游I/O，不创建可提前查询的Worker任务。默认真实通道guard仍要求产品已启用；Prepare本身不提供上游发送权限。

读取 `GET /transfer-batches?external_no=...` 可恢复丢失的准备响应；管理员必须给merchant_id。列表默认20、最大100，用before_id游标；`GET /transfer-batches/{id}` 读原批次。页面离开清除收款资料，不把其写入LocalStorage或URL；未知结果保留原请求号，先读取原记录。

`POST /transfer-batches/{id}/execute {limit:1..100}` 才发送。每项按商户→原转账→批次项顺序加锁，重新检查开关、白名单、原产品和AppID。`unsent→claimed`、真实操作者审计及只读核对任务在同事务提交，随后使用单笔代付共用发送/回执/银行账务函数。重复或并发执行只能领取尚未发送项。已claimed、未知、明确失败均不能再次发送；剩余unsent可继续。HTTP执行预算15秒；客户端断连或中断后读取原批次，不能把HTTP失败视作未付款。200也不能代表整批成功。

`POST .../query {}` 仅核对已claimed的原TF编号。未发送/已取消项即使从单笔查询入口访问也只回本地；已发送项在通道重配为其它AppID后拒绝向新应用查询。Worker只读核对，不领取首次发送权。领取提交后进程在外呼前崩溃，因不能证明是否外呼，保持原冻结并人工核对，不能自动再发送。

`POST .../cancel-unsent {}` 在相同锁序下只取消仍unsent项；与执行竞争只有一个胜者。取消证明、资金释放、事件及审计同事务。银行卡走独立 `cancelled` 生命周期和不可变未发送证明，既不假造BANK_FAIL/REFUND，也不创建银行观察证据。已claimed或未知项不释放。停写后仍允许读原批次、只读核对和取消未发送项。

列表、详情及CSV明确区分 `unsent/cancelled` 与历史账本pending/failed；批次summary逐项汇总当前有效银行退票状态。后续已补原五列CSV/XLSX文件校验预览和显式应用草稿，见 `verification/transfer-batch-import-2026-10-23.md`；XLS仍不支持，上传不预留或付款。自动定时发送、跨商户结算申请批次、红包或账簿产品另行实施；单结算申请原资金出款绑定已见 `settlement-wallet-payout.md`、`settlement-bank-payout.md`。不宣称Pro原批次原子性和所有错误契约完全相同。

专项覆盖原子预留、费用及加密身份、幂等顺序/身份、12并发一次发送、未知不重发及原号查询恢复、停止/撤权/AppID变更、取消竞争、银行未发送证明、审计失败回滚、归属/CSRF、单笔与列表投影；实际TLS支付宝请求验证签名/原收款/金额、先审计再外呼、503未知及原TF查证。全量结果另见测试记录。
