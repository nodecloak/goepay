# 61内置页面模板实施约定

依据`remaining-console-evidence.md`及2026-10-23本机复核：Pro模板库和controller被swoole保护；明文路由、前端及默认manifest证明home/pay/qrpay三类及选择/配置/图片操作，但默认配置为空，不能发明原加密JSON。原动态show使用eval，本实现不复制。Go当前template-configs仅资料；checkout固定HTML，collection合法颜色已经生效；mini启动页另有固定HTML。原支付HMAC、账号绑定、CSP和return校验是必须保留的前置条件。

## 独立实现

`internal/pagetemplate`包含构建时白名单registry、类型化配置、CAS持久化、受限图片和受审HTML框架。schema61只新增模板head/config/asset表，不修改1..60。三类内置default/compact，不执行上传模板、路径、脚本或任意表达式，无新插件/依赖。

每类head保存selected及一把revision（字符串API），选择和该类任一模板配置保存均在同Tx行锁/CAS递增；每模板配置保留，切换不丢草稿。public Resolve用一致快照取得选择/配置，校验registry与typed值。无配置使用内置默认；无效配置仅外观回退，通过固定fallback响应头与结构化enum日志可观测；支付验证失败保持原错误。

配置字段为新Go明确公开配置：brand_name、notice、accent_color、background_color、logo_asset_id、density，pay另有show_order_name。全量PUT，未知/重复/null/错类型拒绝；纯文本有界，颜色仅六位hex，密度枚举。订单号、金额、支付状态、通道/账号/签名、支付action和安全提示不可配置。内置框架只控制展示，原已验证material分支仍由server控制，配置不进入template.HTML。

PNG/JPEG上传原文件最多512KiB，整请求受1MiB上限；DecodeConfig先检查边长2048/总像素1048576，再完整解码重编码（规范输出最多2MiB），去元数据。资产ID为规范输出SHA256，数据库保存规范字节/固定mime/尺寸，无文件路径、URL抓取、SVG/GIF/WebP或可执行内容。上传后仅本人super接口预览；公共资产只有当前选中模板的有效配置实际引用才可读，清空/切换立即撤销，no-store。

## 专用接口与接线

- GET admin `/templates?type=`：type/revision/selected/items。
- GET/PUT admin `/templates/{type}/{name}/config`：读type/name/revision/config/fields，写expected_revision/config。
- PUT admin `/templates/{type}/selection`：expected_revision/name。
- POST admin `/templates/images`：唯一multipart file；GET admin `/templates/images/{asset_id}`为私有编辑预览。
- GET admin `/templates/{type}/{name}/preview`：固定假资料无支付动作，不接受真实trade选择器。
- GET/HEAD `/template-assets/{asset_id}`：当前已引用规范图片。
- GET `/`：API公共origin的home展示；不改变独立web console origin路由。

admin仅本人super，拒代登录；写操作CSRF，严格JSON/方法/query，上传限速和有界并发。不增加资金授权动作。公共页面在原订单/collect授权之后才Resolve；预览不调用provider、不建立订单或支付意图。

root负责共有server.go、App.vue/menu、OpenAPI/Nginx及原generic通道封闭。新方法`s.templateConsole(w,r,role,session,parts) bool`、`s.templatePublic(w,r) bool`；新TemplatePage组件props client/adminClass/ownSession。旧backoffice definitionFor对template-configs写必须拒绝，schema权限只读，避免resources/manage绕过。Nginx代理template-assets，collect CSP加img-src self。collection/provider_http由模板owner修改；打印owner提供CreateCollectionOrder后由模板owner替换唯一出生调用，不推断来源。

## 验收

先红绿证明registry白名单、typed边界、CAS并发唯一赢家/审计回滚、图片私有→引用公开→撤销、坏MIME/像素/超限拒绝。真实PG仅专用55436随机schema；不访问其他并行测试实例。

真实HTTP从super登录、选择/改配置、真实checkout/collect/home获取证明内容与布局改变，并检验非法HMAC、过期token、错误purpose、原收款账号变化仍拒绝。全部现有qrcode/jump/html/JSAPI/app/preauth/mini/scan/return状态及collection幂等/风控测试回归；模板不能改变支付material/签名/金额/状态。

浏览器验证原/app/template专用页、三类切换与字段、确认/CAS冲突、PNG/JPEG预览及公开引用、375px、operator/代登录拒绝，无generic模板写请求；真实页面视觉与HTTP行为一致。独立审查后固定SHA、实际命令/输出，不把本期结果当作所有原模板或加密PHP行为全兼容。
