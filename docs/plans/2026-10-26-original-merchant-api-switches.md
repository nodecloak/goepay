# 174 原商户个人API开关：范围与实施方案

父：冻结167，1893文件逐文件核SHA后clone，工作区 `.tools/work174-b6d40a88-6904-4c47-a3cc-57f488f66362`。父manifest SHA a861a97e253c05c5408cd59d1caded982a89b15f656ce8283e4aca2c0121771c。独立migration101；98/99/100分别留既定B/C/171 scheduler，不并入。QQ/min/max(172)、买家加费/费率拆分、原keytype算法与所有旧资金证明编码不改。允许本项Go/SQL/前端/模型/测试/OpenAPI；仅本地合成PG和loopback假provider。

## 已核本机原证据与不可证明边界

Pro `app/sql/install.sql:320,325,326`：keylogin NOT NULL DEFAULT1、refund DEFAULT1、transfer DEFAULT0。`public/static/assets/index-fMgyEUZ4.js` 中读取账户API后 `Il.keylogin=e.keylogin; Il.refund=e.refund; Il.transfer=e.transfer_api`，保存原字段keylogin/refund/transfer，开关active-value1/inactive-value0。标签“开启密钥登录”“订单退款API接口”“代付API接口”；全局close_keylogin时隐藏keylogin，全局user_refund/transfer条件控制可见性。JS临时初始值0不等于持久数据库默认值。

Pro `app/middleware/ApiVerify.php` 明文证明现代签名认证读取refund/transfer，但不读取keylogin（故不得用keylogin0关闭所有签名支付API）。Pro Account/Auth/Pay/Transfer及AuthLogic/ApiVerifyLogic/TransferLogic主体均swoole_loader加密；已记录原文件SHA，不伪称这些现代消费者明文已完整证明。

Release明文 `user/ajax.php:69-79` 区分账号密码(type1)与商户ID+key(type0)，后者查全局close_keylogin及user.keylogin；keylogin0不禁密码登录。`api.php:115-125` legacy退款个人refund0拒绝。`includes/lib/api/Transfer.php:8-15,42-49,94-101,119-126` submit/query/proof/balance均查个人transfer与全局/组user_transfer。Release现代Pay.refund仅查全局user_refund，未见个人refund门：Go按用户要求对所有新API退款闭环，不复制此遗漏。现代Pro加密消费者差异明确保留。

## Go兼容与权限决策（root已确认）

已有 `profile.transfer_api_enabled` 是管理员grant，并用于console/manual/batch/settlement及provider发送，个人setter绝不改它或全局/组权限。新增独立个人偏好只取交集。新merchant原grant默认false，对应原transfer0；既有未设置个人policy且管理员grant=true的商户沿用旧已启行为，不能通过升级静默关其原API。个人policy一旦显式设置transfer0/1才固定偏好；true永不把false管理员grant升权。keylogin/refund未配置都按原默认1。

个人开关只控制相应入口，不泛化成商户总封禁：
- keylogin控制专用密钥网页登录认证；账号密码/TOTP登录、原支付签名API与已存会话不被keylogin0静默撤销。
- refund控制legacy `/api.php act=refund`、已签名 `/api/pay/refund` 的**新**退款意图。
- transfer控制已签名 `/api/transfer/submit` 的**新**代付意图；balance无旧资金恢复理由，关闭时拒绝。原query/proof已存在意图可以持原身份读取/恢复。
- 已受理同号/同参数原意图先验证原身份/不可变证明，再返回原状态，不重占、不盲重发、不换账户。仅内部reserve而尚无原provider_operation的退款不能借旧ID绕过关闭去首次发送；原成功/失败终态的只读重放不被关闭误挡。read-only query、provider回调/恢复worker、原退款/转账财务结算不重新消耗个人API授权。即使原Release查询也按flag拒绝，用户本轮明确“关闭不影响已执行资金恢复”优先；此为Go兼容安全决策，不冒充原完全一致。
- 偏好在原reserve事务内、持merchant锁校验，仅新意图分支消费；不能只在HTTP前置读flag留下关闭/新reserve竞态。已提交原意图沿原一次发送/查询状态机继续，不借关闭产生第二次支出。

## API/字段与UI

独立本人原merchant GET/PATCH `/console/v1/merchant/account/api-switches`；不扩展172严格DTO。GET返回 `{merchant_id:string,keylogin:0|1,refund:0|1,transfer:0|1,configured:boolean,revision:string,fingerprint:64hex,can_update:true,transfer_granted:boolean}`。fingerprint绑定完整merchant/config_revision/当前grant/完整个人policy修订和原配置。PATCH严格完整 `{keylogin:0|1,refund:0|1,transfer:0|1,expected_fingerprint:string}`，整数原始JSON0/1（非boolean/string/null/1.0/1e0），未知/重复/query/merchant_id拒绝，CSRF+原非代登录持久会话，无秘密，no-store。CAS409需刷新显式确认、不自动重试；audit非秘密、config_revision+1。GET及写后所有等待最后DBclock复核owner/user/session，锁序merchant→policy→durable user→session，原版本撤销/降权/过期正确序列化。

新增专用 `POST /console/v1/merchant/key-login`，严格JSON `{merchant_id:string,api_key:string,code:string}`；GET/所有query/URL含密钥拒绝，绝不写URL/请求体/key到日志或存储。保留原密码login接口和numeric username语义，不自动把数字username当key。按完整canonical ID找唯一enabled持久merchant账户（多账号映射不任意挑选），恒时验当前解密APIkey；复用现TOTP、原account/source节流和日志。MFA成功后原merchant→policy→user锁内重查key/配置/账户版本/启用，再同事务生成持久session+非秘密成功审计；并发版本revoker先赢则拒发，后赢则新session立刻失效。禁止只有内存伪User或缺有效持久账户时发session。有效RSA-only签名策略不暗改，密钥网页登录与签名算法是独立能力。

个人设置面板单独承载三原开关和明确管理员grant提示，172QQ/minmax表单保持。密钥登录作为商户登录界面明确选项，密钥只临时输入，切模式/提交不确定后清除，复用TOTP栏，不写query/localStorage。busy/context/client/raw-CSRF stamp、迟到响应/精确ID、取消/保存确认、未知不盲重试和375/1440实际浏览器验证。

## 101持久性与旧兼容

新增 `merchant_api_switches`(merchant PK FK、三个CHECK0/1、独立revision>0、private proof_hash32)，正常禁止DELETE，MAC绑定全部偏好/merchant/修订。旧merchant/profile/order/refund/transfer/attempt/ledger/全部旧proof行列原样，不批量回填。缺行且无origin才允许旧默认；有独立append-only audit origin却缺行/全表与101登记丢失不可退回默认。真pre101无schema/登记/origin fixture仍可按原行为。每次读取当前已配置policy还核同商户append-only policy-update审计count严格等于policy.revision，阻止普通UPDATE恢复旧合法MAC行；同值保存也恰好+1，172/密钥/其它config_revision变化不计入policy审计修订。当前stored policy不得被generic商户profile编辑覆盖/提权。

## TDD顺序与实际验证

1. 原HTTP policy GET/PATCH和密钥登录先RED（未路由/未实现），随后最小schema/DTO/session/CSRF/CAS实现GREEN。
2. 通过真实owner会话设置refund0/transfer0后，实际签名退款/转账新意图仍可发送的RED；再接新reserve内原子消费者GREEN。每个off用例有on正例，不能仅错误路由/坏签名导致拒绝。
3. actual PG锁等待：关闭与新reserve竞争、双CAS一次winner、merchant→user→session lock/revoker无死锁、版本/角色/启用/过期/审计失败全部回滚；密钥TOTP缺/错/回放、限流、禁用/轮换/配置关闭时不发session。
4. unknown/已受理同号提交、退款/代付查询/worker/回调在关闭后仍保持原single-send/账户/预留/真实可信财务结果；新号0reserve/0attempt/0send。
5. legacy+V2退款实际签名、transfer submit/query/proof/balance、全ID/严格JSON/query/CSRF/委托/跨商户；无secret响应/日志/审计/DOM持久化。
6. migration101所有旧表每旧列每行投影及真实历史HMAC保全，新表空；相关163/172/退款/转账/会话/TOTP回归，UI单元/浏览器/types/OpenAPI。原始失败日志保留，不削弱断言。

收据prefix merchant174；新建自有loopback synthetic PostgreSQL17/max_locks1024，禁止其它容器/生产/外网/真实上游。最终逐文件167旧/新SHA+完整收据+独立review，root整合；不commit/freeze。

## 真实密钥登录DTO跨层追加

独立发现schema16给所有role的admin_class默认operator，旧key登录读取后原样投影会多发管理员字段，严格前端拒收。先用小ID2及9007199254740993实际HTTP做完整envelope/data/user schema RED，随后仅清商户登录User中无关AdminClass，不改数据库列、持久版本、MFA或前端3keys解析规则。大ID真实成功JSON导出到显式synthetic fixture，仅响应CSRF与三字段user，无cookie/bearer/API key；子schema已实际drop且原session不可用后才导出。model与PW复用同一真实JSON，避免手写mock漏字段。原eb208/604候选、index与失败日志保留为修复前证据，追加版本另出清单/收据。
