# 43 结算钱包出款界面

仅处理已批准结算的独立出款绑定；审批状态与支付机构出款结果分别显示。原收款快照只展示脱敏资料。银行卡、历史缺失独立资金或收款快照的申请不提供假操作。

1. 先新增 helper/组件回归，验证未准备、unknown、失败释放、权限、陈旧响应与确认边界。
2. 独立 Panel 读取 request/payout 和管理员 capabilities；prepare 明确确认渠道，execute/cancel 通过已有管理员资金授权桥，query 不授权、不重发。
3. 在结算申请表加入详情入口；原 approved 不改为 paid。组件取消和卸载中止请求，网络不确定后只刷新原申请。
4. 定向 unit/component、mock browser、typecheck/build/audit；真实 Docker/live 等 root 统一部署。

后端合同与边界见 ../docs/design/settlement-wallet-payout.md。商户只读与查询原出款；不显示或持久保存原账号、姓名、票据或密码。
