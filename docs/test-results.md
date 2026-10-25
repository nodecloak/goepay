# 测试与复核记录

## 最新执行状态（2026-10-25）

实际本地已升级到98/schema83，后台8185/API8184。固定1394源码完整Windows/Linux各37包2094通过事件，test/vet/build全0，Linux实际race，源码前后不变。fresh前端322单元/168mock、12项原PHP（含四种结算CSV逐字节）、双平台各19原生控制器及扫描独立通过。本地96→98增量保留196张原业务表所有原列/行哈希及82条原迁移记录，三张新增批次表；8二进制/3静态资源SHA核对通过。两通道模拟、9项实际部署浏览器、额外通知/审核/域名/批次及只读性能观察全部通过；200表完整恢复、83迁移checksum、32项零检查完成，33订单/208凭证/416分录、原库前后哈希不变，临时库已清理。连续流程实际退出0，ACTUAL_ACCEPTANCE已记录，详见[98集成记录](verification/integrated-candidate-98-2026-10-25.md)。

密集登录后，额外验收已等待原15分钟限流自然恢复；没有重置、绕过或修改登录策略。部署工具第一轮因遗留97路径校验在构建/停机/迁移前阻断，失败日志和原工具保留；精确修正路径、重新核对完整证据后，在独立resume目录完成实际升级及全部本地验收。应用源码与完整测试结论未改动。恢复归档1,391,255字节，SHA256 `8ab3c3b9ae519c90b9f955416bffc61ab99834e96038ac9bcf72d19c188590fb`。

此前96通知/支付审核/域名额外验收及197表恢复已全部完成，见[96历史集成](verification/integrated-candidate-96-2026-10-25.md)。97全库实际76→77结构失败已保留并取消，98修复后重新完整执行；不把97或中断任务算作通过。其余下文均为历史记录，以最新固定候选的明确证据为准。

日期：2026-10-24；代码目录 `E:\GOEPAY`。以下只代表列出的本地验证范围，不代表原系统所有功能完全兼容。当前独立验收项目8184/8185已运行后端/前端83、schema70；8180/8181是较早环境，不用于本轮界面核对。

## 当前83已完成的验收

[完整来源与失败保留记录](verification/integrated-83-2026-10-24.md)。Windows及Linux race分别组合覆盖32包、各1,329个通过事件：82与83除一个contract测试夹具文件外969文件逐字节相同，取其余31包真实终态，加上83完整111个contract顶层测试及全树vet/build退出0。82原完整命令exit1保留，不冒充单次83全树命令成功。

最终83前端252单元、130mock通过、9个live默认跳过，fresh双npm audit均exit0/漏洞0；PHP V1/V2及七项通道SDK全部实际RUN/PASS、network none；Go源调用图无已导入包或可达符号告警，另保留未引用OpenPGP模块通告。所有固定969源前后SHA不变。

实际Docker69→70不重置升级，156旧业务表全行SHA/条数与69旧迁移记录不变；六Linux二进制和实际服务静态资源核对通过，默认只PG/API/web。微信、支付宝本地模拟create/checkout/重复callback/query/部分退款幂等通过；General7、Funds1、Printing1真实9项通过，无失败/跳过，未apply修改登录限流。

真实18.6客户端自定义归档备份→全新GUID库恢复通过：162表全行hash/count一致，9订单、62日志、124分录，70迁移逐名/校验和相符、20项检查全0；原库前后不变，临时库已清理。归档1,011,651字节，SHA256 `530ae4b26795ceb9ba66afa483ec623da25ccf908ce7d83ba9eaa212d2b0b07b`。证据入口 `.tools/deployment74-bf15d7cdc2c9445da7465641360cb661/RESTORE83_VERIFIED.json` 与 `.tools/live83-9d34c71e465d4e9e98dd03a331b5626d/ACTUAL_ACCEPTANCE.json`。

主工作源码中的利润成本71、余额会员推广72、RSA密钥对及订单精确搜索不在83之内，继续独立集成和全量验收；本记录不将开发专项通过写成下一总候选已部署。以下为历史记录。

## 历史集成80（当时完整后端验证中）

[固定966文件及完整证据入口](verification/integrated-80-2026-10-24.md)。日结70退款/来源检查器、双后台和默认关闭TLS SMTP已集成；前端55文件252项单元、类型/生产构建、130mock/0fail/9个实际联调opt-in skip通过。fresh npm audit完整及omitdev均0。原PHP V1/V2和六项通道SDK全部实际通过、无skip；govulncheck源调用图扫描退出0，仅未导入的openpgp模块通告，无已导入包或可达符号告警。Windows/Linux全库仍运行，未提前宣布通过或部署；支付宝订单码后续窄增量另行冻结。

## Pro样式与字段整合74/75（验收继续中）

最新复核：75固定源码935前后不变，214单元/typecheck/build及126mock/0fail/9skip均通过。独立Docker后端74/schema69、前端75已部署8184/8185；原PUBLIC_ORIGIN错指web端口的模拟smoke失败已保留，修为API8184后两通道强制loopback/禁止redirect的原完整smoke通过。9项实际浏览器通过，其中旧英文状态断言与新中文展示不符的失败保留；唯一测试修复改为精确data-state，SQL证明原转账成功只结算一次。第一次默认API8180误配与真实Printing429均保留，最终显式API8184及仅两个合成账号/已核实web源的account-source窗口处理后原项通过。没有修改global限流或删除审计；失败批次遗留1分未发送预留如实保留。

实际PG18.6自定义格式备份/恢复：157表全行hash与count一致，6订单/44journals，69迁移校验、20对账全0；独占GUID恢复库已清理，源库只读且前后不变。archive942634字节，SHA256 `ce86a91341375ba6bd216572c7b08b22531bc7ee78af7a0f08d470fe11a103bf`。两次前置guard/目标空public schema导致的失败也保留。证据`.tools/deployment74-bf15d7cdc2c9445da7465641360cb661/RESTORE75_HANDOFF.md`。

71父基线完整Windows29包、1197测试pass/7skip，vet/build0；Linux完整race29包同样1197pass/7skip，test/vet/build/shell/container全0且898源SHA前后不变。74完整Linux仍运行；Windows74的URL Link与并发登录失败已单独复现并记录原失败。诊断确认前者Windows与Docker DB时钟偏差使adapter拒绝未来出生，后者30个冷连接竞争触发生产2秒DB预算；79固定只修改两测试夹具，生产校验/时限/401与429断言不变，同目标count3实际21pass/0fail，75.144秒。此专项不是新的完整Windows全库通过声明。

时间显示76/78仅8个前端源/单测文件与1个浏览器期望增量，Go源及依赖同74。显式时间白名单严格解析原offset，固定UTC+8、无效或无offset原样保留；219单元/构建与126mock/0fail/9skip通过。76原125pass/1fail的旧注册时间标签/原始RFC3339定位断言保留，78只修精确显示期望；独立两个host时区专项通过。部署目前仍是75，70/SMTP开发代码不在74..79这些固定候选内。

固定74为935个源码文件，原1..67迁移完全不变，仅新增68只读预授权观察及69原手续费预算封顶的gross基数即时返现。订单、商户余额/有效会员/已审核域名/加密收款资料脱敏视图、资金账户变更前后金额及Pro双后台布局已整合。详见[持续验证](verification/pro-console-and-native71-2026-10-24.md)。新的Windows完整PG/vet/build与Linux完整race/vet/build已在两个专属cluster启动，最终状态尚待日志确认。

74固定前端214单元、类型和构建通过；真正全量mock浏览器结果为116通过/10失败/9实际联调用例跳过，不能记成全通过。10项375px溢出已测得共同根因：主内容区width/margin动画在桌面转手机的瞬间保留旧偏移。75严格从74复制，唯一源码变化是移动端主容器transition:none，Go及依赖完全相同；原10个失败用例无sleep、无修改断言实际复跑通过，75完整前端回归另行执行。所有失败和935个源码前后SHA记录保留。

另以74固定源码编译测试二进制，在原公开PHP参考目录运行SDK互操作，PHP容器network=none：原商户SDK V1/V2，以及原通道签名、APP、付款码、银行卡OAEP、证书序列和小程序编码六项均实际通过。首次工具参数错误与一次空选择器运行保留，空运行不计成功；最终校验明确要求V1/V2实际RUN和六项provider PASS。证据`.tools/php74-5f2bdca5262c4d3ea249389ac7f445b9/RESULT.json`，没有执行PHP服务或访问上游。

独立Docker验收项目已从native71的正账自定义备份恢复67，升级69后11个原财务表SHA和条数完全不变，2订单/21journal，CLI20字段全0。API8184和前端8185使用新项目/新卷及明确合成配置，全部外部通道开关关闭，不加载旧.env或真实导入凭证。实际支付smoke发现初次公开支付地址误指向仅提供后台路由的前端，失败保留；正在修复独立项目公开地址配置并重跑，尚不宣布全部Docker功能验收完成。

## 固定68独立运行与67开发验证

[固定68原生9项联调及真实备份恢复](verification/native-integrated-68-2026-10-24.md)通过：使用独立GUID沙箱数据库、8182 API和5179前端，恢复后2订单/13journal，迁移66及CLI20字段全0。首次余额0正确拒绝的2个测试失败保留，之后仅通过签名sandbox订单模拟到账补夹具，再完整重跑9项通过。原始Docker8180/8181未切换。

旧退款正数据演练另在专属随机schema和新建恢复库完成：3条原succeeded/failed/unknown历史完整保留，HMAC/父清单/重放/保存点及迁移checksum通过。冻结68 CLI只有`unknown_legacy_refunds=1`非0，`invalid_legacy_refund_history=0`，预期exit1；这是正确保留待核对义务，不能写成20项全0。原始证据`.tools/history65-backup-f2b7668acbba46e08157f02de42422e2`，仅本次资源已清理。

支付宝应用授权与固定打印/历史修复证据分别见[应用授权68](verification/application-authorization-68-2026-10-24.md)、[打印66](verification/printing-transport-66-2026-10-24.md)与[历史65](verification/legacy-refund-history-65-2026-10-24.md)。固定68完整Windows及Ubuntu Linux race尚在运行，不因专项通过而提前认定全库成功。新增微信URL Link代码与迁移67不在68内，须另行固定与验收。

## 最新62集成验收

[811父候选全库与812收款标题修复版专项、部署完整证据](verification/integrated-62-2026-10-23.md)。Windows全库PG/vet/build、Linux全库race/vet均退出0：payment1477.904s/1469.962s，printing110.547s/111.154s，contract368.984s/390.604s；110单元/92mock、类型、生产构建、依赖审计通过。规范811与测试811仅正式Nginx测试网络脚本不同，Go/UI逐字节一致，正式13路径测试单独通过。

实际Docker发现原自定义收款h1回归，保留原浏览器断言，真实PG红→绿后修复。812候选仅该运行时标题一行、原契约强化和新只读浏览器脚本变化；两平台Collection/Template/Printing实际PG/race通过，原7live59.6s+资金与模板打印2live5.5s通过。密集登录触发的正常限流及测试窗口准备约束失败均保留；只准备本地种子计数窗口，不改限流代码和审计。60→62无重置升级、三项smoke、正式恢复库完整CLI及最终16字段全0通过，1..62迁移SHA未改。修复版没有冒称再次全库，后续63/64须另行固定完整候选。

## 历史60集成验收

[759父快照全库及760恢复修复版专项、部署证据](verification/integrated-60-2026-10-23.md)。Windows完整PG/vet/build和Linux完整race/vet通过：payment1379.657s/1387.969s、contract345.935s/369.217s；97单元/86mock、生产构建及npm audit0通过。独审dead恢复P2修复后760源候选通过计划/执行器Windows回归与Linuxrace专项，不冒称修复版再次全库；该修复已纳入后续62全库。无重置59→60升级、三项smoke、7实际浏览器59.8s+1资金授权3.0s、60恢复、CLI15字段全0通过，执行器默认关闭。

62结果见上方最新集成验收。打印和模板独审的P1/P2、原失败与修复另见各专项记录。

## 最新59统一验收

[固定718文件、初轮失败事实与完整结果](verification/integrated-59-2026-10-23.md)。Windows全库PG/vet/build及Linux全库race/vet全部通过，payment1183.939s/1179.164s、contract329.635s/341.450s；89前端单元、83mock、生产构建、npm audit0、原PHP互操作通过。本地schema55→59无重置升级、三项smoke、7项实际浏览器和1项独立资金授权、59备份恢复、CLI14字段全0通过。1–55冻结迁移SHA未改变。

60候选后续结果及dead恢复修复见上方最新60记录，保留59与更早记录作为历史证据。

## 最新55统一验收

[固定源码、失败事实与完整证据](verification/integrated-55-2026-10-23.md)。663 文件修订快照已通过 Windows 全库真实 PG/vet/cmd 构建、Linux 全库 race/vet；payment 分别 914.253s/808.002s。81 前端单元、78 mock 浏览器、生产构建通过；schema49→55 无重置升级、三项 smoke、7 项实际浏览器加1项串行管理员资金授权、55恢复演练及CLI10字段全0通过。

保留初轮旧测试错误断言和 payment 默认10分钟超时事实；完整重跑使用 `-timeout 25m -p 2`。新56–59专项、独立审查及后续源修改不能沿用55通过结论。

## 最新46/47/48验证

46/47详见verification/provider-account-identity-2026-10-23.md。固定47-verified-candidate-2034d756-acbd-4a08-b6bd-440a84237d11含573文件：Windows全库/vet/cmd构建exit0，payment414.023s/contract232.210s；Linux全库race/vet exit0，payment364.330s/contract211.729s。71前端单元、70mock浏览器、7项Docker live、47迁移恢复通过。原82菜单75partial/5pending/2deferred，不是完整功能验收。

48增加直连微信APIv3支付与加密通知，临时TLS覆盖四种材料/查询/关闭/退款/查退款、签名原始字节、重复JSON、超限、可信serial、204及未知不重发；实际HTTP/PG证明V3回调204/JSON与重复入账一次。发现guarded未透传实际收付款商户号，真实配置工厂红测后补转发，不能以47全库通过掩盖此缺口。48已重新构建部署api/worker/migrate，本地仍simulation，三项冒烟通过。

48固定候选48-candidate-9e0d1bf7-bd4f-4175-8d67-2b35db8a30a8含582文件，Linux全库race/vet exit0：payment354.827s/contract208.545s。Windows首次全库因测试PG max_locks_per_transaction=64耗尽shared memory而失败（backoffice迁移000005），其他包包括payment402.169s/contract227.787s通过；后续vet/build当轮因门禁未运行。同一固定快照以-p 2降低包并发重跑全库/vet/cmd构建exit0：payment380.367s、contract188.397s、backoffice43.447s，未改变应用逻辑或PG参数。后续49文件不属于此快照。

## 最新44/45集成验证

| 检查 | 实际结果 |
|---|---|
| 固定候选 | 45-candidate-bef86d2d-d121-40a8-a2f4-b7d8f928409c，558源码文件；Linux测试镜像复制此Go/测试/迁移快照，不挂载配置或原PHP。后续P2修复另见下行。 |
| Windows全库真实PG / vet / cmd构建 | exit0；payment393.535s、contract236.904s、backoffice90.732s、identity69.654s等。与容器/构建并行，不能当独占性能基准。 |
| Linux全库race + vet | exit0；payment347.881s、contract214.574s、backoffice70.715s、identity64.693s等。 |
| 独立审阅及P2红绿 | 审阅发现依赖账户身份损坏后银行funding投影缺行被检查遗漏；真实PG只改clearing.kind、不禁约束，红测重现错误全0。改CheckLedger和restore从每条funding检查NOT EXISTS(valid)，不改冻结45SQL。Windows结算/账本/payment专项83.503s、reconciliation3.405s及vet通过；Linux对应race68.088s/3.418s及全库vet通过。未将此小幅修复声称再次全库运行。 |
| 前端 | 65项unit/component（25文件）、类型/生产构建通过，构建BcvNA2QM；68项mockbrowser25.7s通过，8项live默认跳过。新增两银行产品成功→待核对→退票的浏览器流程。 |
| 部署后真实浏览器 | 原7项live整组58.2s通过（含74个partial菜单、RBAC、钱包/批次、申请/拒绝）；独立资金密码→逐动作钱包结算live2.8s通过（套件3.9s）。银行卡真实协议通过临时TLS/PG专项，不把mock银行测试说成Docker真实银行出款。 |
| 本地Docker / 恢复 | 原43库应用44/45，无清库；API/PG健康，Web/Worker运行。P2后api/worker/migrate重新构建部署。45库备份到本次随机新库恢复，journal/account/逐原funding/subset异常0；CLI reconcile全部7项0。 |
| 冒烟 | 原身份/CSRF/MD5/幂等入账/退款预占、微信/支付宝simulation下单/重复回调一次入账/原号退款、Nginx代理及查询token日志过滤均通过。 |
| 最终检查点 | 45-final-670837ed-b094-4022-822f-549139581914，559源码文件逐项SHA256验证，含P2修复；后续46/47开发不归入此快照。 |

失败事实保留：44首轮Windows和Linux全库均因旧bankHTTPProvider嵌入nil Provider但未声明ID而panic；补测试夹具显式ID后，由45全库重跑覆盖。45最初银行卡准备不支持、银行取消未识别结算专用proof、报表把held fee计收入、微信未显式声明bank产品，均经红测/真实TLS暴露并修复；未降低金额、签名或资金归属断言。首次live过滤`^live`未匹配到测试，修正过滤后实际执行7项，不将“无测试”算通过。

浏览器报告：[68项mock](../web/playwright-report/runs/2026-10-23T05-08-13-605Z/index.html)、[7项live](../web/playwright-report/runs/2026-10-23T05-12-37-611Z/index.html)、[独立资金授权](../web/playwright-report/runs/2026-10-23T05-15-19-600Z/index.html)。[独立审阅](verification/transfer-account-bank-review-2026-10-23.md)确认P2静态修复，无额外确认P1/P2；审阅者未运行测试，运行证据来自主任务。

冻结迁移SHA256：44=`0E9AD39C01F523A2D731BDAAA41474924D6DA71439043F3B2995D5DC47AE1EF0`；45=`35330E2B564469B646419F5AC964FCB606A20A6E62F9B971DD13A3CFA5A20130`。没有真实资金、生产访问或完整Pro兼容签署。

## 最新43迁移统一验证

| 检查 | 实际结果 |
|---|---|
| Windows全库真实PG测试/vet/cmd构建 | 全部exit0：payment305.369s、contract205.470s、backoffice85.337s、identity62.485s等。测试与Linux并行，时长不是单独容量基准。 |
| Linux Docker全库race+vet | 全部exit0：payment272.224s、contract189.782s、backoffice67.989s、identity64.950s等。 |
| 全量之后的小幅修复 | 真实PG红测发现结算列表遗漏既有payout，导致UI仍显示尚未核对；补返回原payout，Windows相关BO8.561s/contract5.455s及vet通过，Linux两包race8.194s/5.276s及vet通过。镜像已包含修复，不冒称该修复后再次全库运行。 |
| 前端单元/构建/mock | 63项unit/component、类型检查及生产构建通过；66项mockbrowser17.2s通过、7项live默认跳过。构建DLTtWYQd。新增串行live测试后仅改测试脚本，另typecheck通过。 |
| 实际本地浏览器 | 原7项live整组57.1s通过（含74个partial原菜单巡检、钱包/批次、申请/拒绝）；新增管理员资金密码设置→逐动作确认→钱包结算付款独立live通过2.3s（套件3.4s）。 |
| 新live失败事实 | 首次新测试错误地向Web8181发送/mapi.php而收到HTML；改用明确回环API8180后通过。没有修改支付机构协议或降低断言。只在sandbox运行，保留财务测试流水；恢复全部修改的政策并禁用临时测试管理员。 |
| 原PHP SDK互操作 | 临时密钥、禁网PHP容器：compat2.286s、adapters4.992s通过。 |
| 本地Docker | 原38库原地应用39～43，无清库；API/PostgreSQL健康、Worker/Web运行。部署迁移43 SHA256 D3B73C24584490A872100B2D2AF2A5172A638FFE1F539DCE0EF5E6C58DEA765A。 |
| 冒烟/恢复/账本 | 基本身份/CSRF/幂等及两simulation支付回调/退款、Nginx代理均通过；43库备份至随机新库恢复，journal/account/专属结算funding/批次授权subset异常均0；CLI reconcile全部报告项0。 |
| 文档/源码 | OpenAPI离线检查71paths/81operations。固定源码检查点43-final-94ad4b30-bf8b-4274-8ca4-5fd4fafa4f23含546文件逐项SHA256验证，仅源码，不含.env/.secrets/原PHP。后续测试脚本/文档更新见最新manifest。 |

实际浏览器报告：[原7项](../web/playwright-report/runs/2026-10-23T04-38-24-690Z/index.html)、[新增资金授权结算](../web/playwright-report/runs/2026-10-23T04-40-01-683Z/index.html)、[mock66项](../web/playwright-report/runs/2026-10-23T04-32-25-792Z/index.html)。数据库故障/渠道协议仍以临时PGschema和本地TLS测试证明，没有真实支付机构资金联调。

39～42的独立审阅已关闭已发现P2；43钱包资金流主任务复核并补HTTP/list/report/CSV红绿及统一回归。其它代理本轮后续因workspace credits不足退出，43未完成额外的独立钱包实现审阅，不冒称独立签署全部功能。

## 39～42开发期专项记录（已由上方43验收覆盖部署）

39结算政策快照在跨日修复前已完成Windows全库PG测试/vet/cmd构建、Linux Docker全库race/vet，前端42项单元和58项mock通过；7项live依赖部署而默认跳过。独立审阅随后发现日次数与事务起始时间跨午夜不一致，补真实PG可控时钟及账户锁等待红绿，修复为资金锁后固定一次operationAt，计数上下界及created_at使用同一业务日；修复后payment13项17.723s、backoffice3项4.974s与两包vet通过。该记录不能冒充修复后已重跑全库。

40结算专属预留/加密收款快照、41管理员独立资金凭据及动作授权、42批次授权固定明细正在接线。主任务的资金事务guard专项及批次原回归共25.598s通过，覆盖授权拒绝无资金/意图写入、后续业务失败回滚授权标记、同号幂等不二次授权、授权扣款/解除、押金/会员/收入冻结和解冻、批次准备一次确认及执行固定子集、取消竞争、unknown不重发。尚未以这些专项替代新增代码的全库、真实HTTP和Docker验收。

另以真实PG重现旧结算设置姓名进入不可变审计，修复通用audit过滤后相关3项回归4.938s通过；后续40将旧敏感设置入口切换到加密收款配置，需按最终接口补充验收。

## 最新38迁移统一验证

| 检查 | 实际观察结果 |
|---|---|
| Windows全量 `go test ./... -count=1`，真实PG | 全部通过；payment192.103s、contract147.559s、enrollment52.106s、ordersubject11.397s等；随后全库vet和cmd构建exit0。后续仅测试断言/模拟AppID增量另定向通过。 |
| Linux Docker全量race与vet，真实PG | 修正测试中的不合理完成量断言后全量通过：payment170.369s、contract136.247s、enrollment45.235s；全库vet exit0。模拟AppID的后续三包race与全库vet再通过。 |
| T+1并发失败调查 | 首轮race失败为成熟资金仍余50分，而非超支；退款预占到确认之间，一次性出账可保守拒绝。已补阶段检查：并发累计不得超过1000分，余额/预占严格对应；确认后可用余款全部可预占。新增明确pending/confirmed回归，Windows两测试count5通过19.368s。未放宽生产资金逻辑。 |
| 原PHP SDK互操作 | 临时密钥、禁网PHP容器：compat2.148s、payment adapters9.578s通过。 |
| 前端 | 41项unit/component通过；类型检查和构建通过；完整mock56项通过12.3s，7项live默认跳过。 |
| 本地真实浏览器 | 7项已验证：首轮6通过，包括74个partial原菜单只读巡检；批量新增联调暴露模拟通道缺少AppID后，补synthetic身份及单元红绿，付款项定向重跑通过2.1s。包含钱包付款、整批预留、仅执行1项、取消剩余、停写后查原付款与恢复配置。没有真实资金。 |
| 本地部署 | 原运行库18→38迁移成功，API/PostgreSQL健康、Web/Worker运行；未重建或清空数据库，未修改已冻结迁移。 |
| 冒烟 | 基本登录/租户/CSRF/MD5/幂等入账/退款预占通过；微信及支付宝simulation下单、付款页、重复回调一次入账、原号退款一次及已付关单拒绝通过。Nginx配置/代理/收款授权/日志不含查询token通过。 |
| 备份恢复 | 38迁移运行库备份到随机临时新库并恢复，空/不平衡凭证和账户投影异常均0；只删除本次临时库及dump。CLI reconcile报告invalid_journals=0、invalid_accounts=0，未代替渠道账单核对。 |
| 脱敏迁移 | 最新真实PG importer回归3.364s通过；示例只读校验valid=true，明确保留unknown_refund_requires_manual_provider_mapping警告。 |
| 依赖扫描 | npm全部/生产依赖均0项已报告漏洞；govulncheck v1.9.0/Go1.27.3为0可调用、0导入包发现，1项仅模块层未调用告警GO-2026-5932（x/crypto v0.58.0）。不是绝对安全证明。 |
| 测试资料清理 | 仅禁用精确匹配id3且username=live-operator-1792716414764的旧测试管理员，删除精确名称live-collection-1792716304200的旧样式；未删除财务流水，未清空登录限流。 |

专项设计及覆盖：[批量代付](design/durable-transfer-batches.md)、[订单名称](design/upstream-subjects.md)、[CSV/代理/统计](verification/csv-proxy-report-2026-10-23.md)、[动作差异台账](verification/backoffice-action-coverage.md)。独立审阅关闭了33/34/35/36及CSV/代理/退票统计的已发现问题；37/38后半段由主任务完成集成和回归，不冒称已完成额外独立审阅。

完整mock报告：[2026-10-23T03-37-10-054Z](../web/playwright-report/runs/2026-10-23T03-37-10-054Z/index.html)。浏览器故障及修复重跑报告均保留；不把首次失败隐藏为一次全绿。

## 环境与隔离

Go 1.27.3（Windows/amd64 与 Linux/amd64）、Node 24.19.0、Docker 29.8.2、Compose 5.5.1、PostgreSQL 18.4。Windows 宿主通过模块 toolchain 自动选择 Go 1.27.3；Linux race 使用带 GCC 的 `golang:1.27.3`。

资金/身份/契约测试使用随机独立 PostgreSQL schema，不清空运行库。支付机构协议使用 httptest、临时 RSA 密钥/测试证书及本地 TLS/mTLS；原 PHP SDK 直接在禁网 PHP 容器执行。未读取生产密钥用于测试，未访问生产系统、未发起真实资金交易。Compose 默认 simulation，测试脚本在变更本地资金前检查回环 URL 和 `sandbox=true`。

## 早期18迁移快照（历史记录，非最新结果）

| 检查 | 已观察结果 |
|---|---|
| Windows `go test ./... -count=1`，实际 PG | 18 迁移冻结快照全部通过；后续19～24增量持续进行定向PG回归，最终统一回归待冻结后执行 |
| Linux `go test -race ./... -count=1 && go vet ./...`，实际 PG | 18 迁移冻结快照全部通过；新增增量最终重跑待执行 |
| 原 PHP SDK 互操作 | V1/V2、支付宝 APP 参数、微信 APP/OAEP 等对应增量通过，临时测试密钥、PHP 容器禁网 |
| `go build ./cmd/...` | 18 快照通过；新增能力统一构建待执行 |
| 前端类型检查/生产构建 | 通过；最新源码构建 `BUjNndjb`，最终 Docker 重建待执行 |
| 前端单元测试 | 21 项通过 |
| Playwright 网络替身 | 27 项通过，7 项真实环境测试默认跳过；4 worker 的稳定复跑通过 |
| 实际 Docker 浏览器 | 已有6项通过；新增真实钱包付款及最终统一7项复跑待执行 |
| 菜单只读巡检 | 74 个 partial 页面通过；82 个原菜单叶子保留，6项pending未实现、2项其他插件deferred，不计入已完成业务验收 |
| npm audit（全部及生产依赖） | 均为0项已报告漏洞；不是绝对安全证明 |
| govulncheck v1.9.0 | Go1.27.3扫描0项代码可调用漏洞、0项已导入包漏洞；1项依赖模块中的未调用告警，保留该区别 |
| 本地备份恢复 | 已将18迁移运行库备份至本轮随机新建库并恢复；空/不平衡凭证、账户投影异常均0；只删除本轮临时恢复库和临时dump |
| 脱敏历史迁移 | 实际PG演练通过；相同导出重跑不增加余额，历史paid不重入账，未知退款保留冻结，晚期失败全事务回滚 |
| 最新部署/冒烟/账本检查 | 最终增量冻结后统一执行；之前运行库为18迁移，API/Worker/Web/PG运行 |

首次漏洞扫描编译遇 C 盘空间不足，已保留失败事实；改用 E 盘忽略目录作为编译临时目录和缓存、固定 Go1.27.3 后扫描成功，没有清理用户系统盘。浏览器首次集合清理缺少重新认证、管理员编辑重复发送只创建用户名的问题已修复并定向复跑通过；一次高并发 Vite 加载失败在限制4个worker后全套复跑通过，失败报告保留。

## 资金、协议和安全回归

- 精确金额、溢出/舍入、费用快照不随配置改变；累计退款分摊、失败空洞、并发防超退和防透支。
- 平衡不可变分录、历史凭证禁止追加、账户投影、状态不可逆约束；CheckLedger 检出故意注入的空凭证/余额损坏，损坏仅在随机测试schema。
- 微信/支付宝签名请求、响应节点验签、身份/应用/订单/金额/币种匹配；本地 mTLS、RSA-OAEP、原 SDK 互操作。
- 可信回执与账本同事务、同上游流水不能支付两单；重复回调仅一笔入账。原始客户 IP 只写一次；真实来源和签名买家 IP 各自执行黑名单策略。
- 查单/列表 UTC+8、真实上游流水、买家标识、成功部分退款金额及全额退款状态；V2 实际响应验签、跨商户元数据不可见。
- 未知支付/退款/转账不重发；明确失败才释放；核对任务在I/O前持久化，租约/失效Worker隔离、死信及受限只读重试。
- 转账额度、默认备注、收款索引 HMAC、跨通道/租户、并发日限额、明确失败释放名额、未知跨日继续占用。
- 付费会员到期、基础组迁移的旧完整快照歧义、18迁移后管理员修正保护、显式同组确认、幂等重跑。
- 后台动作写意图审计失败阻断上游；凭证、密码、账号、密钥不写资金动作审计。密码/MFA/权限变更并发旧认证不能生成新会话。
- CSRF、角色/租户、TOTP重放、最后超级管理员、域名修改重新审核、充值前置校验、SSRF/重定向/混合DNS/私网防护。

银行退票与预授权仍处于独立增量回归，不能把此前测试结果当作这些新功能的最终验收。各分支状态见 [方法映射](design/two-plugin-methods.md)。

## 复跑命令

```powershell
Set-Location -LiteralPath 'E:\GOEPAY'
$env:GOTMPDIR='E:\GOEPAY\.tools\go-tmp'
$env:GOCACHE='E:\GOEPAY\.tools\go-cache'
$env:GOEPAY_TEST_DATABASE_URL='<本地隔离测试 PostgreSQL DSN>'
go test -p 2 ./... -count=1
go vet ./...
go build ./cmd/...
$env:GOEPAY_PHP_INTEROP='1'
go test ./internal/compat ./internal/adapters/payment -count=1

# web目录：npm test; npm run build; npm audit; npm audit --omit=dev
# GOEPAY_LIVE_E2E=1 与 GOEPAY_WEB_URL=http://127.0.0.1:8181 才运行真实浏览器项
node scripts/smoke.mjs
node scripts/provider-smoke.mjs
node scripts/proxy-smoke.mjs
./scripts/restore-test.ps1
docker compose run --rm --entrypoint /app/cli api reconcile
```

Linux race 容器仅复制固定allowlist源码、使用 Docker 内部本地测试 DSN 后执行 `go test -race -p 2 ./... -count=1 && go vet ./...`。本地PG锁资源有限，测试脚本默认包并发2，不修改数据库参数。不要省略测试数据库环境变量，否则 PostgreSQL 测试会跳过。

未执行生产流量回放、真实资金、真实商户产品权限/设备SDK调起、生产切换、原版全部动态按钮与加密语义验收、与旧 PHP 的容量对照。基准测试仅为单机单客户端数据库微基准，不能推断整体吞吐或声称更快。
