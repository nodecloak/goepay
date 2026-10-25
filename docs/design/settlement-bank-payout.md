# 45 银行卡结算出款与可信退票

原可读证据：Release `admin/ajax_settle.php` 的 transfer 分支将银行卡结算的 realmoney 交给同一支付插件的 transfer；`includes/lib/Transfer.php` 的普通代付另收 transfer_rate。Go 已实现普通银行卡付款及微信 BANK_FAIL、支付宝 TRANS_BANKCARD_NO_PWD/REFUND 两套证据，不能将已冻结结算总额再按普通代付扣一次。

本增量复用43原结算申请、不可变收款快照、39费用、40专属gross账户、41逐动作授权、44付款商户身份；只新增45迁移。准备时明确银行产品和唯一TF，保存银行卡号/姓名/BankCode原快照，不发送。claim持久化和审计后仅一次发送，unknown只查原TF，管理员/商户读权限与钱包相同。

银行通道必须显式实现BankTransferProduct，并同时声明bank能力；微信产品要求原BankCode和加密公钥，支付宝银行产品要求证书模式及场景材料，复用现适配器验证器。不借另一个产品的返回状态或手续费证据释放资金。原AppID与公开付款账户仍固定，换配置不承接旧TF。

资金政策为明确的保守hold-and-return-v1：原gross=net+settlement fee，成功只从本申请专属预留转net到clearing，结算fee继续留在原专属账户，展示为“银行卡出款已核实，结算手续费保留待退票核对”。没有可信最终结清事件或明确收费政策前不把fee自动确认成平台收入，不虚构N天后不退票。此政策不是已证明的Pro隐藏收费优先级，后续独立费用确认能力另行设计。

微信可信BANK_FAIL要求现有原商户/原业务号/原payment_no/金额/卡号姓名摘要/固定实际银行手续费证据；已成功时clearing返net并释放原申请的fee，确认银行手续费退回沿用现有平台费用账本。首次查到可信退票则释放原gross，不能记录不存在的已付银行手续费。支付宝可信REFUND要求原提交回执绑定、原order_id、金额与类型证据；estimated_fee只展示估算，不当作微信实收费用。

终态成功历史不改写；原TF的bank lifecycle显示reversed，结算资金显示returned。不可信退票进入reversal_pending，fee与本金不会因未知消息再次入账。并发查询、重复退票、跨钱包/银行解析器、取消与claim竞争均用同一merchant→request→transfer→bank锁顺序。

数据库新增不可变settlement_bank_payouts绑定，将原43钱包不变量作为独立基线保留，银行分支额外验证原预留journal、固定净额/费用/银行产品、原本金journal、fee保留余额、退票双journal和对应lifecycle/attempt状态。bank生命周期及绑定变更也在事务提交时检查，不能只改状态伪造付款或释放。

验收全部使用随机PG schema与临时TLS假上游；不访问生产、不读导入真实密钥、不发真实交易。后端、HTTP/授权指纹、双后台读/操作与恢复均需实际验证，未验证前不能宣布45完成。
