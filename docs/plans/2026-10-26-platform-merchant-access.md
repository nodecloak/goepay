# 179 平台商户密钥登录与退款门控：源码契约和实施计划

父冻结176，1966文件，SOURCE_CHECKPOINT SHA256 a6e34d2ad9f9987df11ba2595f18ec4ae01afd75e7867a0a7eb27ef8033de3c8；新owned work见globalaccess179-path.txt，克隆逐文件核验完毕。无迁移，不混178/171、98或其它分支，不改主目录/冻结/Git；只本地合成HTTP/PG/浏览器。

## 原证据与范围

- Pro app/sql/install.sql:62 user_refund='1'；该文件无close_keylogin，兼容缺省0。Pro settings index-C6la0DtP.js SHA503256befdb92f1bcba8c0f4dfb0e6b77c0648e25ac47454ed555c35891a1ee0，merchant-settings模块user_refund控件字符串active1/inactive0；加载前JS初值0不是DB缺省值。
- Pro个人 index-fMgyEUZ4.js SHA87b9217c066343aa61d67e4cd58866fc5a85dc661aeff50a8a7996ca4ea1d48e：close_keylogin真时隐藏个人keylogin；user_refund假时隐藏个人refund。原订单 index-Dk-66xD5.js根据sys.user_refund显示自助退款。
- Release user/ajax.php:73平台close门控与个人keylogin交集；admin/set.php:133 密钥登录value1关闭/0开启，在site网站信息tab。Pro admin编译settings模块未发现close_keylogin控件，不能声称已找到；以原Release位置恢复site tab。Pro user_refund原merchant-settings，恢复商户相关tab（Release旧pay tab的商户支付功能区也有此字段）。
- Release api.php:118、includes/lib/api/Pay.php:563/601平台user_refund门控。与旧代码在refundquery也禁查不同，本任务按root明确要求保留已接受unknown、同ID、查询/回调恢复；不能因关闭开关丢失已执行资金状态。管理员授权/内部业务退款不受商户平台门控。
- 不实施买家加费、费率拆分、104或其它平台设置；不改172 QQ/min/max、174个人九字段strict DTO、163签名政策。

## 数据与接口设计

原两条settings行继续使用key/value，value只能JSON字符串"0"或"1"，沿原backoffice_setting_unique唯一约束。新typed读写使用原全settings advisory锁；generic创建/修改/改名/删除这两key全部拒绝，避免绕过事务和CAS。Admin UI原tabs用独立受控字段面板，不把旧generic PUT的前置GET当原子CAS。

不加SQL表/列。用现有settings中受保护内部key __platform_merchant_access179（value仍为严格字符串、generic GET/List/写/改名/删均隔离）保存单例完整状态MAC/revision/两settings行ID；所有读取要求0/1行、当前MAC与独立append-only审计次数等于revision。与两原settings行同事务写入，audit只非秘密字段名/值/版本。缺settings行、缺内部head、重复/非法值或合法旧head回滚但保留审计均冲突，不恢复默认。真实无历史配置且无来源默认close0/refund1；已有原合法plain settings仅作为未升级兼容值，首次typed保存建立锚点，不重写历史证明。

- GET/PATCH /console/v1/admin/settings/merchant-access：仅原super、无委托；GET data四字段 {close_keylogin:"0"|"1",user_refund:"0"|"1",fingerprint:64hex,can_update:true}。PATCH严格完整三字段 {close_keylogin:"0"|"1",user_refund:"0"|"1",expected_fingerprint:64hex}；禁止query/空?/重复未知字段、数字/布尔值，no-store、CSRF、同事务审计/最后DBclock复核，旧指纹409无重试。
- GET /console/v1/public/merchant-access：只返回严格两数字字段{close_keylogin:0|1,user_refund:0|1}，no-store、无凭据/商户信息/私有proof。成功才表示已知，损坏/读取失败503；前端不能把unknown假定为关闭或开启。
- 174个人九字段DTO不变。个人面板另读公共能力，按真实平台flag隐藏原两开关并明确说明，保留个人值；公共登录仅在已知允许时显示key模式，PW始终可用。商户订单refund能力在后端按真实平台状态投影并实际执行，不能只禁前端按钮。

## 实际消费者与锁

1. PrepareMerchantKeyLogin在merchant→全局shared锁→个人policy→durable user下检查平台+个人；grant私有绑定原全局revision/摘要。Complete重新相同锁序核平台、旧grant和credential/MFA版本，写session/audit后最后DBclock复核。关闭只阻止新key-login，PW/已有session/API签名支付不受影响。
2. 新API refund ReserveAPIRefund与本人自助RefundFromMerchant共享平台门控；个人refund仅API路径交集，平台user_refund同时约束API和本人自助。管理员/内部Refund原路径不受影响。
3. reserve已有真正已接受provider operation/终态按原恢复；内部ReserveRefund仅占了相同ID但尚无operation不得借来源首次发送。第一次refund provider_operation的事务内再次检查当前平台及所需个人政策，不能在reserve→operation的间隙绕过关闭。既有operation优先返回原状态，不重新发送、不改原金额/quote/账户/回执。
4. 全局setter沿原advisory exclusive→原super user/session；资金/登录沿原merchant/order锁→global shared→user（只在各原授权路径需要时）。最后写/等待后clock保持；真实并发锁序测试而非调整超时掩盖。

## TDD与交付

先实际RED：原路径缺失、close1仍可新key-login/原grantComplete、user_refund0仍可新API/本人refund；内部预留无op首次send、reserve/firstsend间关闭、credential锁等待关闭、lastclock过期竞争；owner/super/CSRF/strict字符串/CAS/首次配置与普通settings写锁竞争、审计失败全回滚、删除/旧合法头恢复failclosed。

正例：默认/显式允许、PW/旧session/签名支付；管理员授权/内部业务；旧unknown同ID/query/callback单次send；个人1不越平台0、个人0仍阻API；同值保存及QQ/profile无关更改不误挡，完整大ID/无秘密DTO。

实际HTTP导出销毁后合成public/admin DTO fixture供严格前端模型与PW共享；UI含原tabs/0-1方向、unknown刷新、不盲重试、raw clientCSRF/epoch/abort/迟到响应、1440/375。按最新源before/after核SHA，保留所有原RED/中途失败，交完整原/新路径和收据及明确限制，独立复审后由root整合。


实现验证调整：原backoffice_records_resource_check拒绝新resource，最初typed-storage失败日志完整保留；没有放宽CHECK/增加迁移，改用原settings唯一索引约束的受保护内部key。该value是规范JSON编码字符串，原两业务value仍严格0/1字符串。实际generic DELETE原先200删除gate的RED另行保留，修复后403且typed状态不变。

## 182 独立复核发现与修复

179 原稳定源及全部失败/通过收据保持不动。真实HTTP竞态补充断言后，明确复现 gate已关闭且operation/send均0，但退款pending、available900/reserved100，公开query缺operation无法恢复（globalaccess182-unsent-reservation-red）。该根因是两个durable事务之间的拒绝仅返回错误，没有终结未发送预留。

182 在原 order→refund 锁和现有operation优先恢复逻辑内，仅当当前为pending、当前准入明确ErrMerchantAPIDisabled且不存在任何refund operation时，本地failed、释放原资金来源、写原internal keyed result证据并同事务提交；不新增表、不修改旧MAC或旧回执。既有claimed/accepted/unknown永不走本地释放。原已终结且没有operation的过期调用，禁止新claim；原实现确实能为failed预留建立新op，actual overlay RED另留。

验证：原reserve关闭竞态现为failed、available1000/reserved0、query可读、CheckLedger通过；claimed赢家provider阻塞期间关闭两层政策，重放仍unknown/保留900+100且send1，释放模拟丢失响应后原查询恢复成功send仍1；内部终态stale调用不能重新claim。注入result证据写失败时，failed/status/释放journal全回滚，原pending预留完整；恢复配置后同ID可以首次发送一次及查询完成。所有网络仅本地合成，不访问真实上游。
