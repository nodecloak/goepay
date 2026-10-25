# Pro控制台对齐与迁移恢复后的验证

这是连续实施记录，尚不是全部功能完成或最终部署声明。禁止访问生产环境或发起真实资金交易；本轮只使用本机合成商户、模拟通道、独立测试库和本地TLS夹具。

## 已确认的界面与查询增量

本地Pro资源证明管理员深色230/64px导航、商户浅色导航、#5D87FF主色，以及原卡片、表格、表单、页签和面包屑行为。实现与原文件SHA/偏移分别记录在design/pro-console-visual-baseline.md与design/pro-console-field-mapping.md。没有执行原bundle的外部请求代码。

订单增加通道ID、原接口单号、买家、IP、最新通知任务状态和成功累计退款的只读投影；详情同样返回reporting对象。只查询已存资料，不通过查单填列表。独立审查发现预授权buyer回退与筛选不一致，真实RED后补齐相同回退规则；包括CSV/tenant/filter共4项实际PostgreSQL契约通过，`.tools/pro-buyer-filter-green.log`，17.405秒。

商户列表/详情共用原商户、登录账户和账务余额投影：available_minor、frozen_minor、income_held_minor分别显示，可用和冻结口径保持Store.Balance原定义，订单冻结收入单独显示。注册时间只读。派生字段创建、修改均拒绝；普通名称修改不把投影复制到profile。新测试真实RED后3项专项通过12.111秒；完整backoffice包通过93.713秒，日志`.tools/pro-{merchant-projection-green,backoffice-full}.log`。独立审查没有新增阻塞。

后续继续补齐有效会员组/原到期时间、已审核且启用域名和AEAD结算资料脱敏视图。批量收款投影实际证明100个ID仍只一次参数化查询；8种当前指针/配置/身份/密文损坏均显示unavailable，不回退旧明文，数据库故障保持error。Get/List接入先关闭行游标再批量加载，实际MaxConns1契约通过；普通修改不会回写派生mask。5项后台专项通过21.031秒。

独立审查指出“基础组待确认+未到期会员”应先采用有效会员，与fees.go实际覆盖顺序一致。此组合真实RED后已修正，会员到期/未购买才保持未知基础组。继续实际复现历史profile整包进入Get/List及搜索的问题：现在只返回允许字段且检查JSON scalar类型，私有资料/迁移标记不返回、不参与搜索；普通编辑在原config_revision下合并保存，保留未公开迁移元数据和大整数JSON。两次真实RED与初次无效group夹具错误分别保留，`.tools/pro-merchant-private-profile-*.log`及`pro-merchant-profile-types-red-valid-fixture.log`。新完整后台回归另行执行，不沿用前一包结论。

资金记录增加account_id、balance_before、balance_after（整数分字符串）。同一物理账务账户按不可变entry.id完整累计，然后筛选/分页，CSV共用查询。不能将账户间余额相加冒充原Pro单钱包。真实RED后3项PG契约通过13.380秒，`.tools/pro-ledger-projection-green.log`。独立审查确认单行join、租户窗口与筛选顺序；管理员选中商户的附加谓词目前在窗口外，语义正确，但仍可能扫描所有账户，后续性能优化不能改变金额口径。

字段映射批次全部177个前端单元测试与构建通过；实际浏览器mock覆盖订单、资源、筛选、导出、历史退款、打印和授权相关操作。布局独立审查复现桌面搜索框仅24px，修复CSS后同一宽度断言通过。此处的mock和专项不能替代最终候选的完整测试与实际联调。

## Frozen71实际本地回归

固定运行源码为`.tools/checkpoints/71-mini-link-integrated-2f95e939-8155-415f-95d7-e23986ffbd35`，898个文件；本段运行的界面仍是该候选，不含本轮Pro新样式或68预授权观察增量。API127.0.0.1:8183，静态前端127.0.0.1:5180，仅模拟模式。生产配置未加载。

测试专用后代72只修mini URL Link用例的硬编码端口；后代73只修收款码用例等待PATCH提交的竞态。首次72联调6通过1失败的trace证明GET在禁用PATCH完成前发出；失败证据保留，未据此修改产品权限逻辑。73固定测试源码复跑7项全部通过1.0分钟；另外2项结算资金确认和模板打印串行通过6.6秒。证据位于`.tools/live73-dbdb98e5/.tools/checkpoints/73-live-save-synchronized-ab6439fe-3226-41f2-9dde-1e5c4b40fd0a/native71-live-*.log`。9项均使用真实本地HTTP/PostgreSQL和模拟资金；没有真实支付。

回归后CLI20项对账全部0，readiness200、静态源码匹配。PG18.6真实自定义格式备份921630字节，SHA256 `b91ccde3820409f269b532995b6117a48df3afbb3f3fb4dde5897f4c8b434009`。在本次新建GUID库实际恢复，schema67校验通过，包含2个订单/21个journals，账务和HMAC检查20项均0；临时恢复库已清理。完整证据`.tools/native71/goepay_restore71_a7be902243af46e8b30195dd16231e78/RESULT.json`。最初新helper的源库guard字符串未同步导致在备份前拒绝，修正guard后执行；未写入或删除任何旧库。

## 仍在运行的验证与后续范围

Windows71与Linux71完整测试在不同独占数据库中运行；Linux开启race。此前PG32并发DDL耗尽共享锁而停止的首次Linux运行保留为基础设施中断，不算通过。新Linux专属PG44配置max_locks_per_transaction=1024；工作增量PG46各GUID库同样隔离。只有最终test/vet/build退出码、包结果和固定源码后验全部成功才会记为完整通过。

68预授权余款观察正在独立实现和审查。它只有显式只读查询，不能capture、release、markPaid或改变账务；默认关闭。SQL verified分支的NULL CHECK缺口已由独立审查复现并修复未发布68迁移，边界与真实本地TLS专项通过。HTTP、前端和最终固定集成仍须完成。

68后续HTTP/config/core目标回归已通过：cmd/api0.715秒、真实本地TLS contract9.483秒（含既有capture）、payment132.883秒，targeted vet为0。前端已补原order/merchant/trade/nonce/request严格绑定、proof三状态一致和原冻结金额相同校验，23单元与4浏览器专项通过。GET只读原本地观察，POST显式新nonce；所有运行开关仍默认关闭。最终固定集成尚待执行。

69开发增加即时返现gross基数，奖励仍受原订单platform_fee上限约束；不能扩大到其它订单手续费、无来源clearing或负钱包。旧fee条款HMAC函数字节及57迁移完全不变，政策版本HMAC原已包含basis，原订单条款封存rewardAmount和policyIDs。金额/并发奖励/退款未知与失败恢复/成功全撤/授权摘要专项已通过，前端及全部返现回归正在继续。此项不是原Pro取消封顶、daily/profit/membership行为的兼容声明。

自动出款费用约束、原返现的daily/profit/gross/membership分支等缺口仍按remaining-core-work.md单独记录；不把菜单可访问、外观对齐或已通过的局部用例等同于80个菜单全部功能完成。
