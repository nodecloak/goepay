# parentchannel180 原父通道运行配置实施计划

> 按已授权任务逐阶段内联执行 TDD / 完成前验证。禁止提交、推送、整合、冻结、部署及修改 main/其它候选。阶段是测试/审查边界，不是允许部分产品收尾。

**Goal:** 恢复原父通道专页的真实配置、动作、过滤和统计；已接入微信V2/V3、支付宝账户及证书以AEAD不可变版本进入真实选路/适配器，所有历史资金操作恢复原账户。

**Architecture:** 冻结176独立1966源，migration103仅本任务。类型化父目录头/AEAD账户版本/授权来源/不可变支付和转账执行源，和96费率、99全局限制、既有subject snapshot分域。新增/复制默认关闭，显式同type受控runtime授权模板，只继承operator原有merchant集合；目录、组选项和路由实际消费，不拓宽旧owner集合。原176的98/100/102缺位不补假迁移，root最终整合真实98子账户、100调度、102风险后跑连续链。

**Baseline:** .tools/parentchannel180-path.txt；1966文件；manifest SHA a6e34d2ad9f9987df11ba2595f18ec4ae01afd75e7867a0a7eb27ef8033de3c8，prepare已原/复制字节验证。168B未整合源码不得整包复制，仅复用经证明的纯合并/受控工厂设计；最终写清hook组合。

## Source contract（offset为JS字符偏移；行号为PHP/Go一基）

- Pro public/static/assets/index-DNfqr2Hp.js，3790..9400基础编辑：name/rate/costrate/mode/type/plugin；action add/edit/copy，名称/支付类型/plugin必填，type联动plugin，本地刷新插件目录。
- 同文件9400..23900配置抽屉：实际插件inputs声明、apptype接口、条件字段、证书选择上传/移除、公众号appwxmp和小程序appwxa。Go不执行原eval表达式，不读取任意服务器路径/下载私钥，PEM内容作为受控字段验证并AEAD保存；GET只返回公开允许值和secret-present。
- 23900..27400统计：今昨realmoney/refundmoney/profitmoney/orderCount/successCount/successRate和曲线。只用实际parent FK与可信支付/退款回执，成本未知profit未知，不推断0成本；已存成本是估算，不称账单实际利润。
- 27400..36000高级：daytop/daymaxorder/paymin/paymax/timestart/timestop/rate_limit_seconds/count由现99真实全局限制消费；ordername/rules接原上游subject resolver，保原name和首次subject快照；六变量和随机规则按现有可证明引擎消费，不重解释历史。payfee_lessthan/mincost本任务仅记录源合同并在UI明确未接入金额模型，不能保存假运行字段；其全局继承和买家/商户承担统一归后续104，180不重算金额。Pro加密控制器空/0未知必须记录。
- 37000..55000列表：kw ID/名称、plugin、type、dstatus -1/1/0，size20；ID/name/mode/rate/type/plugin/status/operation，成本/日限/笔限/时段可选列。单行密钥/编辑/删除/测试，dropdown订单/付款/复制/统计/高级。删除采用归档保留原来源，不能物理删除旧资金证据。
- 38900..42000批量：open/close/delete；wxpay选中子集批绑公众号/mini，0不绑定。新Go写入全部CAS、sorted ID locks和同事务审计，要么全成功，要么无变更。不能混入alipay假称wx绑定成功。
- 测试{name:'支付测试',money:'1'}是实际支付意图，不是ping；明确owner、名称、元金额及确认，仅单次POST/原意图查询，unknown不换号/不换账户。
- Release admin/pay_channel.php:55..130、admin/ajax_pay.php:194..299证明基础字段、唯一名称和copy仅服务器复制config/apptype绑定。Release includes/lib/api/Pay.php:172..198证明普通代收最低费用正值计算和clamp；channel.mode1是独立直清账务，当前只明确不可用，不能按mode0执行。
- Pro app/lib/Channel.php:10..73,103..121,153..320,401..465证明parent实际type/status/rate/限制与子账户替换；原现代支付/写控制器加密，不解密/不运行、不虚构未知插件。

## 不可绕过规则

1. 真实parentID、资料ID、authorization-origin-ID、account-version分别类型化；不以名称绑定，不创建synthetic provider ID冒充旧channel。新channel是登记的真实ID，旧operator集合只从显式origin继承。
2. 原super本人持久会话、CSRF、非代登录；operator/merchant拒绝配置、证书、复制。GET无provider I/O、无缓存/任务/账户写，不输出私钥/密钥/原AEAD/proof/accountscope。
3. 103 AEAD AAD绑定channel/version/type/plugin/family，版本不可UPDATE/DELETE，head HMAC及append-only audit count拒合法旧行恢复；copy只server解密重封新ID/新版本，defaultoff。重复新建有稳定意图/原请求绑定，不因unknown新建第二条。
4. 当前parent配置仅影响新意图，route+原parent执行版本和费用quote同事务；出生独立proof要求原版本，不能删route/quote/schema/登记后降pre103。对旧订单只采用已经存在的原account/product/receipt/MAC可证身份，不能把当前资料当原凭据补造。
5. 原order query/refund/callback/close/preauth/capture/OAuth/mini/scan及恢复、分账，以及已实现provider transfer/银行/批次/结算原意图，全程原账户版本；unknown不重发送、不切当前账户。迁移旧证据未知即拒绝，不能修改旧account/proof。
6. 96父费用/99限制绑定与180执行账户域分离，保持旧nil编码与MAC。103独立source绑定原quote proof，不改96/99费用proof格式；独立append-only原意图/出生审计anchor避免丢103全schema/来源后借父降legacy。不能令99 GlobalLimits或98 child校验早返。最后DBclock在所有route/source/quote/head/counter等待写后、Commit前；原off与全局总门动态生效。
7. 99范围和频率仍realparentID，单笔比较原money；成功统计用已有可信realmoney；cost只原构造器的成本估算，缺失未知。mode1仅显示真实未实现并拒绝执行/保存为可用。
8. 微信公众号/mini绑定只选实际注册、provider/app/account匹配的profile/factory，且merchant授权交集；旧weixin资料ID不能冒充，绑定0确实移除能力。profile和凭据变化固化新版本，不改变原OAuth/mini session。
9. 原商品名/签名/重放事实不变；上游subject在既有首次快照引擎消费原可证明policy。不能为了新页面重算已发送标题或费用。
10. 全ID字符串、精确分/百分比；parent订单/付款导航真实接受channel_id且保持owner/adminrole/CSV同谓词。批量/迟到/坏成功DTO/raw同client CSRF和375都需验证。

## 文件职责与执行阶段

### A. 受控构造器 +103原账户存储（先RED）
- [ ] internal/adapters/payment/parent_channel.go：私有执行factory/schema接口，支付/transfer/OAuth/mini产品独立，禁止secret JSON。
- [ ] internal/channelconfig/parent_channel.go：复用既有BuildWithOptions/BuildOAuth/BuildMiniOAuth/mini构造器，仅封闭family允许字段/PEM，固定厂商endpoint。原导入可作为可信bootstrap，不读取其它磁盘文件。API与worker配置同构建器。
- [ ] db/migrations/000103_runtime_parent_channel_accounts.up.sql：typed head、AEAD版本、授权来源、immutable route/transfer commitment、new/create idempotency；不改已有migration，不补写旧资金数据。
- [ ] internal/payment/parent_channel_config.go / parent_channel_accounts.go：头/CAS/audit/复验会话和clock，参数keep/replace/clear，完整oldrow/schema/source-loss反例。
- [ ] 实际PG/HTTP RED：176专页API不存在；原未知字段/外部路径拒绝、API GET不泄secret；新版本保存被实际构造器消费；old原账户恢复而非新版本；撤销/跨role/大ID/rollback。

### B. 当前选路与全部原意图恢复
- [ ] internal/payment/parent_channel_execution.go / parent_channel_origin.go：按当前head解析新意图、按原immutable commitment恢复旧意图，bootstrap仅可证legacy；不存在来源不降级。
- [ ] internal/server/parent_channel_routing.go：owner允许目录批量加入真实新ID，组选项实际使用；当前new binding先于原选路/费用封存，旧intent先恢复原版本。
- [ ] 原ProviderFlow标准/owned/Notify/scan/preauth/recovery/sharing和provider transfers所有g.Providers直接读点统一接解析器；不复制168B整个源码，nil parent managed保持原行为。
- [ ] server OAuth/mini/browser/collection/recharge和worker从同原账户恢复；clone保动态AllowLive/query/scan/identity总门，证书到期无法证明时明确失败。
- [ ] actual local TLS/HTTP：A账户先创建/未知，换B后新单B；原A query/refund/callback/preauth和transfer查询仍A，wrong-account回执拒绝；删原全部来源拒绝且I/O0；并发政策/expiry最后写回滚。

### C. 原专页全部字段与动作真实闭环
- [ ] server parent_channel_console.go：super-only list/detail/schemas/create/copy/PATCH/archive/batch/bind/test/original-test-read/statistics，strict JSON/全ID/同scope筛选，API spec安装器与allowlist。
- [ ] 基础name/分成ratio/mode0/cost estimate/type/plugin进入真实runtime。原fee/99高级/subject policy实际调用既有事务引擎，不扩subject全局规则，不引104最低费用消费者/金额重算；新增最小共享tx helpers，不套两次不同公式。
- [ ] parent统计actual FK/可信回执/退款与原cost，今昨+曲线；未知利润null；关联付款filter按实际provider_transfer_attempt FK而非描述字符串。
- [ ] web/src/parent-channels.ts、components/ParentChannelPage.vue及必要小表单：原列、可选列、筛选、动态输入、批量、copy/test/stat/advanced、wx profile绑定全部可达；原ActionDropdown焦点一致。
- [ ] 新建/参数未知提交必须显式读核对，成功DTO严格目标/版本；前景交互raw stamp失效清秘密，abort/late跨会话不可覆盖新owner，375与1440实际交互截图。

### D. 全回归与交付
- [ ] 全实际PG/HTTP/TLS正负例，既有96/99/101/原PHP/sourceproof/账务/权限/CSV/原firstsend/unknown回归；Vue --noEmit、全unit、原动作桌面/375浏览器。
- [ ] 显式每路径176旧/新SHA、所有RED/失败stage保留、final receipts/screens；C独立review问题修复保留前清单。root最终合成98/100/102/103真实连续迁移与both-on接口，不自称部署/全量原行为完成。

## 实施细化记录（源核对后；继续实现而非验收声明）

- 96/99 routeFeeFacts、routeFeeProof、routeOrderFeeProof编码不变。103单独parent execution source将绑定已有quote proof；RuntimeChannel可持私有受信parent execution引用，fee anchor为首次登记真实parentID的原identity/product/account，params换版不改该fee域。实际provider的PaymentProductIdentity/TransferAccountIdentity绝不伪装成fee anchor。first-attempt验证只有已核103source可使用独立实际execution account；nil路径仍逐字原验证。99 GlobalLimits共用同fee anchor且不得被180早返跳过。
- 已读176 validateRouteFeeAttempt：bound product/identity对原quote，accountScope现直接对quote.AccountHash。因此180需要显式验证独立103scope/source后再过fee域校验，不能让实际provider谎报身份。costQuote现只验证channel/method，Cost实际AppID/account仍由真实provider产生并在原profit cost snapshot验证；不改变旧费用金额或proof格式。
- 原bootstrap registry保留，不直接全局改Providers map。新意图按103当前head构造per-request provider overlay，原意图先恢复immutable account版本；newroute的parent锁内须重新核当前版本，避免在外预检后等待head换版而选旧账户。已封原意图在head参数换版后仍用原版本，只重核当前启停/授权/时限，不要求同current accountVersion。
- 生命周期恢复不能依赖当前import中还有原私钥/未过期bootstrap证书。103 immutable账号内容自足，使用可信固定family restorer；当前head只影响新单。启动时受管理ID应从已验103版本构建，未管理ID保持原import构造；旧证据不能由当前import补造。
- 为防全103表/schema/登记丢失后降legacy，除103source不可变MAC外，在现append-only审计域写独立原意图及出生anchor（按owner/outno或真实orderID索引）。只要原审计/现96出生事实仍在就拒绝缺源；不去改96/99费用proof格式，也不修改旧order/account/proof。仍需实际丢schema/全source/flag反例验证。
- 103目录头仅metadata/accountVersion/archived及私有fee anchor；启停以现96真正enabled为单一支付开关，新ID创建时同事务写96disabled，避免两个enabled相互矛盾。高级99和原subject消费者沿现实现，minimumfee本任务仅说明后续104，绝不增加金额重算。
- 新ID是真实登记parent ID，由103专用sequence+目录锁避开原import全ID和已存ID，溢出拒绝。复制服务端解密重封新ID，默认关闭。受控授权模板同type，创建时operator原merchant集合做不可变授权快照，再和当前operator集合取交集；不会自动添加后来新merchant，也不借旧资料ID。
- parent页面只读应批量无父行FOR SHARE锁，避免持admin session后读parent row锁形成与写入/改密码三方队列死锁；写入sorted parent advisory→metadata/policy→durableactor/finalclock；复制原/新ID同排序。批量atomic不半提交。
- 现纯factory已真实构造器验证WX账户A/Bscope不同、raw原snapshot恢复、跨ID拒绝、fresh secret必须显式提供、GET projector不echo secret、copy只server重封、unknown keep/clear也拒绝。完整channelconfig39根37子0skip通过。它尚未接103或runtime，不当作产品闭环完成。

### 103新意图bootstrap捕获决策（待runtime RED→实现）

首次180新选路可在已有parent锁/原route事务内封存可信原import的bootstrap账号版本，不新增/改写96费率政策、启停或金额；否则编辑前创建、尚未发起的订单无法保原key。该捕获不是管理配置变更，纯GET不做它。原registered self-head可legacy_origin=true、96仍unconfigured；当前权限仍原operator self ID。新alias/copy为legacy=false，96显式off和创建时grant快照交集。新意图均有103source/quote-proof及独立审计anchor；真正pre180若无原产品/密钥版本证明不把当前import补造为旧版，未知拒绝I/O。

head pay_type/source/初始fee anchor保持固定；跨方式用新ID和明确新授权模板。微信V2/V3或支付宝RSA/cert在同pay_type内改params新版本，实际execution full identity另证。103来源恢复不依赖current head.accountVersion，prepared/unknown只原source；新选路必须锁内复验current version。后续configured启动要先读取已认证103 current账号，避免旧bootstrap证书到期阻断已经更新的current版本；未管理ID仍原import构造。

实际已通过：创建AEAD且defaultoff；same request幂等/改变原intent409；last write trigger延迟后到期403且业务表/96新policy全回滚；parent锁等待时raw CSRF变更403无写；account更新新增v2、v1密文不变、合法完整old head UPDATE复活被audit count拒409；catalog只读单连接可工作（原nested tx已actual503 RED→GREEN）。这些不是完整产品闭环，runtime/所有原操作/UI仍须实现验证。
