# 44/45 独立代码审阅记录（2026-10-23）

审阅方式：只读对照 43-final 检查点与 45 候选增量，核对 `transfer-account-binding.md`、`settlement-bank-payout.md` 及相关实现。未访问生产或外部支付，未读取 `.env`、`.secrets` 或 reference 导入密钥。本次仅写入本记录。

已审阅普通付款、批次与结算的付款账户 HMAC 绑定、发送领取屏障、原号查询与凭证；银行卡结算使用原申请 gross 专属预留，成功结转 net、保留结算 fee，以及微信 BANK_FAIL／支付宝 REFUND 各自证据和重复退票路径；同时查看相关迁移、后台资金授权快照、报表和前端状态呈现。未发现另外可确认的 P1/P2。

发现并复核关闭的 P2：45 银行资金不变量使用 INNER JOIN 依赖 available／clearing 账户，依赖身份损坏时整条资金投影可能消失；旧 `WHERE valid IS NOT TRUE` 计数遗漏这类记录，导致结构检查和恢复校验误报零异常。该问题不直接绕过付款事务的失败关闭检查。

修复复核：`internal/payment/ledger_check.go` 与 `scripts/restore-test.ps1` 现在均从每条 `settlement_fundings` 出发，检查是否存在同一 `request_id` 且 `valid IS TRUE` 的投影。因此，投影缺行、false 和 NULL 均会计入异常；原有孤立 settlement_reserve 账户检查仍保留。此修复在代码层面覆盖了报告的 P2，未修改已部署迁移的内容或 checksum。

回归用例 `internal/payment/settlement_bank_integrity_test.go` 保留账户 ID、余额和账本 FK，仅变更 clearing.kind，检查普通余额及流水仍有效而 `InvalidSettlementFundings=1`，与缺陷触发条件一致。独立审阅者未执行该用例、恢复脚本或全库／race 测试，也未观察这些测试的最终输出；运行结果由主任务另行记录。本记录不代表全部功能或部署验收完成。
