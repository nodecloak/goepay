# 原商户签名模式

对照Pro API信息模块index-CgFnG600.js恢复keytype=0（MD5+RSA兼容）、1（仅RSA）的明确设置动作。保留MD5/RSA密钥生成入口，不以生成公钥代替模式切换。数据库未显式设置的旧商户保持signature_algorithm原严格策略，不迁移为宽松兼容。V1兼容模式允许MD5或RSA，V2按本机原SDK和接口文档仍只接受RSA；仅RSA模式拒绝所有MD5及legacy key查询。

95迁移仅新增nullable keytype列及合法值约束，旧数据与历史证明不重写。共享签名验证使用商户持久策略决定允许算法，请求不能自行降级；RSA继续校验时间窗和有效公钥。切换只改变策略/config_revision，不替换密钥/算法；生成密钥也不暗改策略。

GET/PATCH /console/v1/merchant/account/signature-policy只允许本人原会话，GET返回完整字符串商户ID、keytype（0/1/null）、原algorithm、rsa_configured、非秘密HMAC指纹、can_update。PATCH严格接受keytype和expected_fingerprint，CSRF与事务内持久会话检查、完整CAS、审计后数据库时钟复核，不允许query/未知重复字段、代登录。无有效公钥不能启用仅RSA。冲突或丢响应不能自动重试。

前端沿用现有Pro样式，在API信息区展示当前模式、下拉选项、目标ID及明确确认/取消；历史模式明确标注，未有公钥给出生成提示。取消不发送请求，处理期间互斥，焦点回到有效控件，异步跨会话结果丢弃，不存储密钥。先模型/真实PG-HTTP/桌面375 RED再实现，补并发、撤销/过期、回滚、真实签名、迁移旧列保全与全部旧单元/浏览器回归。本地合成环境，无生产或真实交易。

独立复审确认原管理员密钥页同样具有keytype0/1，且新增策略会与旧算法字段产生误导和可用性问题，故本阶段同时增加GET/PATCH `/console/v1/admin/merchants/{id}/signature-policy`。复用162原super持久会话、完整ID/CSRF、同事实HMAC/CAS与审计后时钟检查；可管理停用商户，普通管理员只读实际模式。既有merchant投影/schema增加只读keytype，禁止存进profile。显式模式下不再显示可编辑的历史算法，并由真实事务锁内拒绝改变它；仅RSA模式的任何普通公钥更新必须保持有效，不能靠旧algorithm=MD5绕过。新建/null历史模式仍保留原严格算法编辑语义。

管理员策略明确提交前同步丢弃旧历史算法草稿，取消不丢；普通资料与手工密钥草稿保持。成功回读更新实际模式，后台外层保存/取消/其它密钥生成动作在处理中互斥。失响应后保存普通资料不得带上原先拟改为RSA的旧草稿。浏览器显示不代替数据库规则，direct Store漏传策略也必须依据当前持久keytype保护公钥。
