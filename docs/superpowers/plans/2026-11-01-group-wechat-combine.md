# 微信合单有效用户组覆盖

- 对照本地 Pro `app/common.php:618` 和 Release `admin/gedit.php:389-410`：`wxcombine_open`、`wxcombine_minmoney`、`wxcombine_submoney`，空/`_` 继承，显式零有效，金额精确到分。
- 新增 `merchantpolicy/combine.go` 共用类型化读取和全局记录存在性快照；坏组配置拒绝首次发送，全局旧缺省/解析行为保持现有 SplitCombine207 兼容。
- 新增 `backoffice/group_combine_settings.go` 注册三字段并验证，组保存同一事务；`service.go` 验证 hook 由根代理挂载。
- `payment/combine_suborders207.go` 首次合单准备先无锁读取既有不可变拆单；新拆单按 merchant→membership/group→order 锁顺序重读历史、认证订单金额、复用 SplitCombine207、记录子单、复核会员期限及全局缺省首次插入。
- 原始 provider attempt 的 ownership、未知结果和原号重放保持现有路径；不因当前组变化重算已记录拆单。非 CombineCreator 不读组合单配置。所有外部 Create/CombineCreate 都在准备事务提交之后。
- 行为回归先红：真实 ProviderFlow 假通道证明组开关/最小金额/子单上限、组0、VIP、坏值、原号重放；额外订单锁等待证明会员到期与首次全局配置插入都无外发、无子单残留。
- 绿测包含现有 TLS 微信合单创建→签名/加密通知→子单退款流程、不可变与金额守恒测试；唯一现有合成 PostgreSQL runner 串行复用，不访问生产、不发真实交易。
