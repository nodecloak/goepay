# GOEPAY 接口兼容规范与差异清单

日期：2026-10-23。与 [总体设计](2026-10-23-goepay-design.md) 配套。本文件是设计约束，不是已通过兼容测试的证明。

## 1. 证据优先级与兼容画像

Pro 运行行为决定目标版本行为，Pro 文档/SDK/明文路由用于建立初稿，Release 明文实现用于交叉检查与提出假设。冲突时记录原始请求和响应并建立固定测试，不能以某一版本文档自动覆盖另一版本事实。

采用按来源和商户配置的兼容画像：`pro-v1`、`pro-v2`、必要时 `release-legacy`。画像由服务端配置与入口决定，不由未验签用户字段任意切换。对同一地址不同版本的歧义，可按迁移商户保存来源版本；仍无法区分时必须先确定策略，禁止偷偷同时改变输出字段名和金额语义。

所有测试样本保存：来源版本和哈希、路由、方法、原始 query/body、Content-Type、固定时间、身份配置、数据库初始状态、上游返回、HTTP 状态/headers/body、资金后置断言。动态订单号和时间只在允许的规则下规范化，先验签再比较；不能把签名、金额或状态从断言中全删掉。

## 2. 对外路由清单

以下列出 `epay_pro/route/api.php` 全部 22 条路由；`ANY` 是路由声明，不代表所有 HTTP 方法的实际业务行为都已验证。

| 路径 | 声明方法 | 契约重点 |
|---|---|---|
| `/api/pay/submit` | ANY | 页面支付，文档 GET/POST；缺 type 进入收银台 |
| `/api/pay/create` | ANY | 统一下单，文档仅 POST；返回 pay_type/pay_info |
| `/api/pay/query` | POST | trade_no/out_trade_no 选择、订单归属、状态映射 |
| `/api/pay/refund` | POST | API 开关、部分退款、可选 out_refund_no、余额扣减 |
| `/api/pay/refundquery` | POST | 两种退款号、退款状态及扣减金额 |
| `/api/pay/close` | POST | 插件能力、支付与关闭竞态 |
| `/api/transfer/submit` | POST | 开关、可选 out_biz_no、费率、处理中结果 |
| `/api/transfer/query` | POST | biz_no/out_biz_no、0/1/2 状态、errmsg 条件返回 |
| `/api/transfer/proof` | POST | 路由存在，无独立本地文档，需运行样本 |
| `/api/transfer/balance` | POST | available_money、transfer_rate |
| `/api/merchant/info` | POST | status/pay_status/settle_status、余额、统计、结算资料 |
| `/api/merchant/orders` | POST | offset/limit，最大 50，状态过滤 |
| `/api/complain/image` | GET | 无统一 ApiVerify 中间件；具体访问保护需核实 |
| `/api/complain/list` | POST | page_num/page_size、日期、状态、data.total/rows |
| `/api/complain/detail` | POST | showtype 与渠道扩展字段 |
| `/api/complain/upload` | POST | multipart/form-data，文件不参与签名，image_id |
| `/api/complain/feedback` | POST | showtype 特定结果码，images 不签名，complete |
| `/api/complain/reply` | POST | 回复内容、图片数组、资源归属 |
| `/api/complain/refundprogress` | POST | 审批反馈并不自动代替订单退款 |
| `/api/complain/complete` | POST | 微信投诉完结条件 |
| `/api/complain/supplement` | POST | 路由存在，无独立本地文档，需运行样本 |
| `/api/obsolete` | ANY | 旧 act 分发、pid/key 认证，不能统一当 RSA |

必须保留的 PHP 主入口：

| 入口 | Pro 明文入口映射 | 结果约束 |
|---|---|---|
| `/submit.php` | 定义 API_INIT_OLD，进入 api/pay/submit | MD5 页面支付和缺 type 收银台 |
| `/mapi.php` | 定义 API_INIT_OLD，进入 api/pay/create | 老格式 code=1，payurl/qrcode/urlscheme |
| `/api.php` | 进入 api/obsolete | 旧 act=query/settle/order/orders/refund 等，实际 act 集合需补样本 |

Release `api.php` 还存在其他内部 act 分支，不应直接暴露到新公网上；分别记录用途、认证与是否属于真实对接依赖。Release Nginx 将 `/api/*` 重写到 `api.php?s=...`，Pro 的 PHP 主入口则直接重设 s；这些差异须按来源版本验证。

其他公开接口/页面：`route/app.php` 的 33 条静态路由、`route/user.php` 中登录授权和 OpenID 等公开流程、`public` 入口和模板、`/pay/...` 网关、聚合收款、红包/微信转账确认、语音、QQ webhook、小程序收银台，全部从 routes.csv 逐条分类，不能仅测试上述 22 条。

## 3. 协议不可随意改变的细节

### 3.1 通用请求和签名

- 旧支付请求使用 UTF-8 表单；投诉上传为 multipart。不能把原请求改成仅收 JSON。
- 待签名字段按键 ASCII 顺序连接为 `k=v&k2=v2`；排除 sign/sign_type，不对值先做 URL 编码。保留原始字符串进行验签，不能把 money=1.0 格式化成 1.00 后再验签。
- V1 为小写 MD5(待签名串 + 商户 key)；V2 为 SHA256WithRSA，SDK 使用标准 OpenSSL RSA 签名与 Base64。Go 实现需与 SDK 的 RSA PKCS#1 v1.5 行为验证一致，不擅自改 RSA-PSS。
- 实施复核：Pro `app/lib/Payment.php:14` 调用 `app/common.php:311` 的 `isNullOrEmpty`，仅排除 null 和严格空字符串，保留纯空白值；SDK_2.0.zip 的 `EpayCore::isEmpty` 则排除 PHP trim 后空字符串。两者存在真实差异。`internal/compat` 显式提供 `ProfileProServer`（默认）和 `ProfileSDKV2`，不自动尝试多种规则。字符串 `"0"` 均保留；参与签名的非空值不 trim。
- PHP 对重复参数、数组语法、空值、`+` 与 `%20`、百分号转义、字段名大小写、GET/POST 混合的解析必须测出实际规则；Go 的 `ParseForm/Get` 结果不能直接假定等价。
- 认证算法必须受商户 keytype 限制；不能因请求将 sign_type 改成 MD5 就绕过 RSA-only。
- 数组/文件不参与旧签名时，仍对对象归属、内容格式、数量和用途做验证；签名兼容不等于该部分数据可信。

### 3.2 返回与支付材料

V1 `/mapi.php` 成功 code=1；V2 成功 code=0；旧 `api.php?act=refund` 文档同样是 code=0，不能机械将全部 V1 成功统一成 1。旧错误不自动改成后台 HTTP 422 JSON envelope；HTTP 状态、msg、字段类型分别冻结。

V2 `pay_type` 至少覆盖 jump/html/qrcode/urlscheme/jsapi/app/scan/wxplugin/wxapp。`pay_info` 文档定义为字符串，JSAPI 等可能是 JSON 字符串，不能改成对象破坏 SDK/客户端。V1 三种支付材料字段通常仅返回一种，必须维护行为。

V2 method：web/jump/jsapi/app/scan/applet；device：pc/mobile/qq/wechat/alipay/douyin。V1 device 还有 jump。支付类型不能只硬编码支付宝/微信，应保留配置化列表以及银行、QQ、京东、PayPal、抖音等原定义。

支持 merchant_id、channel_id、fee_mode、cert_no/cert_name/min_age、sub_openid/sub_appid/is_applet、auth_code。签名成功后必须校验请求商户是否有权使用该进件商户/子通道，不能仅以 ID 存在为条件。

金额外部使用字符串；商品名文档标注 127 字节截断，数据库 name 却为 varchar(64)，需要核实截断点与 UTF-8 边界。不能在验签前改变原商品名。param 原样回传，不能二次 JSON 序列化。

### 3.3 时间戳与通知

V2 SDK `verify` 明确要求 timestamp 存在且时间差 ≤ 300 秒。Release `ApiHelper::api_verify` 对 API_INIT 或 forceRsa 情况采用相同窗口；Pro 服务器窗口需运行验证，不把 SDK 规则当成服务端已读到的实现。

异步 notify_url 和同步 return_url 文档均为 GET；trade_status 固定成功值 TRADE_SUCCESS；异步 ACK 是 success。ACK 是否允许空白、大小写、子串或非 200 状态需对原系统验证。

重试刷新 timestamp 与签名，业务订单时间不变；签名对额外非空扩展字段应保持兼容。不能只固定签几个已知字段。同步跳转即使验签成功也不能在平台内部触发新的支付成功记账。

## 4. 已发现的差异与必须执行的验证

| ID | 已知事实 | 结论 / 验证方法 |
|---|---|---|
| C-01 | Pro create 文档 POST，路由 ANY | 测 GET/POST/PUT 等实际结果；根据生产调用需求和真实行为确定兼容，不凭路由放行所有方法 |
| C-02 | Pro submit 文档 type 标必填，但说明允许不传 | 缺失、空字符串分别测试，确认收银台展示和可选方式 |
| C-03 | Pro V1 文档 order_today/order_lastday；Release api.php 为 orders_today/orders_lastday | 按版本保存实际字段；Pro 需样本；不能盲目同时增字段导致签名/严格客户端变化 |
| C-04 | Pro V1 orders 文档 page 默认1/limit20；Release 为 offset 默认0/limit10 | 测空参、page、offset、都传、边界和最大50；确定每个画像 |
| C-05 | V2 支付状态 0～4；V1 文档只描述0/1但 Release 原样输出状态 | 退款/冻结/预授权订单做查询与通知回归，禁止一律映射成“已支付” |
| C-06 | V2 create 参数表有签名与时间，示例未完整给出；SDK 成功响应必验签 | 以完整可验签响应作为接入测试，不照抄不完整示例 |
| C-07 | transfer/proof、complain/image、complain/supplement 有路由无完整独立文档 | 原界面/构建资源/运行样本取出请求响应和认证规则 |
| C-08 | `/api/pay/submit`、create 无统一 ApiVerify 中间件 | 不代表不验签，支付入口自有验证必须对照；全部新支付入口均需按协议鉴权 |
| C-09 | V1 SDK `$v!=''`，V2 SDK 使用 trim 判空 | PHP 运行版本、空格、0、false、null、数组分别跑互操作用例 |
| C-10 | Release 通知注释为1/3/20分钟/1/2小时，代码后续间隔2/16/36分钟/1小时 | 注释与累计时间存在差别；测试调度实际时间，Pro 另测；不可根据注释承诺重试时刻 |
| C-11 | 旧退款/转账可无外部幂等号 | 保留可选性，记录无法完全辨识客户端重复意图的限制；平台重试使用稳定内部号 |
| C-12 | Pro 路由配置大小写不敏感、非完全匹配、无去尾斜杠 | 测大小写、尾斜杠、多余路径、错误页/JSON；仅复制有业务依赖的路由兼容，不扩大未认证面 |
| C-13 | 前端页、按钮和 JSON 菜单可能按配置/角色动态变化 | 全角色 + 全相关开关组合建立页面快照与操作清单 |
| C-14 | 产品应用市场、在线更新与原厂服务相连 | 每个动作的服务契约及可替代性要验证；不得返回虚假“安装成功” |
| C-15 | Pro 核心加密，安装 SQL 字段不能说明真实算法 | 费率优先级、退款退费、冻结金额、分账计算、负余额和结算日必须采样 |

## 5. 必备契约与资金测试矩阵

| 测试组 | 具体用例 | 通过条件 |
|---|---|---|
| 签名 | 乱序、未知扩展字段、空串、空格、0、中文、emoji、+/%、数组、文件、错误key、RSA-only降级 | 与来源 SDK/原系统结论一致；错误签名无副作用 |
| 请求解析 | form/multipart、GET/POST、重复键、长字段、缺字段、大小写与尾斜杠 | 契约固定，不出现前后两次解析值不同 |
| 下单 | 同号同参并发100次、同号不同金额、不同商户同号、外呼超时 | 同商户一个有效业务订单；上游不重复受理；冲突可见 |
| 收银台 | 缺type、每个method/device、二维码过期、扫码成功、取消、预授权 | 返回材料与页面可用，最终状态与查单一致 |
| 回调 | 错误签名、错商户/金额、重复/乱序、已关闭后晚到成功、事务失败 | 不错误入账；晚到成功必须查证并记录真实资金责任，不能直接丢弃 |
| 通知 | 首次成功、超时、非success、服务重启、300秒以后重试 | 订单不丢、可重试、V2新签名可由原SDK验证 |
| 退款 | 同退款号并发、两笔部分退款竞态、超额、无外部号、成功响应丢失 | 预占+成功不超实收；查证恢复，无重复退款 |
| 代付 | 同号重试、资金不足、支付密码错误、批量部分失败、unknown | 预占正确；unknown不释放；批量逐项报告 |
| 结算 | 同商户并发提现、批次重跑、在途取消、扣费上下限 | 不超可用；每笔出款与费用有凭证 |
| 分账 | 多接收者、尾差、重复执行、部分成功、退回、站内与渠道模式 | 逐笔明细、总额不超可分；失败可查证 |
| 业务类型 | 普通订单、付费注册、余额充值、聚合收款、会员购买、保证金 | 账务和附带权益只生效一次 |
| 越权 | 商户A读/改B订单、附件、投诉、子通道、导出；管理员缺权限 | 后端拒绝，无信息泄漏或资金变化 |
| 进件投诉 | 每种showtype/表单版本/附件归属/签约/回退重提/补充举证 | 能力对应，审批反馈与真实退款分开 |
| Worker | 提交前/后崩溃、外部成功后崩溃、租约过期双Worker、消息重复 | 无丢失资金事件，外部操作可查证、内部恰好一次生效 |
| 迁移 | 零日期、重复号、NULL/空串、负余额、待处理款、跨日 | 冲突可报告与修复；分桶金额和期初账一致 |

测试环境使用原版合法实例、模拟通道与沙箱。真实出款/退款不得因差分测试双发。只读影子可双查；资金写操作必须各自独立隔离数据与上游环境。

## 6. 基线台账使用方法

`menus.csv` 标识菜单，不代表完整需求。`routes.csv` 标识每个可见静态动作，包含源码路径/行号、控制器和加密标记。新增 `feature_id → menu_id / route_id / setting_key / job_name / plugin_capability → implementation → testcase → result` 的验收映射；对 `operation/merchant_op` 等分发接口继续拆到 action 子操作，不能一条路由只测一个默认分支。

所有状态必须使用“已定位”“已确认契约”“已实现”“已通过”四级，缺材料单独标注阻塞原因。当前生成文件统一是已定位，不自动把路由数或菜单数当成完成率。

## 7. 首批兼容实现边界（2026-10-23）

`internal/compat` 实现标量表单验签、MD5、SHA256WithRSA/PKCS#1 v1.5、PEM/裸 Base64 DER 密钥解析、商户算法限制、秒级时间戳边界校验。`Parse(url.Values)` 保留解码后的原值，对重复键、PHP 数组字段、点/空格归一化名称及 NUL 名称明确拒绝。这是消除解析歧义的安全边界，尚未证明与 PHP 对畸形请求完全一致。投诉图片等无签名数组须由类型化端点独立解析并校验归属。

RSA 密钥至少 2048 位；旧商户弱密钥需要迁移。`Verify` 使用商户配置算法，未知算法不回退到 MD5。RSA 默认接受当前时间前后 300 秒（包含边界），该窗口来自 SDK/设计，Pro 服务端黑盒行为仍待取证。

单元测试通过仅证明该包约束；原服务端黑盒和真实通道契约仍须补充，不能把源码级实现视为完整兼容验收。
