# 原Pro买家加费与双金额模型104

本阶段使用冻结187作为基线（2253源，SOURCE_CHECKPOINT SHA fd33e9629d58a922843efef62d7d5a774200e5d04d75b6c2f5ac15c5f9e7cdb4）。此前基186的候选、失败和验证收据保留，精确三方重放104增量至187无冲突。仅使用本机原资料、合成PostgreSQL及回环假上游；不访问生产、SSH、实际支付机构或真实资金。

## 已核实的原计算器合同

原 epay_pro/app/logic/PaymentLogic.php SHA b4b068f66d3eaff19eacddfafd613bfc6ffa086236223b1a942ae7b939f2de75，使用原vendor/autoload、framework/helper.php及app/common.php和用户已有loader，在固定PHP82只读、network none容器中实际调用；没有伪造缺失函数、修改原包、启动原App或连接原数据库。44,020组输入输出及另外2,880组高精度B实际计算成功，独立Go实现逐组比较0差异。参考树786文件在重放前后均逐SHA相等。

- 44,020输入SHA 62fb28c6129ddeb9ed1b1c73e5b52d0aad451e1625b65772eabacb0eff89d28a；输出SHA 739982f9277f2d715dd9e9e8e451dfb092c20b40d38a36a5e9774b84bcdc7fd7。
- 高精度2,880输入SHA 5d9bc454ca2bfe47dc4589ca8a2d7ae7c130767491718e3efaf66a1656fff393；输出SHA a020f4f0bd21c8587a1ffbae60c35e0b79874d9962670b9bd9e25f71323c41d3。
- 已采样金额0.01..1000000元，B含0.125、0.0001、0.000001、3.141592、0.333333333333、0.000000000001和超总费率。该证据证明采样范围的原计算器行为，不声称原设置端点任意精度校验或整个应用全兼容。
- Release旧可读288例单独保留为历史参照；其小额保留净额舍入不能替代Pro新出生公式。

设M为商户原金额、T为总手续费百分比、B为显式买家承担百分比：

1. 名义手续费F0=half-up(M*T/100)。门槛和最低费均正且F0严格低于门槛时，将F0替换为最低费；不是取最大值。
2. mode0买家承担金额A=0。mode1且B空时A=F0；显式B时A=min(F0,half-up(M*B/100))。
3. C=M+A；N=max(0,C-F0)；实际平台手续费F=C-N。因此最低费超过M且部分承担时不能仍把名义F0记入账。
4. 父通道门槛/最低费仅在两项都正时整组覆盖全局；父空、0或仅填写一项均使用全局整组，不混合继承，不用父0关闭全局。
5. B作为规范十进制文本保存和MAC承诺，最多12位小数，0..100，拒指数、负数及多余前导零；整数/大整数计算，禁止JS浮点传金额。原96 retained BPS格式不改变。

## 出生与升级

所有104迁移后的真正新报价和无旧报价的新订单都冻结104来源，包括从未保存手续费设置的默认商户模式。不得依赖可写head是否存在来判断订单是历史。

orders.amount始终为M；新增nullable actual_charge_amount104、不可变出生标记和独立私有来源。新报价有omitempty104叶子，旧v1/v2报价保持nil和原MAC字节；旧prepared quote成交仍按旧报价，旧订单不回填、不重价。读取C前同时认证104来源、独立审计锚、原route及referral出生证明；丢来源/标记/表/迁移记录不能静默回到M。

政策使用严格typed CAS、原持久会话与最后写入前数据库时钟检查；商户仅本人，父/全局仅非委托super。head、审计历史和独立不可变逐revision政策witness互证；head与审计一起丢失仍由witness拒绝默认回退；合法旧head及旧MAC与截短audit不能越过最新revision witness。公开DTO不包含私有proof、通道密钥、会话或CSRF。

## 消费者金额矩阵

|消费者|金额与认证|
|---|---|
|商户签名请求、原query/list money、商户异步notify|保留M；退款状态/剩余量另按认证C|
|原订单/路由分配、单笔商户/组/父报价限制|M；不把既有入参和旧MAC偷偷改义|
|Create/native/H5/page/JSAPI/APP/mini、OAuth支付状态|认证C；mini状态写入及展示前也认证来源|
|上游查询、回调、scan首次/恢复|C；M或伪签名回执不能支付新单，重复正确回执只入账一次|
|预授权冻结、观察、扣款、撤销、剩余来源|C；原请求号/账户和可信终态保持|
|商户到账/平台手续费、收入hold、退款来源|N/F，N+F=C；退款cap与分摊分母C|
|退款申请/列表能力/批次105/资金预览|认证C减成功退款；R==M<C仍有真实剩余；Prepare在锁定订单后一次同Tx批读104来源，逐项执行仍按原独立事务认证|
|用户成功日金额risk177|已确认C累计；origin.Amount保留M，新增omitempty104来源承诺；批读同Tx、常量SQL|
|IP/买家成功笔数、原日额触发次序|原成功笔数；累计已达阈值才拒下一笔，不改incoming硬预算|
|父99成功金额、T+1今日阻留|认证C；资金读取同Tx批读，不在账户锁后锁订单|
|返奖gross、上游成本估计、profit/daily|新出生使用C；原任意成功退款整奖撤销政策不改|
|已有渠道分账/完结|声明gross与expected_unsplit按认证C、merchant_net按N；不开放用户暂缓的其它业务插件|
|商户业务通知、打印原订单金额|原money=M（Release functions.php:709、Pro MsgNotice/Printer字段）；重新执行前仍验证104来源|
|原订单金额统计|保留M字段语义；realmoney搜索、显示单独采用C，不全局重写money|

订单页面并列原订单金额M和认证实际支付C；无法认证C时显示未确认并关闭金额相关能力。手续费设置沿原mode/mode_feerate/payfee_lessthan/payfee_mincost字段、空白说明、取消及保存交互。保存丢响应进入只读重新读取，禁止自动补POST；会话/CSRF/主体变化使旧读取和保存上下文失效。

## 验证与交接要求

- [x] 真实Pro44,020+2,880逐例Go对照；旧Release288实际PHP对照。
- [x] 实际104迁移保留旧订单原列、旧terms/source MAC字节；旧NULL认证M，新默认订单按Pro舍入。
- [x] 实际本地签名TLS支付宝/微信Create、错误M回执、真C回执、防重复、原号退款及查询恢复。
- [x] 实际PG资金分摊、R==M<C批次、gross/profit奖励、C日额、T+1、私有来源组合丢失与CAS。
- [x] 实际HTTP公开DTO导出，清理其schema/会话；浏览器1440/375原控件、取消、键盘保存及丢响应读取。
- [ ] 最终稳定源码全Go/Windows/Linux race/vet/build、全前端测试及旧fixture兼容审查。
- [ ] 非作者审查、根累计精确合并、再次冻结测试和本地部署。未完成前不能宣称项目全部完成。

已保留的失败包括真实行为RED、测试脚手架错误和环境初始化错误；交接报告逐类区分。所有外部live用例默认禁止，本地签名假上游验证不是生产验收。
