# 付款码恢复78开发记录

仅主区开发，不在固定91或实际部署91中。已实现两通道撤销/只读查询适配器、原待支付应答凭据、持久来源/六次查询链和首次撤销claim/全入口入账屏障。78迁移随下一固定源码候选冻结，当前追加六个新表及原账户两个版本字段。恢复worker、HTTP有界等待、独立执行配置、CLI及双后台只读状态已在主区接入并阶段验证；完整候选、两平台/race和本地部署验收尚未完成。当前实际部署仍91/schema77，新执行默认关闭。不得称已完成付款码超时恢复。计划及官方/本地依据见[78设计](../design/scan-recovery-78.md)。

新增独立`ScanCancellationProvider`及默认false的适配器配置；所有实际使用方必须先建立原持久授权与屏障，原Build默认关闭该标志；新BuildWithOptions须显式启用，仍受普通写权限和完整凭据约束。公开结果带私有认证来源，原账户/产品/订单/金额和请求、已验签应答投影摘要绑定；JSON反序列化或修改公开结果字段不能保留认证。

实际本地证据：`.tools/settlement-auto73-next-scan78-alipay-red.log`先因没有类型化撤销失败，`alipay-green`1.103秒覆盖原请求RSA2、真实TLS、close/refund、重试标志/错单号/错机构号/坏签名/坏action/明文HTTP，不隐式重试。微信`wechat-red`先缺能力失败，`both-green`1.467秒覆盖真实双向TLS客户端证书、MD5原请求/应答、可选recall缺省、Y未决、错账户/错单/坏签名、失败与无证书/默认关闭/明文HTTP/Insecure配置零IO；同时回归原76微信错误认证。

`.tools/settlement-auto73-next-scan78-adapters-full.log`实际运行当前整个payment适配器包11.864秒通过（PHP互操作开关未启用），包括默认关闭及非法原请求零IO测试。外呼全部为临时localhost TLS监听与本地私钥；未使用真实密钥、真实支付机构或资金交易。这个包级结果不能替代后续完整源码候选的Windows/Linux PG/race、PHP、本地部署/备份恢复。

补充真实响应边界红绿：`response-boundary-red`发现支付宝可选app_id/seller_id矛盾、无预期机构号时的畸形trade_no和微信缺必填nonce仍被受理；修复后`response-boundary-green`整个适配器包12.882秒通过。取消前额外校验实际账户scope，防止外呼后才发现不能生成凭据。没有因此启用渠道工厂或恢复入口。

原attempt绑定的持久恢复来源、收款屏障及单次claim已在后续阶段实现并专项验证。当前继续接有界轮询、HTTP响应及worker；前述适配器记录保留其阶段时间边界。普通close、退款或再次扫码发送都不得冒充原号撤销。

本轮原待支付凭据：`pending-behavior-red`实际发现缺少4个原USERPAYING/SYSTEMERROR与10003/20000分支的认证来源；`pending-green2`通过。`pending-generic-red`又发现通用支付宝error_response可误作原扫码应答，已限定明确的alipay_trade_pay_response节点。`pending-adapters-full`整个适配器包13.598秒通过，包括真实已验证TLS、签名、错误订单/账户/金额、Insecure、通用错误、普通文本、篡改Code、JSON重放、取消/未知包装均不给自动恢复凭据。回执不含付款码和原始上游内容。原查询及76出款错误语义保持；尚未接入自动取消调用。

本轮持久来源：`origin-red`实际缺少原事务恢复来源；`origin-green`38.094秒通过首轮。新BeginRecoverableScanAttempt只在首次scan原事务固定请求身份HMAC、原账户HMAC、原商户/订单/金额/通道和15秒/3秒/最多6次/120秒条款。V1账户MAC独立绑定来源，不可删来源或降成V0绕过；V0原MAC不变。追加审计的`origin-audit-red`发现原读取忽略多余审计，已限定该来源恰好一条匹配审计。`origin-regression`73.492秒实际通过完整新来源测试及旧扫码/账户回归：8并发只有一次发送权、换身份/账户冲突、来源/审计缺失或篡改拒绝、写失败完整回滚、真实77旧形状读取及78升级无补授权、旧未知订单与所有旧字段保持。来源创建没有提供扣款、退款或撤销能力，余额为零。

原待支付持久登记`eligibility-red`实际缺API而失败；`eligibility-green`10.280秒通过，匹配完整原请求、应用身份，且事务审计失败无部分登记或恢复任务。`source-eligibility-regression`88.731秒通过当前全部来源/登记测试，含新账户bytea_output=escape、篡改receipt/来源、删除登记/审计/任务、换任务payload均不能重开原权限。迁移78仍在开发，当前新增来源和登记两表，未发布或修改旧SQL。

`observation-red`实际缺少类型化只读认证来源而失败；`observation-green`1.230秒通过真实TLS与签名的原query、待支付/成功/关闭、错单号/账户/机构号、坏签名、HTTP零IO、Insecure、未知状态、通用错误节点/缺nonce、修改Buyer/Amount和JSON伪造拒绝。补充普通Query不能获得此来源、禁用/非法请求零IO之后，`observation-adapters-full`整个适配器包11.735秒通过。`.tools/scan78-main-vet-build-20261024.json`实际全项目vet/build均0；这是开发区阶段检查，不是固定候选的完整双平台测试或部署验收。

有界查询：`query-claim-red`缺持久查询权而失败；`query-claim-green`9.419秒通过8消费者一次领取、原租约、账户、私有应答及三秒间隔。`query-source-regression`183.253秒有1个测试夹具失败：新query_heads外键已正确阻止删除eligibility，旧损坏夹具没有先处理该保护；未修改生产外键。夹具先断言23503，再仅在随机测试schema删除独立head构造损坏，`query-fixture-fk-green`6.075秒通过。修正后的`query-source-reviewed-regression`184.614秒实际全部扫码来源/查询测试通过，覆盖六次/12事件上限、真实时间间隔、未知读错误终止、过期/换租约不重开、头/历史/审计损坏和审计失败回滚。该阶段SQL在运行期间未修改。

持久撤销与屏障：`cancel-domain-red`实际缺永久取消来源而失败；`cancel-domain-green`48.404秒通过8并发一次claim，回调/查询/直接入账/普通close全部受同一订单锁内屏障保护，先入账则新撤销拒绝。取消网络错误与伪造结果不关单，原余额不增加。`legacy-guard-red`实际发现新guard会读取不存在的pre46账户表；改为先核实实际表形状，V1/部分新形状缺失保持拒绝，真实旧形状保持兼容；`legacy-guard-green`3.188秒通过。

原请求绑定：`cancel-request-binding-red`24.587秒发现支付宝虽然回显相同机构号，但回执原请求没有携带claim中的预期机构号时，仍可完成关单；增加私有原请求摘要匹配。`cancel-matcher-adapters-full`整个适配器包13.421秒通过。第一次`cancel-domain-reviewed-green`整组119.084秒未通过，原因是新支付宝测试回调错用request canonical签名；未更改生产验签。夹具改用notification canonical独立RSA签名并在计时查询之前显式验签后，`cancel-alipay-notification-reviewed`71.842秒实际通过close/refund和不同请求上下文拒绝，原ACK在租约过期/商户停用后仍可按永久原claim保存，重复保存不重复撤销，迟到回调不入账。

微信`cancel-wechat-domain-tls`48.265秒实际通过原扫码、六次只读查询、双向证书reverse全链；取消请求到达本地TLS服务器时，独立PG连接已见永久claim及审计，且原订单没有被网络等待事务锁住。N确定关闭原pending订单；Y保持未决屏障；均只执行一次本地reverse，不重发付款码、不产生余额。`cancel-integrity-final-wait`95.876秒通过claim/来源/账户/查询头/审计损坏拒绝，以及审计中实际等待超过最后待支付应答的三秒窗口、商户停用或租约失效时回滚整个claim。回滚后真实签名原回调仍可入账，证明未产生虚假永久屏障。

以上都是阶段专项，不合并声称当前完整Go工程/两平台/race或本地部署已通过78。完整候选仍为91/schema77，实际部署为91/schema77；78执行入口已接入主区；完整候选集成继续验证。

## 运行接入阶段证据

CLI先以缺字段与独立孤儿来源测试实际失败，`checker-green`67.528秒通过：空来源/合法排队不算未决金融动作；永久取消claim无ACK及unknown计数为1；独立审计、任务、缺父来源、106个候选跨页和畸形数字均被检出。新增invalid_scan_recovery_sources、unknown_scan_cancellations，使完整CLI诊断共24项。查询窗口再按本地PHP复核，`original-poll-rules-red`实际发现缺额外2秒以及NOTPAY被错误继续；修正后`original-poll-rules-green`31.393秒通过。新增取消终态/损坏来源诊断断言后，`cancellation-diagnostics`103.895秒通过其所选支付宝与损坏来源测试；微信新增断言随整组回归验证，不以旧专项冒充已重跑。

私有错误来源补证：`binder-uncertainty-privacy-red`实际发现绑定器可去掉Unknown/context包装及上游错误码泄露原付款码；修复后整个适配器`binder-reviewed-adapters-full`12.009秒通过。普通出款`negative-auth-red`10.484秒复现：错误商户号但相同明确失败码可释放原110分冻结。现在必须有实际已验证TLS、验签来源、原产品及明确失败词汇；错误账户/明文HTTP保持unknown冻结，可信失败仍释放。旧签名测试原先使用明文HTTP，保留业务断言并改真实TLS；`negative-transport-green`37.648秒通过，整个适配器`adapters-after-transfer-auth`11.558秒通过；`wallet-settlement-regression`304.117秒覆盖转账、批次及结算相关回归通过。没有修改持久出款证明格式或扩大任何自动重发权。

入口`submit-red`实际缺独立默认关闭政策；首次green中即时成功夹具缺MICROPAY字段而失败，未放宽生产认证。修正夹具后`submit-fixture-green`31.494秒通过首次扣款前提交来源、唯一发送、唯一原任务、未认证错误不获恢复权、即时成功一次入账及旧扫码回归。`executor-red`缺执行流；补齐后`executor-build-green`60.314秒通过单次私有观测、当前租约/开关、明确成功入账、NOTPAY与读错误结束、重复调用不新增查询。`executor-cancel-routing-red`中payment包78.479秒通过六次查询后单次取消、原ACK丢失保持屏障、调用方取消后保存原ACK；jobs包实际缺路由而失败。按原attempt和严格任务结构路由后，`api-queue-full-green`两个包1.104/1.818秒完整通过，独立worker包`worker-wiring-green`0.896秒通过。原只读reconciler不带新撤销能力。

`wait-red`实际缺原任务有界等待；`wait-green`39.779秒通过成功/NOTPAY、100ms调用方期限、已有worker租约不抢占及跨商户拒绝。`http-wait-red`实际原接口过早返回未决；接入后`http-wait-green`26.638秒通过原V1/V2成功字段及旧扫码/预授权路由回归。开启新政策后的历史V0重放又由`legacy-toggle-red`5.876秒实际发现冲突；分流保持旧身份/账户校验，不补原授权，`legacy-toggle-green`33.068秒通过。补充旧已付V0重放与V1元数据降级拒绝后，`final-legacy-replay-green`28.471秒通过五种实际TLS场景。

双后台本地只读状态：`console-view-red`缺来源投影，`console-view-green`11.180秒通过未提交省略、原来源/已登记/明确NOTPAY状态、当前商户归属、无付款码/内部proof泄露和坏历史拒绝。`console-http-red`实际未返回状态，接入后`console-http-green`27.819秒通过原V1/V2恢复、管理员详情/商户归属及旧订单投影回归。前端新面板3项先缺组件失败，再通过；主区完整68文件/302单元及typecheck/build通过。新增4项桌面/375px管理端/商户端浏览器测试3.3秒通过，截图实际检查无横向溢出，不提供重发、再次取消或普通关单入口。保留>500KB构建提示及环境NO_COLOR/FORCE_COLOR提示。尚未据此声称固定候选完整浏览器/两平台或78部署已通过。

接口冲突补证：原撤销未决/已取消的已知状态原先会被后台fail映射为500，`http-conflict-red`实际复现。现分别映射为明确409业务冲突，`http-conflict-green`1.337秒通过，不把迟到SUCCESS当作可重新入账。主区阶段vet/build实际退出0，见`.tools/scan78-main-final-vet-build.json`；此后仍以固定候选完整执行为准。
