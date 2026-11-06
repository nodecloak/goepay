// Chinese wording for the console API's English messages.
//
// The API keeps short English messages as a stable machine contract: a few
// console decisions and external tools match them exactly. People using the
// console read Chinese, so ApiError.message is localized here while
// ApiError.serverMessage keeps the original text for program logic.
//
// tests/server-messages.test.ts fails when a Go handler returns a new English
// console message without an entry below. A message that is still missing at
// runtime falls back to a Chinese explanation by HTTP status with the original
// appended, so operators never see an English-only error.
const zh: Readonly<Record<string, string>> = {
  "1 to 100 items required": "明细数量必须为 1–100",
  "API settings unavailable": "API 设置暂不可用",
  "API switches changed; refresh before saving":
    "API 开关已变化，请刷新后再保存",
  "API switches could not be confirmed": "无法确认 API 开关",
  "CAPTCHA key operation could not be confirmed; reload before retrying":
    "验证码密钥操作未能确认，请刷新后重试",
  "CAPTCHA key settings unavailable": "验证码密钥设置暂不可用",
  "CSRF token required": "缺少页面安全令牌，请刷新页面后重试",
  "CSV exports busy": "CSV 导出繁忙，请稍后再试",
  "CSV matrix too large; narrow dates": "CSV 矩阵过大，请缩小日期范围",
  "CSV too large": "CSV 内容过大，请缩小筛选范围",
  "GET or PATCH required": "仅支持 GET 或 PATCH 请求",
  "GET or POST required": "仅支持 GET 或 POST 请求",
  "GET or PUT required": "仅支持 GET 或 PUT 请求",
  "GET required": "仅支持 GET 请求",
  "JSON content type required": "请求格式必须为 JSON",
  "PNG/JPEG/GIF image below2MiB and at most one megapixel required":
    "请上传小于 2MiB、不超过 100 万像素的 PNG/JPEG/GIF 图片",
  "POST JSON required": "请以 POST 方式提交 JSON",
  "POST required": "仅支持 POST 请求",
  "PUT required": "仅支持 PUT 请求",
  "RSA key changed; refresh account info before retrying":
    "RSA 密钥已变更，请刷新账户信息后重试",
  "RSA key rotation could not be confirmed; refresh account info":
    "RSA 密钥轮换未能确认，请刷新账户信息",
  "SSO operation not found": "未找到该代登录操作",
  "URL Link outcome could not be stored; do not repeat generation":
    "URL Link 结果未能保存，请勿重复生成",
  "a valid merchant RSA public key is required for RSA-only mode":
    "仅 RSA 模式需要有效的商户 RSA 公钥",
  "a valid test phone and current confirmation required":
    "需要有效的测试手机号并确认本次操作",
  "account bindings unavailable": "账号绑定信息暂不可用",
  "account disabled": "账号已停用",
  "account unavailable": "账号不可用",
  "account verification unavailable": "暂时无法核验账号，请稍后重试",
  "admin balance adjustment recorded": "已记录管理员余额调整",
  "admin security reset could not be completed": "管理员安全设置重置未完成",
  "administrator account required": "需要管理员账号",
  "administrator authority unavailable": "暂时无法核验管理员权限",
  "administrator business notice not found": "未找到该管理员业务通知",
  "administrator business notice proof conflict":
    "管理员业务通知校验冲突，请刷新后重试",
  "administrator business notice source unavailable":
    "管理员业务通知来源暂不可用",
  "administrator buyer statistics required": "仅管理员可查看买家统计",
  "administrator configuration required": "需要管理员配置",
  "administrator group statistics required": "仅管理员可查看用户组统计",
  "administrator login name already reserved": "该管理员登录名已被占用",
  "administrator login name changed": "管理员登录名已修改",
  "administrator login name unchanged": "管理员登录名未变化",
  "administrator merchant statistics required": "仅管理员可查看商户统计",
  "administrator report": "管理员报表",
  "administrator required": "需要管理员权限",
  "administrator session required": "需要管理员登录",
  "administrator sharing overview required": "仅管理员可查看分账总览",
  "administrator template notice input invalid": "管理员模板通知内容无效",
  "administrator template notice requires original claim review":
    "管理员模板通知需要先复核原认领",
  "ambiguous batch query": "批量查询条件有歧义",
  "ambiguous list query": "列表查询条件有歧义",
  "application authorization is unavailable or no longer authorized":
    "应用授权不可用或已失效",
  "application authorization rate limited": "应用授权操作过于频繁，请稍后再试",
  "application authorization request unavailable": "应用授权请求暂不可用",
  "application authorization unavailable": "应用授权暂不可用",
  "application credential unavailable or already revealed":
    "应用凭据不可用或已显示过",
  "application operation unavailable": "应用操作暂不可用",
  "application/json required": "请求格式必须为 JSON",
  "approval and external payout are separate states":
    "审核通过与外部付款是两个独立状态",
  "approved application authorization unavailable": "已批准的应用授权暂不可用",
  "authenticated business role required": "需要已登录的业务角色",
  "authenticated console scope required": "需要已登录的后台权限范围",
  "authenticated ledger scope required": "需要已核验的账务查看范围",
  "authenticated order scope required": "需要已核验的订单查看范围",
  "authenticated refund scope required": "需要已核验的退款查看范围",
  "authentication required": "请先登录",
  "authenticator encryption unavailable": "身份验证器加密服务暂不可用",
  "authenticator is already enabled or unavailable":
    "身份验证器已启用或暂不可用",
  "authenticator settings changed; sign in again":
    "身份验证器设置已变更，请重新登录",
  "authorization applies only to the previewed action":
    "授权只适用于已预览的操作",
  "authorization cannot be released in its current state":
    "该授权当前状态不能解除",
  "authorization capture not enabled": "未开启预授权完成（扣款）",
  "authorization capture unavailable": "预授权完成（扣款）暂不可用",
  "authorization is no longer eligible for capture": "该授权已不能完成扣款",
  "authorization is not eligible for first capture": "该授权不满足首次扣款条件",
  "authorization observation is unavailable or no longer authorized":
    "授权观察不可用或已失效",
  "authorization observation not enabled": "未开启授权观察",
  "authorization observation permission denied": "没有授权观察权限",
  "authorization observation unavailable": "授权观察暂不可用",
  "authorization observation unavailable or binding changed":
    "授权观察不可用或绑定已变更",
  "authorization provider unavailable": "授权服务商暂不可用",
  "authorization release definitively rejected": "上游已明确拒绝解除授权",
  "authorization release operation not enabled": "未开启授权解除操作",
  "authorization release pending; query the original request":
    "授权解除处理中，请查询原请求",
  "authorization release result": "授权解除结果",
  "authorization release unavailable": "授权解除暂不可用",
  "authorized parent order read required": "需要有权读取的父订单",
  "automatic queue runtime unavailable": "自动队列运行环境暂不可用",
  "automatic settlement unavailable": "自动结算暂不可用",
  "backoffice order control saved; payment facts retained":
    "后台订单控制已保存，支付记录保持不变",
  "bank return requires verified reconciliation": "银行退票需要先完成核对",
  "batch POST required": "批量操作需使用 POST 提交",
  "batch backoffice controls saved atomically; payment facts retained":
    "批量后台控制已一并保存，支付记录保持不变",
  "batch method not available": "该批量方式不可用",
  "batch method unavailable": "该批量方式暂不可用",
  "batch not found": "未找到该批次",
  "batch operation unavailable": "批量操作暂不可用",
  "batch prepared; no items sent": "批次已准备，尚未发送任何明细",
  "batch source changed; inspect original state":
    "批次来源已变化，请查看原状态",
  "binding unavailable": "绑定暂不可用",
  "blacklist POST required": "黑名单操作需使用 POST 提交",
  "blacklist access denied": "没有黑名单操作权限",
  "boolean clear required": "清除标记必须为是或否",
  "boolean clear_apikey required": "清除密钥标记必须为是或否",
  "boolean clear_password required": "清除密码标记必须为是或否",
  "bounded limit required": "请填写有效范围内的数量上限",
  "bounded notice body required": "通知内容长度超出限制",
  "brand image settings unavailable": "品牌图片设置暂不可用",
  "brand image source changed or could not be confirmed; reload first":
    "品牌图片来源已变化或无法确认，请先刷新",
  "branding image not configured": "尚未配置品牌图片",
  "branding source conflict": "品牌设置来源冲突，请刷新后重试",
  "built-in and uploaded templates": "内置模板与上传模板",
  "business SMS configuration changed": "业务短信配置已变更",
  "business SMS configuration not confirmed; reload before retrying":
    "业务短信配置未能确认，请刷新后再试",
  "business SMS unavailable": "业务短信暂不可用",
  "business notice history unavailable": "业务通知历史暂不可用",
  "business notice operation unavailable": "业务通知操作暂不可用",
  "buyer charge settings changed; inspect before confirming":
    "买家手续费设置已变更，请核对后再确认",
  "buyer charge settings could not be confirmed": "无法确认买家手续费设置",
  "buyer charge settings saved for future orders":
    "买家手续费设置已保存，仅对之后的订单生效",
  "buyer charge settings unavailable": "买家手续费设置暂不可用",
  "buyer statistics busy": "买家统计正在计算，请稍后再试",
  "calculated order share reserved; administrator review required":
    "已预留按比例计算的订单分成，需管理员复核",
  "callback could not be verified": "无法核验回调",
  "callback domain is not authorized": "回调域名未授权",
  "cannot notify unpaid order": "订单未付款，不能发送通知",
  "canonical after_id required": "请提供有效的起始 ID",
  "canonical authority ID required": "请提供有效的授权 ID",
  "canonical base64 file required": "请上传规范的 Base64 文件",
  "canonical batch pagination required": "批次分页参数无效",
  "canonical batch query required": "批次查询参数无效",
  "canonical business notice pagination required": "业务通知分页参数无效",
  "canonical channel required": "请提供有效的通道",
  "canonical channel without query required":
    "请提供有效的通道且不要附带查询参数",
  "canonical channel_id required": "请提供有效的通道 ID",
  "canonical cursor and limit 1..100 required":
    "请提供有效的游标，每页数量为 1–100",
  "canonical expected revision required": "请提供有效的版本号",
  "canonical expected version required": "请提供有效的版本",
  "canonical merchant id required": "请提供有效的商户 ID",
  "canonical merchant, profile and revision required":
    "请提供有效的商户、档案和版本号",
  "canonical merchant_id required": "请提供有效的商户 ID",
  "canonical notice pagination required": "通知分页参数无效",
  "canonical plan without query required": "请提供有效的计划且不要附带查询参数",
  "canonical policy scope required": "请提供有效的策略范围",
  "canonical positive ID required": "请提供有效的正整数 ID",
  "canonical preference revision and original recipient required":
    "请提供有效的偏好版本号和原收件人",
  "canonical print job id required": "请提供有效的打印任务 ID",
  "canonical profile and revision required": "请提供有效的档案和版本号",
  "canonical schedule interval required": "请提供有效的计划间隔",
  "capability not implemented": "该功能尚未实现",
  "capture outcome pending; query original order without resubmitting":
    "扣款结果处理中，请查询原订单，不要重复提交",
  "capture policy rejected": "扣款策略不允许此操作",
  "capture rejected; original request cannot be resent":
    "扣款被拒绝，原请求不能重新发送",
  "capture result audit unavailable; query original order":
    "扣款结果审计暂不可用，请查询原订单",
  "certification provider unavailable": "实名认证服务暂不可用",
  "certification result unknown": "认证结果未知",
  "certification state changed": "认证状态已变化",
  "channel catalog is read-only": "通道目录为只读",
  "channel does not support complaints (only WeChat Pay V3 / Alipay), or frequency/num/days out of range (frequency 1-1440 min, num 1-200, days >= 0)":
    "该通道不支持投诉同步（仅支持微信支付 V3 和支付宝），或频率、数量、天数超出范围（频率 1–1440 分钟，数量 1–200，天数不小于 0）",
  "channel is stopped for new writes; reconcile original requests":
    "该通道已停止新的写入，请先核对原请求",
  "channel not found": "未找到该通道",
  "channel restored; original transfers were not resent":
    "通道已恢复，原转账未重新发送",
  "channel sharing finish operation unavailable": "分账完结操作暂不可用",
  "channel sharing operation not found": "未找到该分账操作",
  "channel sharing operation or method unavailable": "分账操作或方式暂不可用",
  "channel sharing permission required": "需要分账权限",
  "channel sharing return operation unavailable": "分账回退操作暂不可用",
  "channel sharing unavailable": "分账功能暂不可用",
  "channel stop status unavailable": "通道停用状态暂不可用",
  "channel sync is an administrator action": "通道同步只能由管理员操作",
  "cleanup POST without query required":
    "清理请求需使用 POST 且不要附带查询参数",
  "cleanup days must be 1..9999": "清理天数必须在 1–9999 之间",
  "client_request_no must be 64 lowercase hexadecimal characters":
    "client_request_no 必须是 64 位小写十六进制字符",
  "collection action not found": "未找到该收款操作",
  "collection code not found": "未找到该收款码",
  "collection configuration unavailable": "收款配置暂不可用",
  "collection image not found": "未找到该收款图片",
  "collection link is valid for two hours; regenerate after expiry":
    "收款链接有效期为两小时，过期后请重新生成",
  "collection origin unavailable": "收款来源地址暂不可用",
  "collection provider unavailable": "收款服务商暂不可用",
  "combined order history not found": "未找到该合单历史",
  "combined payment does not cover every sub-order": "合单支付未覆盖全部子订单",
  "complaint action submitted": "投诉处理已提交",
  "complaint channel mismatch or busy": "投诉通道不匹配或正忙",
  "complaint channels are an administrator view": "投诉通道仅管理员可查看",
  "complaint event connection not established": "投诉事件连接未建立",
  "complaint event delivery result unavailable": "投诉事件投递结果暂不可用",
  "complaint event request construction failed": "投诉事件请求构建失败",
  "complaint event signing unavailable": "投诉事件签名暂不可用",
  "complaint feature disabled": "投诉功能未开启",
  "complaint feature unavailable": "投诉功能暂不可用",
  "complaint not found": "未找到该投诉",
  "complaint sync task configuration is an administrator action":
    "投诉同步任务配置只能由管理员操作",
  "complaint sync tasks are an administrator view":
    "投诉同步任务仅管理员可查看",
  "complaints synced": "投诉已同步",
  "complete business SMS snapshot required": "请提交完整的业务短信配置",
  "complete explicit notification account required":
    "请提供完整且明确的通知账号",
  "complete string templates required": "请提供完整的文字模板",
  "complete template snapshot required": "请提交完整的模板内容",
  "complete voice settings required": "请提交完整的语音设置",
  "configuration changed; reload before saving": "配置已变更，请刷新后再保存",
  "configuration revision and enabled flag required":
    "需要配置版本号和启用状态",
  "configure your funds password first": "请先设置资金密码",
  "confirmation changed or expired; inspect the original request":
    "确认信息已变化或已过期，请查看原请求",
  "connect-login state invalid, expired, or already used":
    "快捷登录状态无效、已过期或已被使用",
  "connect-login state source cannot be confirmed": "无法确认快捷登录状态来源",
  "console role mismatch": "登录角色不匹配",
  "core payment risk method unavailable": "核心支付风控方式暂不可用",
  "core risk check could not be confirmed": "无法确认核心风控检查",
  "core risk check method unavailable": "核心风控检查方式暂不可用",
  "core risk facts changed; refresh before confirming":
    "核心风控数据已变化，请刷新后再确认",
  "core risk policy changed; refresh before confirming":
    "核心风控策略已变化，请刷新后再确认",
  "core risk policy could not be confirmed": "无法确认核心风控策略",
  "credential change could not be confirmed; refresh current state":
    "凭据变更未能确认，请刷新查看当前状态",
  "credential changed; save the new secret now": "凭据已变更，请立即保存新密钥",
  "credential verification busy": "凭据核验繁忙，请稍后再试",
  "credentials changed; refresh before an explicit new operation":
    "凭据已变更，请刷新后再明确发起新操作",
  "cross-site login refused":
    "登录请求来自其他网站，已拒绝。请直接打开本站后台登录页后重试",
  "current build state unavailable": "当前版本状态暂不可用",
  "current channel does not support channel sharing or writes are disabled":
    "当前通道不支持分账或已停止写入",
  "current common conditions only; payer group may vary; examples are not an order estimate or spendable balance":
    "仅为当前通用条件；付款方用户组可能不同；示例不代表订单预估或可用余额",
  "current native scan confirmation required": "需要确认当前原生扫码设置",
  "current password rejected": "当前密码不正确",
  "cursor uses relation ID; final full page may be followed by an empty page":
    "游标按关联 ID 计算，最后一整页之后可能还有一个空页",
  "cutoff must be 1 to 365 days ago": "截止时间必须在 1–365 天之前",
  "dashboard query parameters not accepted": "仪表盘不接受查询参数",
  "dashboard tips unavailable": "仪表盘提示暂不可用",
  "database unavailable": "数据库暂不可用",
  "delegated action audit unavailable": "代操作审计暂不可用",
  "delegated authority revoked": "代操作授权已撤销",
  "delegated identity unavailable": "代操作身份暂不可用",
  "delegated merchant session created": "已创建商户代登录会话",
  "delegation grant unavailable": "代操作授权暂不可用",
  "delegation target is no longer available": "代操作目标已不可用",
  "deposit release reviewed": "保证金解冻已审核",
  "duplicate field": "字段重复",
  "duplicate object key": "对象键重复",
  "effective channel write status": "通道实际写入状态",
  "effective merchant group unavailable": "商户实际所属用户组暂不可用",
  "email binding changed or flow cannot perform this action":
    "邮箱绑定已变化，或当前流程不能执行此操作",
  "email verification unavailable; query the original flow before retrying":
    "邮箱验证暂不可用，请先查询原流程再重试",
  "empty JSON object required": "请求体必须为空 JSON 对象",
  "empty notification URL": "通知地址为空",
  "empty object required": "请求体必须为空对象",
  "empty statistics scope": "统计范围为空",
  "enable the gateway with an active provider before a test send":
    "请先启用网关并选择可用服务商，再发送测试",
  "enabled merchant login account required": "需要已启用的商户后台登录账号",
  "estimate only; submit revalidates policy and funds":
    "仅为估算，提交时会重新核验策略和资金",
  "event subscription changed; refresh before saving":
    "事件订阅已变化，请刷新后再保存",
  "event subscription could not be confirmed": "无法确认事件订阅",
  "event subscription saved": "事件订阅已保存",
  "event subscription unavailable": "事件订阅暂不可用",
  "exact API switches JSON required": "请提交完整的 API 开关设置",
  "exact CAPTCHA key operation required": "请明确验证码密钥操作",
  "exact batch selection required": "请明确选择批次",
  "exact current account confirmation required": "请确认当前账号",
  "exact current branding fingerprint required": "请提供当前品牌配置指纹",
  "exact current frequency confirmation required": "请确认当前发送频率设置",
  "exact event subscription JSON required": "请提交完整的事件订阅设置",
  "exact hold_id and external_no required": "请提供 hold_id 和 external_no",
  "exact merchant key login required": "请填写完整的商户 ID 和商户密钥",
  "exact minimum confirmation required": "请确认最低金额设置",
  "exact observation, remaining cents and source revision are required":
    "需要提供观察记录、剩余金额（分）和来源版本",
  "exact order alias JSON required": "请提交完整的订单别名设置",
  "exact original SMTP fields and current confirmation required":
    "请提供原 SMTP 字段并确认当前设置",
  "exact original buyer charge confirmation required": "请确认原买家手续费设置",
  "exact original fee fields required": "请提供原手续费字段",
  "exact payment defend group JSON required": "请提交完整的支付验证用户组设置",
  "exact payment defend policy JSON required": "请提交完整的支付验证策略",
  "exact personal settings confirmation required": "请确认个人设置",
  "exact platform access settings required": "请提交完整的平台访问设置",
  "exact positive amount and external_no required":
    "请提供正数金额和 external_no",
  "exact scan payment setting confirmation required": "请确认扫码支付设置",
  "exact sms gateway fields and current confirmation required":
    "请提供短信网关字段并确认当前设置",
  "exact stop event and revision required": "请提供停用事件和版本号",
  "exact update intent and fingerprints required": "请提供更新意图和指纹",
  "existing login password required": "需要输入现有登录密码",
  "existing original URL Link intent; no generation repeated":
    "原 URL Link 请求已存在，不会重复生成",
  "expected JSON object": "请求体必须为 JSON 对象",
  "expected_fingerprint required": "缺少 expected_fingerprint",
  "expected_revision and then one zip file required":
    "请先提供 expected_revision，再上传一个 zip 文件",
  "explicit IPv4 or IPv6 trusted proxy CIDRs required; /0 forbidden":
    "需要明确的 IPv4 或 IPv6 可信代理网段，不允许 /0",
  "explicit automatic authority method required": "请明确自动授权方式",
  "explicit bounded plan authorization required":
    "需要明确且有额度上限的计划授权",
  "explicit bounded settlement business mode required":
    "请明确有额度上限的结算业务模式",
  "explicit channel status or restoration method required":
    "请明确通道状态或恢复方式",
  "explicit current revision required": "请提供当前版本号",
  "explicit merchant_id required": "请提供商户 ID",
  "explicit opt-in to pending request generation only":
    "只能明确选择生成待处理请求",
  "explicit original notice fields required": "请提供原通知字段",
  "explicit plan definition required": "请提供计划定义",
  "explicit preference fields required": "请提供偏好设置字段",
  "explicit schedule fields required": "请提供计划时间字段",
  "explicit supported membership policy fields required":
    "请提供受支持的会员策略字段",
  "explicit supported reward policy fields required":
    "请提供受支持的奖励策略字段",
  "explicit template snapshot strings required": "请提供模板快照文本",
  "explicit template strings required": "请提供模板文本",
  "explicit typed printing fields and canonical revisions required":
    "请提供打印字段和版本号",
  "export format required": "请选择导出格式",
  "external printing connector unavailable": "外部打印连接器暂不可用",
  "external printing execution unavailable": "外部打印执行暂不可用",
  "field and expected_fingerprint required":
    "需要提供字段和 expected_fingerprint",
  "field required": "请填写必填字段",
  forbidden: "没有权限执行此操作",
  "format must be csv": "导出格式必须为 CSV",
  "fresh action authorization required": "需要重新授权本次操作",
  "fresh funds action authorization required": "需要重新进行资金操作授权",
  "fresh provider evidence unavailable; no new capture confirmed":
    "暂无新的服务商凭证，未确认新的扣款",
  "funds action authorization required or invalid": "资金操作授权缺失或无效",
  "funds action resolver unavailable": "资金操作授权服务暂不可用",
  "funds authentication failed": "资金密码验证失败",
  "funds authorization unavailable": "资金授权暂不可用",
  "funds credential not configured": "尚未设置资金密码",
  "funds password updated; previous funds authorizations revoked":
    "资金密码已更新，之前的资金授权已撤销",
  "funds reserved; no external payout": "资金已预留，尚未向外付款",
  "funds security operation not found": "未找到该资金安全操作",
  "funds verification capacity unavailable": "资金验证服务繁忙，请稍后再试",
  "generation and reservation only; approval and payout require their original authorization":
    "仅生成并预留资金；审批和付款需要各自的授权",
  "generation request changed or source invalid": "生成请求已变化或来源无效",
  "global channel limits updated": "全局通道限额已更新",
  "group not found": "未找到该用户组",
  "group or inherited risk settings changed; refresh before confirming":
    "用户组或继承的风控设置已变化，请刷新后再确认",
  "group risk method unavailable": "用户组风控方式暂不可用",
  "group risk settings could not be confirmed": "无法确认用户组风控设置",
  "group statistics busy": "用户组统计正在计算，请稍后再试",
  "group statistics contains an unsupported payment method":
    "用户组统计包含不支持的支付方式",
  "group unavailable": "用户组暂不可用",
  "historical assertions only; no verified payment source":
    "仅为历史记录，没有已核验的支付来源",
  "historical issued, voided and refund-pending flows; not a spendable balance":
    "历史发放、作废及待退款记录，不是可用余额",
  "historical read-only authorization observation": "历史只读授权观察",
  "historical refunds are read only": "历史退款为只读",
  "historical source verification failed": "历史来源核验失败",
  "historical suborder source could not be confirmed": "无法确认历史子订单来源",
  "historical suborders are read only": "历史子订单为只读",
  "historical suborders reader unavailable": "历史子订单读取暂不可用",
  "idempotency conflict": "请求重复或与原请求冲突，请刷新后核对",
  "identity creation rate limited": "身份创建过于频繁，请稍后再试",
  "identity lookup is not configured": "未配置身份查询",
  "identity lookup is unavailable or no longer authorized":
    "身份查询不可用或已失效",
  "identity lookup query is not allowed": "不允许该身份查询",
  "identity operation unavailable": "身份操作暂不可用",
  "identity request could not be created": "无法创建身份请求",
  "identity request is unavailable or expired": "身份请求不可用或已过期",
  "identity request not found": "未找到该身份请求",
  "identity request unavailable": "身份请求暂不可用",
  "identity request unavailable or profile changed":
    "身份请求不可用或档案已变更",
  "identity result is unavailable or already revealed":
    "身份结果不可用或已显示过",
  "identity result unavailable or already revealed": "身份结果不可用或已显示过",
  "identity route is no longer available": "身份路由已不可用",
  "identity verified; return to the initiating console for one-time reveal":
    "身份已核验，请回到发起的后台页面查看（仅显示一次）",
  "image POST or exact private GET required":
    "仅支持上传图片或读取指定的私有图片",
  "image exceeds 1MiB": "图片不能超过 1MiB",
  "image exceeds 512KiB limit": "图片不能超过 512KiB",
  "image must be smaller than2MiB": "图片必须小于 2MiB",
  "image processing busy": "图片处理繁忙，请稍后再试",
  "image too large": "图片过大",
  "image upload or private GET required": "仅支持上传图片或读取私有图片",
  "image upload rate limited": "图片上传过于频繁，请稍后再试",
  "image uploaded": "图片已上传",
  "import operation unavailable": "导入操作暂不可用",
  "import rate limited": "导入过于频繁，请稍后再试",
  "import request too large": "导入内容过大",
  "import validation failed": "导入校验未通过",
  "income ledger unavailable": "收入账务暂不可用",
  "independent application authorization; no channel binding":
    "独立的应用授权，不绑定通道",
  "independent authority does not imply executor running":
    "独立授权不代表执行程序已在运行",
  "independent bounded payout authority created": "已创建独立的限额付款授权",
  "independent display alias could not be confirmed": "无法确认独立显示别名",
  "independent display alias source or revision conflict":
    "独立显示别名来源或版本冲突",
  "independent display alias unavailable": "独立显示别名暂不可用",
  "independent identity application catalog unavailable":
    "独立身份应用目录暂不可用",
  "independent identity only; no payment": "仅为独立身份，不涉及支付",
  "independent identity service unavailable": "独立身份服务暂不可用",
  "inspect individual results; response is not proof all items succeeded":
    "请逐项查看结果，本响应不代表全部成功",
  "inspect payout state; approval alone is not paid":
    "请查看付款状态，审批通过不代表已付款",
  "insufficient available balance": "可用余额不足",
  "insufficient balance": "余额不足",
  "insufficient certification balance": "实名认证余额不足",
  "internal error": "服务器内部错误，请稍后重试",
  "internal ledger deposit recorded": "已记录内部账务入账",
  "invalid API switches": "API 开关设置无效",
  "invalid CAPTCHA configuration or proof": "验证码配置或验证结果无效",
  "invalid CSRF token": "页面安全令牌无效，请刷新页面后重试",
  "invalid ID": "ID 无效",
  "invalid JSON": "JSON 格式无效",
  "invalid JSON input": "JSON 输入无效",
  "invalid PNG/JPEG image; maximum 4MP and 4096px edge":
    "图片无效：仅支持 PNG/JPEG，最大 400 万像素且边长不超过 4096px",
  "invalid QQ or personal amount bounds": "QQ 或个人金额范围无效",
  "invalid RSA private key (minimum 2048 bits)": "RSA 私钥无效（至少 2048 位）",
  "invalid RSA public key (minimum 2048 bits)": "RSA 公钥无效（至少 2048 位）",
  "invalid RSA rotation request": "RSA 密钥轮换请求无效",
  "invalid SMTP account": "SMTP 账号无效",
  "invalid SMTP account fields": "SMTP 账号字段无效",
  "invalid Weixin body": "微信请求内容无效",
  "invalid admin id": "管理员 ID 无效",
  "invalid administrator business notice": "管理员业务通知无效",
  "invalid adoption body": "载入请求内容无效",
  "invalid amount": "金额无效",
  "invalid batch body": "批次请求内容无效",
  "invalid batch fields": "批次字段无效",
  "invalid batch item or channel": "批次明细或通道无效",
  "invalid batch request": "批次请求无效",
  "invalid blacklist value or expiry": "黑名单值或有效期无效",
  "invalid branding image": "品牌图片无效",
  "invalid business SMS configuration": "业务短信配置无效",
  "invalid business SMS field types": "业务短信字段类型无效",
  "invalid buyer charge mode or rate": "买家手续费模式或费率无效",
  "invalid buyer statistics filter": "买家统计筛选条件无效",
  "invalid callback": "回调无效",
  "invalid capabilities query": "能力查询参数无效",
  "invalid cashier parameters": "收银台参数无效",
  "invalid certification request": "认证请求无效",
  "invalid channel": "通道无效",
  "invalid channel list": "通道列表无效",
  "invalid collection ID": "收款 ID 无效",
  "invalid collection configuration or unavailable origin/style":
    "收款配置无效，或来源地址/样式不可用",
  "invalid collection layout or revision": "收款版式或版本无效",
  "invalid collection origin": "收款来源地址无效",
  "invalid collection style": "收款样式无效",
  "invalid complaint action body": "投诉处理内容无效",
  "invalid complaint action input": "投诉处理输入无效",
  "invalid copy source": "复制来源无效",
  "invalid core payment risk setting": "核心支付风控设置无效",
  "invalid core payment risk target": "核心支付风控对象无效",
  "invalid core risk check JSON": "核心风控检查 JSON 无效",
  "invalid core risk check values": "核心风控检查数值无效",
  "invalid core risk policy JSON": "核心风控策略 JSON 无效",
  "invalid core risk policy values": "核心风控策略数值无效",
  "invalid credential confirmation": "凭据确认信息无效",
  "invalid credential target": "凭据对象无效",
  "invalid credentials": "账号、密码或密钥错误",
  "invalid credentials or authenticator code": "账号、密码或身份验证码错误",
  "invalid current password": "当前密码不正确",
  "invalid daily scheduler configuration": "每日计划配置无效",
  "invalid database business date": "数据库业务日期无效",
  "invalid date range (maximum 367 days)": "日期范围无效（最长 367 天）",
  "invalid email": "邮箱无效",
  "invalid email binding input": "邮箱绑定信息无效",
  "invalid encrypted channel import": "加密通道导入文件无效",
  "invalid end": "结束时间无效",
  "invalid enrollment input": "注册信息无效",
  "invalid event URL or event types": "事件地址或事件类型无效",
  "invalid exact parent selection": "父项选择无效",
  "invalid exact subject fields": "主体字段无效",
  "invalid financial request body": "资金请求内容无效",
  "invalid fixed amount": "固定金额无效",
  "invalid form": "表单无效",
  "invalid funds authorization input": "资金授权输入无效",
  "invalid generator path or query": "生成路径或查询参数无效",
  "invalid global channel limits JSON": "全局通道限额 JSON 无效",
  "invalid global channel limits target": "全局通道限额对象无效",
  "invalid global parent channel limits": "全局父通道限额无效",
  "invalid group payment info": "用户组支付信息无效",
  "invalid group redpack feature": "用户组红包功能设置无效",
  "invalid group risk JSON": "用户组风控 JSON 无效",
  "invalid group risk config": "用户组风控配置无效",
  "invalid group risk config or local lookup unavailable":
    "用户组风控配置无效，或本地地址库不可用",
  "invalid group risk target": "用户组风控对象无效",
  "invalid group sales configuration": "用户组销售配置无效",
  "invalid group settlement cycle": "用户组结算周期无效",
  "invalid group settlement minimum": "用户组最低结算金额无效",
  "invalid group settlement type switch": "用户组结算方式开关无效",
  "invalid group statistics filter": "用户组统计筛选条件无效",
  "invalid group transfer fee": "用户组转账手续费无效",
  "invalid history query": "历史查询参数无效",
  "invalid identity lookup JSON": "身份查询 JSON 无效",
  "invalid import request": "导入请求无效",
  "invalid independent display alias": "独立显示别名无效",
  "invalid input": "输入无效",
  "invalid input or state": "输入内容或当前状态不允许此操作",
  "invalid invitation input": "邀请信息无效",
  "invalid job id": "任务 ID 无效",
  "invalid job payload": "任务内容无效",
  "invalid lease duration": "租约时长无效",
  "invalid ledger summary date": "账务汇总日期无效",
  "invalid ledger summary filter": "账务汇总筛选条件无效",
  "invalid ledger summary id": "账务汇总 ID 无效",
  "invalid ledger summary query": "账务汇总查询无效",
  "invalid ledger summary scope": "账务汇总范围无效",
  "invalid ledger summary search": "账务汇总搜索条件无效",
  "invalid limit": "数量上限无效",
  "invalid list filter": "列表筛选条件无效",
  "invalid local simulation request": "本地模拟请求无效",
  "invalid login log pagination": "登录日志分页参数无效",
  "invalid login log query": "登录日志查询条件无效",
  "invalid mail frequency": "邮件发送频率设置无效",
  "invalid matrix query": "矩阵查询参数无效",
  "invalid matrix sort column": "矩阵排序列无效",
  "invalid merchant": "商户无效",
  "invalid merchant combined payment policy": "商户合单支付策略无效",
  "invalid merchant deposit policy": "商户保证金策略无效",
  "invalid merchant group feature switch": "商户用户组功能开关无效",
  "invalid merchant id": "商户 ID 无效",
  "invalid merchant notice input": "商户通知内容无效",
  "invalid merchant payment domain policy": "商户支付域名策略无效",
  "invalid merchant query": "商户查询条件无效",
  "invalid merchant settlement permission": "商户结算权限无效",
  "invalid merchant statistics filter": "商户统计筛选条件无效",
  "invalid merchant transfer permission": "商户转账权限无效",
  "invalid merchant_id": "商户 ID 无效",
  "invalid migration backup upload": "迁移备份上传无效",
  "invalid migration wizard operation": "迁移向导操作无效",
  "invalid minimum fee amount": "最低手续费金额无效",
  "invalid native scan application selection": "原生扫码应用选择无效",
  "invalid notice configuration": "通知配置无效",
  "invalid notification URL": "通知地址无效",
  "invalid notification account": "通知账号无效",
  "invalid notification account types": "通知账号类型无效",
  "invalid notification failure": "通知失败设置无效",
  "invalid notification request": "通知请求无效",
  "invalid notification retry delay": "通知重试间隔无效",
  "invalid oauth credential operation": "OAuth 凭据操作无效",
  "invalid object close": "对象结尾无效",
  "invalid object key": "对象键无效",
  "invalid or ambiguous JSON": "JSON 无效或有歧义",
  "invalid or ambiguous import fields": "导入字段无效或有歧义",
  "invalid or duplicate selected order": "所选订单无效或重复",
  "invalid or expired timestamp": "时间戳无效或已过期",
  "invalid or expired verification": "验证无效或已过期",
  "invalid or reused authenticator code": "身份验证码无效或已使用",
  "invalid or unavailable runtime parent configuration":
    "运行时父通道配置无效或不可用",
  "invalid or unsupported merchant filter": "商户筛选条件无效或不受支持",
  "invalid or unsupported resource filter": "资源筛选条件无效或不受支持",
  "invalid order summary date": "订单汇总日期无效",
  "invalid order summary filter": "订单汇总筛选条件无效",
  "invalid order summary id": "订单汇总 ID 无效",
  "invalid order summary query": "订单汇总查询无效",
  "invalid order summary scope": "订单汇总范围无效",
  "invalid order summary search": "订单汇总搜索条件无效",
  "invalid original Weixin ID": "原微信应用 ID 无效",
  "invalid original Weixin selection": "原微信应用选择无效",
  "invalid original Weixin selection fields": "原微信应用选择字段无效",
  "invalid original batch target": "原批量对象无效",
  "invalid original parent ID": "原父通道 ID 无效",
  "invalid original parent channel": "原父通道无效",
  "invalid original parent selector": "原父通道选择条件无效",
  "invalid original query": "原查询条件无效",
  "invalid original registered source": "原注册来源无效",
  "invalid original request lookup": "原请求查找条件无效",
  "invalid original state fingerprint": "原状态指纹无效",
  "invalid original test email request": "原测试邮件请求无效",
  "invalid outbound proxy configuration": "出站代理配置无效",
  "invalid page": "页码无效",
  "invalid page template input": "页面模板内容无效",
  "invalid pagination": "分页参数无效",
  "invalid parameters": "参数无效",
  "invalid parent account fields": "父账户字段无效",
  "invalid parent channel filters": "父通道筛选条件无效",
  "invalid parent configuration body": "父通道配置内容无效",
  "invalid parent configuration field": "父通道配置字段无效",
  "invalid parent configuration fields": "父通道配置字段无效",
  "invalid parent configuration request": "父通道配置请求无效",
  "invalid parent configuration values": "父通道配置值无效",
  "invalid parent statistics": "父通道统计无效",
  "invalid parent subject target": "父通道主体对象无效",
  "invalid payment defend group": "支付验证用户组无效",
  "invalid payment defend group value": "支付验证用户组取值无效",
  "invalid payment defend policy": "支付验证策略无效",
  "invalid payment defend policy values": "支付验证策略取值无效",
  "invalid payment parameters": "支付参数无效",
  "invalid payment subchannel settings": "支付子通道设置无效",
  "invalid payment test amount": "支付测试金额无效",
  "invalid payment test body": "支付测试内容无效",
  "invalid payment test field": "支付测试字段无效",
  "invalid payment test fields": "支付测试字段无效",
  "invalid payment test target": "支付测试对象无效",
  "invalid phone": "手机号无效",
  "invalid plan list query": "计划列表查询条件无效",
  "invalid printing input": "打印设置无效",
  "invalid printing request": "打印请求无效",
  "invalid private search input": "隐私搜索输入无效",
  "invalid profile": "档案无效",
  "invalid provider request": "支付机构请求参数无效，请核对通道配置和业务数据",
  "invalid province count": "省份数量无效",
  "invalid province statistics filter": "省份统计筛选条件无效",
  "invalid proxy credential operation": "代理凭据操作无效",
  "invalid public test payment": "公开测试支付无效",
  "invalid query": "查询条件无效",
  "invalid reconciliation task": "对账任务无效",
  "invalid referral list query": "推广列表查询条件无效",
  "invalid referral list query encoding": "推广列表查询编码无效",
  "invalid refund attestation":
    "核实内容无效：请选择结论，凭证号不超过 128 字、说明不超过 500 字",
  "invalid refund event subscription": "退款事件订阅无效",
  "invalid refund summary date": "退款汇总日期无效",
  "invalid refund summary filter": "退款汇总筛选条件无效",
  "invalid refund summary id": "退款汇总 ID 无效",
  "invalid refund summary query": "退款汇总查询无效",
  "invalid refund summary scope": "退款汇总范围无效",
  "invalid refund summary search": "退款汇总搜索条件无效",
  "invalid registered channel request": "已注册通道请求无效",
  "invalid report view": "报表视图无效",
  "invalid resource input": "资源输入无效",
  "invalid retained rate or unsupported collection mode":
    "留存费率无效或收款模式不受支持",
  "invalid risk action JSON": "风控操作 JSON 无效",
  "invalid risk action target": "风控操作对象无效",
  "invalid risk action values": "风控操作数值无效",
  "invalid runtime authorization source": "运行时授权来源无效",
  "invalid runtime channel policy JSON": "运行时通道策略 JSON 无效",
  "invalid runtime channel target": "运行时通道对象无效",
  "invalid runtime cleanup request": "运行时清理请求无效",
  "invalid runtime log date": "运行日志日期无效",
  "invalid runtime log filter": "运行日志筛选条件无效",
  "invalid runtime log pagination": "运行日志分页参数无效",
  "invalid runtime log query": "运行日志查询条件无效",
  "invalid runtime parent ID": "运行时父通道 ID 无效",
  "invalid runtime template request": "运行时模板请求无效",
  "invalid scan payment setting": "扫码支付设置无效",
  "invalid schedule or unavailable task executor":
    "计划无效或任务执行程序不可用",
  "invalid scheduled task JSON": "计划任务 JSON 无效",
  "invalid scheduled task runtime": "计划任务运行设置无效",
  "invalid schema request": "结构请求无效",
  "invalid search filter": "搜索筛选条件无效",
  "invalid settlement generation job": "结算生成任务无效",
  "invalid sharing fields": "分账字段无效",
  "invalid sharing overview query": "分账总览查询条件无效",
  "invalid sharing query": "分账查询条件无效",
  "invalid signature": "签名无效",
  "invalid signature policy confirmation": "签名策略确认信息无效",
  "invalid signature policy target": "签名策略对象无效",
  "invalid signed update manifest": "已签名的更新清单无效",
  "invalid sms gateway configuration": "短信网关配置无效",
  "invalid sms gateway fields": "短信网关字段无效",
  "invalid state transition": "状态变更无效",
  "invalid statistics query": "统计查询条件无效",
  "invalid style ID": "样式 ID 无效",
  "invalid subchannel request": "子通道请求无效",
  "invalid subject content type": "主体内容类型无效",
  "invalid sync task body": "同步任务内容无效",
  "invalid sync task status body": "同步任务状态内容无效",
  "invalid system log filter": "系统日志筛选条件无效",
  "invalid task history pagination": "任务历史分页参数无效",
  "invalid template field": "模板字段无效",
  "invalid template snapshot": "模板快照无效",
  "invalid trailing JSON": "JSON 末尾有多余内容",
  "invalid transfer method": "转账方式无效",
  "invalid transfer route": "转账路由无效",
  "invalid trusted update configuration": "可信更新配置无效",
  "invalid typed template settings or image": "模板设置或图片无效",
  "invalid upload": "上传内容无效",
  "invalid upstream subject policy": "上游商品名称策略无效",
  "invalid voice configuration": "语音配置无效",
  "invalid voice settings types": "语音设置字段类型无效",
  "invalid website settings": "网站设置无效",
  "invitation access forbidden": "无权访问该邀请",
  "invitation request changed; use original request":
    "邀请请求已变化，请使用原请求",
  "job lease lost": "任务租约已失效",
  "job scheduled": "任务已安排",
  "key login could not be completed": "密钥登录未能完成",
  "key login could not be verified": "无法核验密钥登录",
  "known dead job required": "需要已确认失败的任务",
  "layout query parameters not accepted": "版式接口不接受查询参数",
  "layout unavailable": "版式暂不可用",
  "ledger review recorded; no external payout": "已记录账务复核，未向外付款",
  "ledger statistics busy": "账务统计正在计算，请稍后再试",
  "legacy history reader unavailable": "历史记录读取暂不可用",
  "legacy import conflicts with target or existing export":
    "旧数据导入与目标或已有导出冲突",
  "legacy referral alias method unavailable": "旧推广别名方式暂不可用",
  "legacy snapshot validation failed": "旧数据快照校验未通过",
  "limit 1 to 100 required": "每页数量必须为 1–100",
  "limit exceeds 100": "每页数量不能超过 100",
  "list filter owner forbidden": "无权使用该列表筛选范围",
  "live financial operations disabled": "真实资金操作未开启",
  "live provider operations disabled": "真实服务商操作未开启",
  "local capabilities only; no provider request made":
    "仅显示本地能力，未向服务商发起请求",
  "local case comment recorded; no upstream action":
    "已记录本地备注，未向上游提交",
  "local funding preview; reservation revalidates original evidence":
    "本地资金预览；预留时会重新核验原始凭证",
  "local geographic database unavailable or invalid":
    "本地地理位置库不可用或无效",
  "local original URL Link status": "本地原 URL Link 状态",
  "local review recorded; no upstream refund or onboarding action":
    "已记录本地审核，未向上游发起退款或进件操作",
  "local simulated lost response": "本地模拟：响应丢失",
  "local update package could not be verified": "无法核验本地更新包",
  "login verification busy": "登录验证繁忙，请稍后再试",
  "login verification unavailable": "登录验证暂不可用",
  "lookup unavailable": "查询暂不可用",
  "mail account changed": "邮件账号已变更",
  "mail account changed; reload before confirming":
    "邮件账号已变更，请刷新后再确认",
  "mail account source cannot be confirmed": "无法确认邮件账号来源",
  "mail account source could not be confirmed": "无法确认邮件账号来源",
  "mail account unavailable": "邮件账号暂不可用",
  "mail frequency changed": "邮件发送频率已变更",
  "mail frequency changed; reload before confirming":
    "邮件发送频率已变更，请刷新后再确认",
  "mail frequency source cannot be confirmed": "无法确认邮件发送频率来源",
  "mail frequency source could not be confirmed": "无法确认邮件发送频率来源",
  "mail frequency unavailable": "邮件发送频率暂不可用",
  "mail test unavailable": "测试邮件暂不可用",
  "mandatory account session revocation incomplete":
    "未能完成必要的账号会话撤销",
  "masked current version only; no existing bearer reveal":
    "仅显示当前版本的掩码，不会再次显示已有令牌",
  "matrix exceeds 50000 merchant/dimension groups; narrow dates":
    "矩阵超过 50000 个商户/维度分组，请缩小日期范围",
  "matrix exceeds 50000 source transfers; narrow dates":
    "矩阵超过 50000 条来源转账，请缩小日期范围",
  "maximum file size is 512 KiB": "文件不能超过 512 KiB",
  "membership purchased from available balance": "已使用可用余额购买会员",
  "membership referral policy unavailable": "会员推广策略暂不可用",
  "membership referral service unavailable": "会员推广服务暂不可用",
  "merchant API admission could not be confirmed": "无法确认商户 API 准入",
  "merchant API disabled": "商户 API 已关闭",
  "merchant RSA actor unavailable": "商户 RSA 操作人暂不可用",
  "merchant RSA key changed; refresh before retrying":
    "商户 RSA 密钥已变更，请刷新后重试",
  "merchant RSA password rejected": "商户 RSA 密码错误",
  "merchant account required": "需要商户账号",
  "merchant cashier authority could not be confirmed": "无法确认商户收银台权限",
  "merchant cashier unavailable": "商户收银台暂不可用",
  "merchant combined payment policy changed or expired":
    "商户合单支付策略已变更或已过期",
  "merchant deposit policy changed or expired": "商户保证金策略已变更或已过期",
  "merchant disabled": "商户已停用",
  "merchant feature unavailable": "商户功能暂不可用",
  "merchant features unavailable": "商户功能暂不可用",
  "merchant identity required": "需要商户身份",
  "merchant is not available": "商户不可用",
  "merchant key login rejected": "商户密钥登录被拒绝",
  "merchant not found": "未找到该商户",
  "merchant notice configuration unavailable": "商户通知配置暂不可用",
  "merchant ownership required": "需要商户本人的数据",
  "merchant payment domain policy changed or expired":
    "商户支付域名策略已变更或已过期",
  "merchant real-name certification required": "商户需要先完成实名认证",
  "merchant refunds are disabled by platform policy": "平台已关闭商户退款",
  "merchant report scope comes only from the session":
    "商户报表范围只能来自当前登录",
  "merchant scope comes from original session": "商户范围以本人登录为准",
  "merchant scope is determined by the session": "商户范围由当前登录决定",
  "merchant session required": "需要商户登录",
  "merchant statistics contains an unsupported payment method":
    "商户统计包含不支持的支付方式",
  "merchant unavailable": "商户不可用",
  "merchant_id required": "缺少 merchant_id",
  "method not allowed": "不支持该请求方式",
  "method unavailable": "该请求方式暂不可用",
  "migration staging capacity reached": "迁移暂存空间已满",
  "migration target contains business data": "迁移目标库已有业务数据",
  "migration target is busy": "迁移目标正忙",
  "migration wizard state changed": "迁移向导状态已变化",
  "migration wizard unavailable": "迁移向导暂不可用",
  "mini authorization could not be completed": "小程序授权未能完成",
  "mini identity could not be stored": "无法保存小程序身份",
  "mini identity exchange failed": "小程序身份交换失败",
  "mini identity state unavailable or consumed": "小程序身份状态不可用或已使用",
  "mini payer verified; payment requires provider confirmation":
    "小程序付款人已核验，支付仍需服务商确认",
  "mini profile required": "需要小程序档案",
  "minimum fees changed; inspect before confirming":
    "最低手续费已变更，请核对后再确认",
  "minimum fees could not be confirmed": "无法确认最低手续费",
  "minimum fees saved for future orders":
    "最低手续费已保存，仅对之后的订单生效",
  "minimum fees unavailable": "最低手续费暂不可用",
  "missing file": "缺少文件",
  "missing plan definition field": "缺少计划定义字段",
  "multipart template package required": "请以 multipart 方式上传模板包",
  "native scan URL must be the configured cashier page":
    "原生扫码地址必须是已配置的收银台页面",
  "native scan application requires explicit current authorization":
    "原生扫码应用需要当前明确授权",
  "native scan application unavailable for this merchant":
    "该商户不能使用原生扫码应用",
  "native scan authority changed; reload before scanning":
    "原生扫码授权已变化，请刷新后再扫码",
  "native scan configuration unavailable": "原生扫码配置暂不可用",
  "native scan settings unavailable": "原生扫码设置暂不可用",
  "native scan signature could not be confirmed": "无法确认原生扫码签名",
  "native scan signature profile could not be confirmed":
    "无法确认原生扫码签名档案",
  "native scan source changed; reload and explicitly confirm current selection":
    "原生扫码来源已变化，请刷新并重新确认当前选择",
  "negative delay": "延迟时间不能为负数",
  "new bearer token shown once; previous token invalidated; no automatic retry":
    "新令牌仅显示一次，旧令牌已失效，不会自动重试",
  "newest 50 versions of the uploaded template": "上传模板的最近 50 个版本",
  "no configured recharge payment provider": "未配置充值支付服务商",
  "no verified refund provider configured": "未配置已核验的退款服务商",
  "no verified transfer provider configured": "未配置已核验的转账服务商",
  "not found": "未找到",
  "notice access denied": "无权访问该通知",
  "notice configuration access denied": "无权访问通知配置",
  "notice configuration unavailable": "通知配置暂不可用",
  "notice method unavailable": "通知方式暂不可用",
  "notice operation unavailable": "通知操作暂不可用",
  "notice owner comes from original session and path":
    "通知所有者以本人登录和路径为准",
  "notice owner not found": "未找到通知所有者",
  "notice version or original proof conflict; reload before editing":
    "通知版本或原始凭证冲突，请刷新后再编辑",
  "notification account changed or damaged": "通知账号已变更或已损坏",
  "notification accounts unavailable": "通知账号暂不可用",
  "notification already has an active delivery": "该通知已有正在进行的投递",
  "notification delivery failed": "通知投递失败",
  "notification delivery interrupted": "通知投递已中断",
  "notification delivery status": "通知投递状态",
  "notification resend cooldown": "通知重发冷却中，请稍后再试",
  "notification source conflict": "通知来源冲突",
  "oauth credential operation could not be confirmed; reload before retrying":
    "OAuth 凭据操作未能确认，请刷新后重试",
  "oauth credential settings changed": "OAuth 凭据设置已变更",
  "oauth credential settings unavailable": "OAuth 凭据设置暂不可用",
  "object required": "请求体必须为对象",
  "observation not found": "未找到该观察记录",
  "offline refund recorded; no provider refund sent":
    "已记录线下退款，未向服务商发送退款",
  "older version restored as a new version; administrators are notified":
    "已将旧版本恢复为新版本，并通知管理员",
  "one PNG or JPEG file required": "请上传一个 PNG 或 JPEG 文件",
  "one image file required": "请上传一个图片文件",
  "one multipart file required": "请上传一个文件",
  "one multipart image file required": "请上传一个图片文件",
  "one original external_no required": "请提供一个原 external_no",
  "one supported template type required": "请选择一种受支持的模板类型",
  "one to one hundred batch items required": "批次明细数量必须为 1–100",
  "one zip file required": "请上传一个 zip 文件",
  "one-time application credential": "一次性应用凭据",
  "one-time identity result": "一次性身份结果",
  "only expired sessions, login throttle entries and login-audit rows are eligible; financial records are immutable":
    "只能清理过期会话、登录限流记录和登录审计记录；资金记录不可删除",
  "only known dead notification or query jobs can be retried":
    "只能重试已确认失败的通知或查询任务",
  "only one file part accepted": "只接受一个文件",
  "only one file part is allowed": "只允许一个文件",
  "only one image is allowed": "只允许一张图片",
  "only the original super administrator can configure an account":
    "只有原超级管理员可以配置账号",
  "only unsent batch items cancelled": "只取消了尚未发送的批次明细",
  "operation failed": "操作失败",
  "operation forbidden": "不允许此操作",
  "operation not found": "未找到该操作",
  "operation not supported by this channel": "当前通道不支持此操作",
  "order alias GET has no request body": "订单别名查询不接受请求体",
  "order alias method unavailable": "订单别名请求方式暂不可用",
  "order alias query parameters are not accepted": "订单别名接口不接受查询参数",
  "order alias target unavailable": "订单别名对象暂不可用",
  "order batch could not be confirmed": "无法确认订单批次",
  "order batch not found": "未找到该订单批次",
  "order batch service unavailable": "订单批次服务暂不可用",
  "order income ledger operation complete": "订单收入入账已完成",
  "order not found": "未找到该订单",
  "order or alias source changed; reload before saving":
    "订单或别名来源已变化，请刷新后再保存",
  "order rejected": "订单被拒绝",
  "order service unavailable": "订单服务暂不可用",
  "order source or original request changed": "订单来源或原请求已变化",
  "order statistics busy": "订单统计正在计算，请稍后再试",
  "original SDK state required": "需要原 SDK 状态",
  "original URL Link authority changed": "原 URL Link 授权已变化",
  "original URL Link authority unavailable": "原 URL Link 授权暂不可用",
  "original URL Link intent unavailable": "原 URL Link 请求暂不可用",
  "original URL Link profile changed": "原 URL Link 档案已变化",
  "original URL Link result unavailable": "原 URL Link 结果暂不可用",
  "original URL Link status; SDK identity only":
    "原 URL Link 状态；仅 SDK 身份",
  "original account session required": "需要账号本人登录",
  "original active account session required": "需要账号本人的有效登录",
  "original administrator required": "需要管理员本人操作",
  "original administrator session required": "需要管理员本人登录",
  "original authorized administrator session required":
    "需要已授权的管理员本人登录",
  "original authorized session required": "需要已授权的本人登录",
  "original barcode cancellation outcome requires reconciliation":
    "原付款码撤销结果需要先完成对账",
  "original barcode payment has been cancelled": "原付款码支付已撤销",
  "original batch execution state": "原批次执行状态",
  "original batch request state": "原批次请求状态",
  "original blacklist record no longer exists": "原黑名单记录已不存在",
  "original business notice proof conflict": "原业务通知凭证冲突",
  "original cashier request could not be confirmed": "无法确认原收银台请求",
  "original console session required": "需要后台本人登录",
  "original durable account session required": "需要账号本人的持久登录",
  "original email binding session required": "需要发起邮箱绑定时的原登录",
  "original export format required": "请选择原导出格式",
  "original identity profile unavailable": "原身份档案暂不可用",
  "original identity request unavailable": "原身份请求暂不可用",
  "original immutable reward and derived refund state":
    "原始不可变奖励及其退款衍生状态",
  "original merchant session required":
    "该功能需要商户本人登录，请使用商户账号登录后操作。",
  "original mini-program URL Link profile unavailable":
    "原小程序 URL Link 档案暂不可用",
  "original notifications queued; delivery remains separate":
    "原通知已排队，投递结果另行查看",
  "original owner or super administrator required": "需要原所有者或超级管理员",
  "original owner session required": "需要原所有者本人登录",
  "original payment is not verified paid; no completion recorded":
    "原支付尚未核验为已付款，未记录完成",
  "original payment outcome remains unconfirmed; query the same request":
    "原支付结果仍未确认，请查询同一请求",
  "original payment outcome remains unconfirmed; read the same request":
    "原支付结果仍未确认，请读取同一请求",
  "original payment query unavailable": "原支付查询暂不可用",
  "original payment verified and posting recovered": "原支付已核验，入账已恢复",
  "original payout outcome pending; query it without resubmitting":
    "原付款结果处理中，请查询原请求，不要重复提交",
  "original persistent administrator session required":
    "需要管理员本人的持久登录",
  "original persistent super administrator session required":
    "需要超级管理员本人的持久登录",
  "original printing state does not permit this operation":
    "原打印状态不允许此操作",
  "original public test payment requires read-only verification":
    "原公开测试支付需要只读核验",
  "original request cannot be submitted; recover the same request":
    "原请求不能提交，请恢复同一请求",
  "original request not found; do not create another charge while outcome is unknown":
    "未找到原请求；结果未知时请勿再次发起扣款",
  "original request recorded; approval is not external payment":
    "已记录原请求；审批通过不代表已向外付款",
  "original request unavailable or application changed":
    "原请求不可用或应用已变更",
  "original risk check result could not be confirmed": "无法确认原风控检查结果",
  "original risk check result is not confirmed": "原风控检查结果未确认",
  "original risk check source could not be confirmed": "无法确认原风控检查来源",
  "original runtime cleanup request changed": "原运行清理请求已变化",
  "original scan cancellation requires reconciliation":
    "原扫码撤销需要先完成对账",
  "original scan payment was cancelled": "原扫码支付已撤销",
  "original scan query interval has not elapsed": "原扫码查询间隔尚未到",
  "original selection prepared; no business sent":
    "已准备原选择，未发送业务请求",
  "original session required": "需要本人登录",
  "original settlement request IDs required": "需要原结算请求 ID",
  "original settlement state does not permit this batch action":
    "原结算状态不允许此批量操作",
  "original super administrator required": "需要原超级管理员",
  "original super administrator required for historical funding adoption":
    "历史资金承接需要原超级管理员",
  "original super administrator required for recipient export":
    "导出收款人需要原超级管理员",
  "original super administrator session required": "需要超级管理员本人登录",
  "original test email request not found": "未找到原测试邮件请求",
  "original transfer display cannot be verified": "无法核验原转账显示信息",
  "outbound destination is not a public HTTP endpoint":
    "目标地址不是公网 HTTP 地址",
  "overview uses today, yesterday and the latest 7 business days":
    "总览统计今天、昨天和最近 7 个业务日",
  "own administrator required": "需要管理员本人",
  "own administrator session required": "需要管理员本人登录",
  "own authenticated session required":
    "该功能需要商户本人登录，请使用商户账号登录后操作。",
  "own authorized administrator required": "需要已授权的管理员本人",
  "own merchant or authorized administrator required":
    "需要商户本人或已授权的管理员",
  "own merchant or original super administrator session required":
    "需要商户本人或原超级管理员登录",
  "own merchant or super administrator session required":
    "需要商户本人或超级管理员登录",
  "own merchant session required":
    "该功能需要商户本人登录，请使用商户账号登录后操作。",
  "own merchant session required for membership purchase":
    "该功能需要商户本人登录，请使用商户账号登录后操作。",
  "own session required": "需要本人登录",
  "own super administrator required": "需要超级管理员本人",
  "own super administrator session required": "需要超级管理员本人登录",
  "own verified original daily entitlements; pending amounts are not spendable":
    "本人已核验的原每日权益；待发金额不可使用",
  "page template not found": "未找到该页面模板",
  "page template operation forbidden": "不允许此页面模板操作",
  "page template revision conflict": "页面模板版本冲突，请刷新后重试",
  "paid notification order required": "需要已付款且有通知地址的订单",
  "paid or pending marker required": "需要已付款或待处理标记",
  "paid order with a valid stored return address required":
    "需要已付款且保存了有效返回地址的订单",
  "paid order with stored notification URL required":
    "需要已付款且保存了通知地址的订单",
  "parent registry unavailable": "父通道登记暂不可用",
  "pay and wait for verified callback before balance credit":
    "请先付款，等待已核验的回调后才会入账",
  "pay type closed": "该支付方式已关闭",
  "pay type options take no parameters": "支付方式选项不接受参数",
  "payment channel not permitted": "该支付通道不允许使用",
  "payment channel source could not be confirmed": "无法确认支付通道来源",
  "payment credential store unavailable": "支付凭据存储暂不可用",
  "payment defend group could not be confirmed": "无法确认支付验证用户组",
  "payment defend group or policy changed; reload before saving":
    "支付验证用户组或策略已变化，请刷新后再保存",
  "payment defend group unavailable": "支付验证用户组暂不可用",
  "payment defend method unavailable": "支付验证方式暂不可用",
  "payment defend policy could not be confirmed": "无法确认支付验证策略",
  "payment defend policy source or revision conflict":
    "支付验证策略来源或版本冲突",
  "payment defend policy unavailable": "支付验证策略暂不可用",
  "payment defend query parameters are not accepted":
    "支付验证接口不接受查询参数",
  "payment defend source or revision changed; reload before saving":
    "支付验证来源或版本已变化，请刷新后再保存",
  "payment defend target unavailable": "支付验证对象暂不可用",
  "payment outcome requires query": "支付结果需要查询确认",
  "payment outcome requires query; no balance credited by this endpoint":
    "支付结果需要查询确认；此接口不会入账",
  "payment provider unavailable": "支付服务商暂不可用",
  "payment rejected by configured policy": "支付被已配置的策略拒绝",
  "payment statistics busy": "支付统计正在计算，请稍后再试",
  "payment subchannel account is not currently executable":
    "该支付子通道账户当前不可执行",
  "payment subchannel changed; refresh before confirming":
    "支付子通道已变化，请刷新后再确认",
  "payment subchannel could not be confirmed": "无法确认支付子通道",
  "payment subchannel is unavailable for the current group or parent":
    "当前用户组或父通道不能使用该支付子通道",
  "payment subchannel unavailable": "支付子通道暂不可用",
  "payment subchannels unavailable": "支付子通道暂不可用",
  "persistent account store unavailable": "持久账号存储暂不可用",
  "persistent merchant authentication unavailable": "持久商户认证暂不可用",
  "persistent original administrator session required":
    "需要管理员本人的持久登录",
  "persistent sessions required": "需要持久登录会话",
  "personal settings changed; refresh before saving":
    "个人设置已变化，请刷新后再保存",
  "personal settings could not be confirmed": "无法确认个人设置",
  "personal settings saved": "个人设置已保存",
  "personal settings unavailable": "个人设置暂不可用",
  "platform access changed; read before confirming":
    "平台访问设置已变化，请先读取再确认",
  "platform access could not be confirmed": "无法确认平台访问设置",
  "platform access unavailable": "平台访问设置暂不可用",
  "policy affects new balance membership purchases only":
    "该策略只影响新的余额购买会员",
  "policy affects new original payment snapshots only":
    "该策略只影响新的原支付快照",
  "preview only; apply locally, then use the existing batch review and funds authorization":
    "仅为预览；请先在本地应用，再走现有批次审核和资金授权",
  "printer gateway outcome unknown; inspect original print intent":
    "打印网关结果未知，请查看原打印请求",
  "printing evidence conflict": "打印凭证冲突",
  "printing method unavailable": "打印方式暂不可用",
  "printing operation unavailable": "打印操作暂不可用",
  "printing record not found": "未找到该打印记录",
  "printing revision or original evidence conflict": "打印版本或原始凭证冲突",
  "printing service unavailable": "打印服务暂不可用",
  "printing state recorded; accepted is not physical printing confirmation":
    "已记录打印状态；已受理不代表已实际打印",
  "printing tenant and scope come from the original session and path":
    "打印租户和范围以本人登录和路径为准",
  "private background uploaded; publish by referencing an enabled collection style":
    "私有背景已上传；被已启用的收款样式引用后才会发布",
  "private image uploaded; becomes public only while referenced by the selected template":
    "私有图片已上传；仅在被所选模板引用时公开",
  "private search too broad; narrow account or name":
    "隐私搜索范围过大，请缩小账号或名称",
  "profile changed; reload original capabilities":
    "档案已变化，请重新读取原能力",
  "proof application unresolved; it will not be submitted again":
    "证明材料申请未处理完成，不会再次提交",
  "proof unavailable or not enabled": "证明材料不可用或未开启",
  "protected notice source conflict": "受保护的通知来源冲突",
  "provider complaint action could not be confirmed":
    "无法确认服务商投诉处理结果",
  "provider credentials required": "需要服务商凭据",
  "provider operation not enabled": "未开启服务商操作",
  "provider operations unavailable": "服务商操作暂不可用",
  "provider order closed": "服务商订单已关闭",
  "provider outcome requires query": "服务商结果需要查询确认",
  "provider refund result": "服务商退款结果",
  "province statistics busy": "省份统计正在计算，请稍后再试",
  "province statistics exceed 5000 groups; narrow filters":
    "省份统计超过 5000 个分组，请缩小筛选范围",
  "proxy credential operation could not be confirmed; reload before retrying":
    "代理凭据操作未能确认，请刷新后重试",
  "proxy credential settings changed": "代理凭据设置已变更",
  "proxy credential settings unavailable": "代理凭据设置暂不可用",
  "proxy did not return an IP echo": "代理没有返回 IP 回显",
  "proxy offered no acceptable SOCKS5 auth method":
    "代理没有提供可接受的 SOCKS5 认证方式",
  "proxy rejected SOCKS5 credentials": "代理拒绝了 SOCKS5 凭据",
  "public browser authorization required": "需要公开浏览器授权",
  "public test payment is closed": "公开测试支付已关闭",
  "public test payment unavailable": "公开测试支付暂不可用",
  "query authority is not accepted": "不接受查询授权参数",
  "query not accepted": "不接受查询参数",
  "query parameters are not accepted": "不接受查询参数",
  "query parameters are not accepted for historical funding adoption":
    "历史资金承接不接受查询参数",
  "query parameters are not accepted for sharing writes":
    "分账写入不接受查询参数",
  "query parameters not accepted": "不接受查询参数",
  "read-only business notice history": "只读业务通知历史",
  "reason, external_no and canonical expected_revision required":
    "需要原因、external_no 和有效的 expected_revision",
  "recipient version recorded; existing applications unchanged":
    "已记录收款人版本，已有申请不变",
  "recorded sources only; unverified subjects remain unknown":
    "仅显示已记录来源；未核验主体仍为未知",
  "red packet already claimed": "红包已被领取",
  "red packet expired": "红包已过期",
  "red packet method unavailable": "红包方式暂不可用",
  "red packet unavailable": "红包暂不可用",
  "referral operation unavailable": "推广操作暂不可用",
  "referral owner or super administrator required":
    "需要推广所有者或超级管理员",
  "referral policy unavailable": "推广策略暂不可用",
  "referral service unavailable": "推广服务暂不可用",
  "refund attestation unavailable": "退款核实暂不可用",
  "refund cannot be attested in its current state":
    "该退款当前状态不能人工核实，请重新读取",
  "refund event connection not established": "退款事件连接未建立",
  "refund event delivery result unavailable": "退款事件投递结果暂不可用",
  "refund event request construction failed": "退款事件请求构建失败",
  "refund event signing unavailable": "退款事件签名暂不可用",
  "refund event source cannot be confirmed": "无法确认退款事件来源",
  "refund event subscription changed": "退款事件订阅已变更",
  "refund funding unavailable": "退款资金暂不可用",
  "refund not found": "未找到该退款",
  "refund reserved; not sent to upstream": "已预留退款，尚未发送到上游",
  "refund statistics busy": "退款统计正在计算，请稍后再试",
  "refund submitted; outcome requires verified query":
    "退款已提交，结果需要核验查询",
  "registered parent unavailable": "已登记的父通道暂不可用",
  "registered runtime channel unavailable": "已登记的运行通道暂不可用",
  "registered scheduled task unavailable": "已登记的计划任务暂不可用",
  "registration is closed": "注册已关闭",
  "registration unavailable": "注册暂不可用",
  "remaining authorization release result": "剩余授权解除结果",
  "remaining release not enabled": "未开启剩余授权解除",
  "remaining release pending; preserve and query the original request":
    "剩余授权解除处理中，请保存并查询原请求",
  "report not found": "未找到该报表",
  "reports busy": "报表正在计算，请稍后再试",
  "request body exceeds 4KiB": "请求内容超过 4KiB",
  "required sharing field missing": "缺少必填的分账字段",
  "resource conflict": "资源冲突，请刷新后重试",
  "resource conflict or insufficient funds": "资源冲突或资金不足",
  "resource not found": "未找到该资源",
  "review this exact action before authorization": "请先核对本次操作再进行授权",
  "risk action changed; refresh before confirming":
    "风控操作已变化，请刷新后再确认",
  "risk action could not be confirmed": "无法确认风控操作",
  "risk action method unavailable": "风控操作方式暂不可用",
  "risk action unavailable": "风控操作暂不可用",
  "risk check read method unavailable": "风控检查读取方式暂不可用",
  "routing catalog accepts no filters": "路由目录不接受筛选条件",
  "runtime channel catalog unavailable": "运行通道目录暂不可用",
  "runtime channel not found": "未找到该运行通道",
  "runtime channel policy could not be confirmed": "无法确认运行通道策略",
  "runtime channel policy or identity changed; refresh before confirming":
    "运行通道策略或身份已变化，请刷新后再确认",
  "runtime channel policy updated": "运行通道策略已更新",
  "runtime channel registry unavailable": "运行通道登记暂不可用",
  "runtime channel write states": "运行通道写入状态",
  "runtime cleanup preview expired; read again":
    "运行清理预览已过期，请重新读取",
  "runtime event persistence unavailable": "运行事件存储暂不可用",
  "runtime log route not found": "未找到该运行日志路由",
  "runtime parent channel changed; read and confirm again":
    "运行时父通道已变化，请重新读取并确认",
  "runtime parent channel registry unavailable": "运行时父通道登记暂不可用",
  "runtime parent channel unavailable": "运行时父通道暂不可用",
  "runtime parent configuration could not be confirmed":
    "无法确认运行时父通道配置",
  "sandbox administrator required": "需要沙箱环境的管理员",
  "sandbox payment recorded": "已记录沙箱支付",
  "scan outcome requires authenticated query": "扫码结果需要已认证的查询确认",
  "scan payment setting changed or cannot be confirmed; refresh before confirming":
    "扫码支付设置已变化或无法确认，请刷新后再确认",
  "scan payment settings could not be confirmed": "无法确认扫码支付设置",
  "scan payment settings unavailable": "扫码支付设置暂不可用",
  "scheduled only; no execution result yet": "仅已安排，尚无执行结果",
  "scheduled task changed or is busy; refresh before confirming":
    "计划任务已变化或正忙，请刷新后再确认",
  "scheduled task configuration saved": "计划任务配置已保存",
  "scheduled task could not be confirmed": "无法确认计划任务",
  "scheduled task operation unavailable": "计划任务操作暂不可用",
  "scheduled task registry unavailable": "计划任务登记暂不可用",
  "security operation not found": "未找到该安全操作",
  "select one to one hundred orders and a stable request number":
    "请选择 1–100 个订单并使用固定的请求编号",
  "selected built-in template now affects page appearance only":
    "所选内置模板现在只影响页面外观",
  "selected independent profile unavailable": "所选独立档案暂不可用",
  "selected web OAuth identity route is unavailable":
    "所选网页 OAuth 身份路由暂不可用",
  "session cleanup unavailable": "会话清理暂不可用",
  "session expired": "登录已过期，请重新登录",
  "settlement batch not found": "未找到该结算批次",
  "settlement batches unavailable": "结算批次暂不可用",
  "settlement capability or writes unavailable": "结算能力或写入暂不可用",
  "settlement generation crossed original business date":
    "结算生成跨越了原业务日期",
  "settlement operation not found": "未找到该结算操作",
  "settlement payout not found": "未找到该结算付款",
  "settlement plan operation unavailable": "结算计划操作暂不可用",
  "settlement preference unavailable": "结算偏好暂不可用",
  "settlement provider unavailable": "结算服务商暂不可用",
  "settlement scheduler disabled": "结算调度未开启",
  "settlement scheduler unavailable": "结算调度暂不可用",
  "settlement scheduling unavailable": "结算安排暂不可用",
  "settlement storage unavailable": "结算存储暂不可用",
  "sharing fields cannot be null": "分账字段不能为空",
  "sharing overview is read-only": "分账总览为只读",
  "sharing overview operation not found": "未找到该分账总览操作",
  "sharing overview unavailable": "分账总览暂不可用",
  "signature policy change could not be confirmed; refresh current state":
    "签名策略变更未能确认，请刷新查看当前状态",
  "signature policy changed; refresh before confirming again":
    "签名策略已变化，请刷新后再确认",
  "signature policy unavailable": "签名策略暂不可用",
  "signature policy updated": "签名策略已更新",
  "signature verification failed": "签名校验失败",
  "single cleanup days required": "请提供一个清理天数",
  "single-use grant; merchant cookie will be replaced on consumption":
    "一次性授权；使用后将替换商户登录 Cookie",
  "site query parameters not accepted": "站点接口不接受查询参数",
  "sms gateway changed": "短信网关已变更",
  "sms gateway changed; reload before confirming":
    "短信网关已变更，请刷新后再确认",
  "sms gateway source cannot be confirmed": "无法确认短信网关来源",
  "sms gateway source could not be confirmed": "无法确认短信网关来源",
  "sms gateway unavailable": "短信网关暂不可用",
  "sms verification is not enabled": "未开启短信验证",
  "sms verification unavailable": "短信验证暂不可用",
  "specific administrator POST target and object body required":
    "需要指定管理员的 POST 目标和对象内容",
  "state, provider_reference and note required":
    "请填写核实结论、支付机构凭证号和说明",
  "statistics busy": "统计正在计算，请稍后再试",
  "statistics do not accept pagination or export": "统计不支持分页或导出",
  "statistics exceed 5000 groups; narrow filters":
    "统计超过 5000 个分组，请缩小筛选范围",
  "status not accepted for this action": "此操作不接受状态参数",
  "string CAPTCHA key required": "验证码密钥必须为文本",
  "string oauth secret required": "OAuth 密钥必须为文本",
  "string proxy API key required": "代理 API 密钥必须为文本",
  "string proxy password required": "代理密码必须为文本",
  "string proxy test field required": "代理测试字段必须为文本",
  "string required": "该字段必须为文本",
  "style access forbidden": "无权访问该样式",
  "style changed; reload before saving": "样式已变化，请刷新后再保存",
  "style deleted": "样式已删除",
  "style not found": "未找到该样式",
  "style query parameters not accepted": "样式接口不接受查询参数",
  "submitted for local review; no provider onboarding submission":
    "已提交本地审核，未向服务商提交进件",
  "super administrator required": "需要超级管理员权限",
  "super administrator required for financial policy":
    "资金策略需要超级管理员权限",
  "super administrator required for manual refund":
    "人工退款需要超级管理员权限",
  "sync task deleted": "同步任务已删除",
  "sync task not found": "未找到该同步任务",
  "sync task saved": "同步任务已保存",
  "sync task status updated": "同步任务状态已更新",
  "sync tasks deleted": "同步任务已删除",
  "synchronous notification could not be confirmed": "无法确认同步通知",
  "system log route not found": "未找到该系统日志路由",
  "system logs busy": "系统日志正在读取，请稍后再试",
  "template configuration not confirmed; reload before retrying":
    "模板配置未能确认，请刷新后重试",
  "template configuration saved; selected template appearance updates immediately":
    "模板配置已保存，所选模板外观立即生效",
  "template image processing busy": "模板图片处理繁忙，请稍后再试",
  "template image upload rate limited": "模板图片上传过于频繁，请稍后再试",
  "template operation not found": "未找到该模板操作",
  "template or image not found": "未找到该模板或图片",
  "template package checked; nothing was saved": "模板包已检查，未保存任何内容",
  "template package saved as a new version; administrators are notified":
    "模板包已保存为新版本，并通知管理员",
  "template revision conflict; reload original settings":
    "模板版本冲突，请刷新原设置",
  "template service unavailable": "模板服务暂不可用",
  "test email source could not be confirmed": "无法确认测试邮件来源",
  "test email submission is disabled": "测试邮件发送已关闭",
  "test email submission limit exceeded": "测试邮件发送次数已达上限",
  "test mail request not found": "未找到该测试邮件请求",
  "test mail submission limit exceeded": "测试邮件发送次数已达上限",
  "test request could not be prepared": "无法准备测试请求",
  "test request could not be recorded": "无法记录测试请求",
  "test request or account changed; inspect original request":
    "测试请求或账号已变化，请查看原请求",
  "token change rate limited": "令牌变更过于频繁，请稍后再试",
  "token revoked; existing registered relations preserved":
    "令牌已撤销，已有注册关系保留",
  "too many attempts": "登录尝试次数过多",
  "too many authenticator attempts": "身份验证码尝试次数过多，请稍后再试",
  "too many funds verification attempts": "资金密码尝试次数过多，请稍后再试",
  "too many images": "图片数量过多",
  "too many outstanding delegation grants": "未完成的代操作授权过多",
  "trailing JSON": "JSON 末尾有多余内容",
  "transfer capability or writes unavailable": "转账能力或写入暂不可用",
  "transfer channel not permitted or unsupported":
    "该转账通道不允许使用或不受支持",
  "transfer channel unavailable; reload choices":
    "转账通道暂不可用，请刷新选项",
  "transfer definitively rejected; funds released":
    "转账被上游明确拒绝，资金已释放",
  "transfer intent persisted; query the same transfer":
    "转账请求已保存，请查询同一笔转账",
  "transfer not found": "未找到该转账",
  "transfer operation unavailable": "转账操作暂不可用",
  "transfer provider unavailable": "转账服务商暂不可用",
  "transfer result": "转账结果",
  "transfer service unavailable": "转账服务暂不可用",
  "transfer settings unavailable": "转账设置暂不可用",
  "transfer statistics are read-only": "转账统计为只读",
  "transfer statistics unavailable": "转账统计暂不可用",
  "transfer writes disabled": "转账写入已关闭",
  "trusted proxy configuration too large": "可信代理配置过长",
  "typed built-in configuration; revision covers this template type":
    "内置模板配置；版本号覆盖该模板类型",
  "unclaimed automatic payouts stopped": "未领取的自动付款已停止",
  "unexpected batch query": "批次查询条件异常",
  "unexpected data after PEM key": "PEM 密钥后有多余内容",
  "unexpected sharing field": "分账字段异常",
  "unique canonical merchant IDs required": "请提供不重复的有效商户 ID",
  "unknown batch filter": "未知的批次筛选条件",
  "unknown certification operation": "未知的认证操作",
  "unknown field": "未知字段",
  "unknown history parameter": "未知的历史参数",
  "unknown migration operation": "未知的迁移操作",
  "unknown notification operation": "未知的通知操作",
  "unknown printing body": "未知的打印内容",
  "unknown provider": "未知的服务商",
  "unknown purpose": "未知的用途",
  "unknown system update action": "未知的系统更新操作",
  "unsupported CAPTCHA key field": "不支持的验证码密钥字段",
  "unsupported batch control": "不支持的批次控制",
  "unsupported binding scope": "不支持的绑定范围",
  "unsupported generator operation": "不支持的生成操作",
  "unsupported invitation action": "不支持的邀请操作",
  "unsupported job kind": "不支持的任务类型",
  "unsupported oauth credential field": "不支持的 OAuth 凭据字段",
  "unsupported or invalid SQL backup": "SQL 备份不受支持或无效",
  "unsupported payment version": "不支持的支付协议版本",
  "unsupported proxy credential field": "不支持的代理凭据字段",
  "unsupported proxy test field": "不支持的代理测试字段",
  "unsupported signature profile": "不支持的签名档案",
  "unsupported style action": "不支持的样式操作",
  "update facts changed; inspect before confirming":
    "更新信息已变化，请核对后再确认",
  "update package could not be verified": "无法核验更新包",
  "update preparation could not be confirmed; read original intent":
    "更新准备未能确认，请读取原更新请求",
  "update state changed; inspect before confirming":
    "更新状态已变化，请核对后再确认",
  "update state unavailable": "更新状态暂不可用",
  "uploaded template removed; its versions stay in the history":
    "上传的模板已删除，其版本仍保留在历史中",
  "upstream outcome unknown; query the original request only":
    "上游结果未知，请只查询原请求",
  "upstream rejected this request; inspect the original operation before any explicit new intent":
    "上游拒绝了此请求，请先查看原操作，再决定是否明确发起新请求",
  "valid amount and external_no required": "请提供有效的金额和 external_no",
  "valid external_no required": "请提供有效的 external_no",
  "valid merchant_id required": "请提供有效的 merchant_id",
  "valid refund amount required": "请提供有效的退款金额",
  "verification code source cannot be confirmed": "无法确认验证码来源",
  "verification delivery unavailable": "验证码发送暂不可用",
  "verification email submission unavailable": "验证邮件发送暂不可用",
  "verification rate limit exceeded": "验证次数超出限制，请稍后再试",
  "verification requested": "已发送验证请求",
  "verification service unavailable": "验证服务暂不可用",
  "verification unavailable": "验证暂不可用",
  "verified account version required": "需要已核验的账号版本",
  "verified authorization capture result": "已核验的预授权完成结果",
  "verified delegated identities required": "需要已核验的代操作身份",
  "verified paid receipts; refunds by original creation date; counts by original order creation date":
    "已核验的到账记录；退款按原创建日期，笔数按原订单创建日期",
  "verified provider outcome remains pending": "已核验的服务商结果仍在处理中",
  "verified provider query": "已核验的服务商查询",
  "verified provider transfer result": "已核验的服务商转账结果",
  "verified query unresolved; no unverified balance credit applied":
    "核验查询未完成，未入账任何未核验金额",
  "verified target successful logins only; unrecorded method/location remain empty":
    "仅显示目标账号已核验的成功登录；未记录的方式和地点留空",
  "verify both email addresses to change the binding":
    "需要验证两个邮箱地址才能修改绑定",
  "visible groups must be existing IDs separated by commas; 0 is the default group":
    "可见用户组必须是以逗号分隔的现有 ID；0 表示默认用户组",
  "voice configuration changed": "语音配置已变更",
  "voice configuration could not be confirmed; reload before retrying":
    "语音配置未能确认，请刷新后重试",
  "voice configuration unavailable": "语音配置暂不可用",
  "wallet products unavailable": "钱包产品暂不可用",
  "website settings unavailable": "网站设置暂不可用",
};

function byStatus(status: number): string {
  if (status === 400) return "请求内容不正确";
  if (status === 401) return "登录已失效，请重新登录";
  if (status === 403) return "没有权限执行此操作";
  if (status === 404) return "请求的内容不存在";
  if (status === 405) return "不支持该请求方式";
  if (status === 409) return "数据已变化或存在冲突，请刷新后重试";
  if (status === 413) return "提交的内容过大";
  if (status === 422) return "输入内容或当前状态不允许此操作";
  if (status === 428) return "需要先完成本次操作授权";
  if (status === 429) return "操作过于频繁，请稍后再试";
  if (status >= 500) return "服务暂时不可用，请稍后重试";
  return "请求未完成";
}

const han = /[\u3400-\u9fff]/;

/** Whether the console has a Chinese translation for an API message. */
export function hasServerMessageTranslation(message: string): boolean {
  return Object.prototype.hasOwnProperty.call(zh, message.trim());
}

/**
 * Text to show for an API error. Chinese server messages pass through; known
 * English ones are translated; anything else is explained by HTTP status with
 * the original kept in parentheses for support.
 */
export function localizeServerMessage(
  message: string,
  status: number,
  retryAfterSeconds?: number,
): string {
  const original = message.trim();
  let text: string;
  if (han.test(original)) text = original;
  else if (original && hasServerMessageTranslation(original))
    text = zh[original];
  else
    text = original ? `${byStatus(status)}（${original}）` : byStatus(status);
  if (
    status === 429 &&
    retryAfterSeconds !== undefined &&
    retryAfterSeconds >= 60 &&
    !/后再试|后重试/.test(text)
  )
    text += `，请约 ${Math.ceil(retryAfterSeconds / 60)} 分钟后再试`;
  return text;
}
