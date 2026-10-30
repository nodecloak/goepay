import { siteKeys191 } from "./site191";
export const settingsTabs = [
  ["site", "网站信息"],
  ["merchant", "商户相关"],
  ["pay", "支付相关"],
  ["complaint", "交易投诉"],
  ["risk", "风控检测"],
  ["settle", "结算规则"],
  ["transfer", "转账付款"],
  ["oauth", "快捷登录"],
  ["notice", "消息提醒"],
  ["certificate", "实名认证"],
  ["ocr", "OCR配置"],
  ["anounce", "公告排版"],
  ["mail", "邮箱配置"],
  ["sms", "短信配置"],
  ["iptype", "IP地址"],
  ["proxy", "中转代理"],
  ["advanced", "高级设置"],
] as const;
export type SettingsTab = (typeof settingsTabs)[number][0];
/** Panel titles as the original console shows them (网站信息配置 …). */
export const settingsTitles: Record<SettingsTab, string> = {
  site: "网站信息配置",
  merchant: "商户相关配置",
  pay: "支付相关配置",
  complaint: "交易投诉配置",
  risk: "风控检测配置",
  settle: "结算规则配置",
  transfer: "转账付款配置",
  oauth: "快捷登录配置",
  notice: "消息提醒配置",
  certificate: "实名认证配置",
  ocr: "OCR文字识别配置",
  anounce: "公告排版配置",
  mail: "邮箱配置",
  sms: "短信配置",
  iptype: "IP地址配置",
  proxy: "中转代理配置",
  advanced: "高级设置",
};
/** Separately saved setting blocks that sit between ordinary fields. */
export type SettingsEmbed =
  | "brand"
  | "captcha-key"
  | "keylogin"
  | "scanpay"
  | "scanjs"
  | "user-refund"
  | "buyer-fee"
  | "proxy-credentials"
  | "oauth-credentials";
type LayoutEntry = string | { group: string } | { embed: SettingsEmbed };
// Field order follows the original epay_pro settings pages. A trailing "*"
// matches a key prefix; keys not listed keep their server order at the end.
const settingsLayout: Partial<Record<SettingsTab, LayoutEntry[]>> = {
  site: [
    "sitename",
    "site_name",
    "title",
    "keywords",
    "description",
    "orgname",
    "email",
    "support_email",
    "kfqq",
    "kfqq_url",
    "wework_url",
    "qqqun",
    "login_subtitle",
    { embed: "brand" },
    "test_open",
    "test_pay_uid",
    "captcha_open_test",
    "captcha_id",
    { embed: "captcha-key" },
    "captcha_version",
    "captcha_open_login",
    "homepage",
    "homepage_url",
    { embed: "keylogin" },
  ],
  merchant: [
    { group: "登录&注册配置" },
    "verifytype",
    "user_totp_open",
    "registration_open",
    "registration_invite_required",
    "user_review",
    "reg_pay",
    "reg_pay_price",
    "reg_pay_uid",
    { group: "商户功能配置" },
    "internal_transfer",
    "recharge",
    "onecode",
    "onecode_domains",
    { embed: "scanpay" },
    { embed: "scanjs" },
    "pay_domain_open",
    "enforce_domains",
    { embed: "user-refund" },
    "refund_fee_payer",
    "subchannel_open",
    "user_profitsharing",
    { group: "进件配置（本轮暂缓）" },
    "applyments_open",
    "mchrisk_open",
  ],
  pay: [
    "pay_maxmoney",
    "pay_minmoney",
    "payment_fee_enabled",
    { embed: "buyer-fee" },
    "pay_force_cashier_pids",
    { group: "商品名称与屏蔽" },
    "pageordername",
    "notifyordername",
    "blockname",
    "blockalert",
    "blackmsg",
    "forceqq",
    { group: "合单支付" },
    "wxcombine_open",
    "wxcombine_minmoney",
    "wxcombine_submoney",
  ],
  complaint: ["complain_open"],
  settle: [
    "settle_open",
    "settle_type",
    "settlement_daily_limit",
    "settlement_minimum_cents",
    "settlement_maximum_cents",
    "settlement_fee_rate_bps",
    "settlement_fee_minimum_cents",
    "settlement_fee_maximum_cents",
    "settlement_retained_cents",
    "auto_settle_money",
    { group: "各收款方式结算开关" },
    "settle_alipay",
    "settle_wxpay",
    "settle_qqpay",
    "settle_bank",
  ],
  transfer: [
    "transfer_enabled",
    "transfer_name",
    "transfer_desc",
    "transfer_alipay",
    "transfer_wxpay",
    "transfer_wxpay_type",
    "transfer_wxpay_scene_id",
    "transfer_wxpay_info_type",
    "transfer_wxpay_info_content",
    "transfer_qqpay",
    "transfer_bank",
    { group: "代付配置" },
    "user_transfer",
    "user_transfer_red",
    "user_transfer_batch",
    "transfer_minmoney",
    "transfer_maxmoney",
    "transfer_maxlimit",
  ],
  oauth: [
    { group: "QQ互联" },
    "login_qq",
    "login_qq_appid",
    { group: "微信开放平台" },
    "login_wx",
    { group: "支付宝开放平台" },
    "login_alipay",
    { group: "第三方登录中转" },
    "login_appid",
    "login_apiurl",
    { group: "Telegram" },
    "login_tg",
    { embed: "oauth-credentials" },
  ],
  notice: [
    "msgconfig_regaudit",
    "msgconfig_apply",
    "msgconfig_domain",
    "msgconfig_risk",
    "mail_recv",
    "mail_name",
    "msgtelegram_chatid",
    "qqbot_openid",
    "telegramnotice",
    "qqbotnotice",
    "msgconfig_order",
    "msgconfig_settle",
    "msgconfig_login",
    "msgconfig_complain",
    "msgconfig_mchrisk",
    "msgconfig_balance",
    "msgconfig_group",
    "msgconfig_domain_audit",
    "msgconfig_daily_report",
    "msgconfig_*",
    "wxnotice_tpl_order*",
    "wxnotice_tpl_settle*",
    "wxnotice_tpl_login*",
    "wxnotice_tpl_complain*",
    "wxnotice_tpl_balance*",
    "wxnotice_tpl_*",
    "voicenotice",
  ],
  anounce: ["modal", "zhuce", "footer"],
  proxy: [
    "proxy",
    "proxy_server",
    "proxy_port",
    "proxy_user",
    "proxy_type",
    "proxy_apiurl",
    { embed: "proxy-credentials" },
  ],
};
export type SettingsBlock =
  | { type: "group"; key: string; title: string }
  | { type: "field"; key: string; definition: SettingDefinition }
  | { type: "embed"; key: string; embed: SettingsEmbed };
/** Ordered page content for one tab: headings, fields and embedded blocks. */
export function settingsBlocks(
  tab: SettingsTab,
  fields: readonly SettingDefinition[],
): SettingsBlock[] {
  const used = new Set<string>(),
    blocks: SettingsBlock[] = [];
  const matches = (pattern: string, key: string) =>
    pattern.endsWith("*")
      ? key.startsWith(pattern.slice(0, -1))
      : pattern === key;
  for (const entry of settingsLayout[tab] ?? []) {
    if (typeof entry === "string") {
      for (const d of fields)
        if (!used.has(d.key) && matches(entry, d.key)) {
          used.add(d.key);
          blocks.push({ type: "field", key: d.key, definition: d });
        }
    } else if ("group" in entry)
      blocks.push({
        type: "group",
        key: "group:" + entry.group,
        title: entry.group,
      });
    else
      blocks.push({
        type: "embed",
        key: "embed:" + entry.embed,
        embed: entry.embed,
      });
  }
  for (const d of fields)
    if (!used.has(d.key))
      blocks.push({ type: "field", key: d.key, definition: d });
  // A heading is shown only when something follows it before the next one.
  return blocks.filter(
    (b, i) =>
      b.type !== "group" ||
      (!!blocks[i + 1] && blocks[i + 1]!.type !== "group"),
  );
}
/** Settings the original console marks as required. */
export const requiredSettings: ReadonlySet<string> = new Set([
  "sitename",
  "site_name",
  "homepage",
]);
export interface SettingDefinition {
  key: string;
  label: string;
  tab: SettingsTab;
  kind: "text" | "textarea" | "email" | "select" | "integer" | "money" | "html";
  storage?: "cents" | "bps";
  options?: { value: string; label: string }[];
  hint?: string;
}
const option = (value: string, label: string) => ({ value, label });
const booleanOptions = [option("true", "开启"), option("false", "关闭")];
const switchOptions = [option("1", "开启"), option("0", "关闭")];
export function settingInitialValue(d: SettingDefinition): string {
  return [
    "test_open",
    "test_pay_uid",
    "captcha_open_test",
    "captcha_version",
    "captcha_open_login",
  ].includes(d.key)
    ? "0"
    : "";
}
const labels: Record<
  string,
  [
    SettingsTab,
    string,
    SettingDefinition["kind"],
    SettingDefinition["storage"]?,
  ]
> = {
  site_name: ["site", "网站名称", "text"],
  sitename: ["site", "网站名称", "text"],
  title: ["site", "首页标题", "text"],
  keywords: ["site", "关键字", "text"],
  description: ["site", "网站描述", "text"],
  orgname: ["site", "公司/组织名称", "text"],
  email: ["site", "联系邮箱", "email"],
  kfqq: ["site", "客服QQ", "text"],
  kfqq_url: ["site", "客服QQ链接", "text"],
  wework_url: ["site", "企业微信链接", "text"],
  qqqun: ["site", "加群链接", "text"],
  login_subtitle: ["site", "登录页副标题", "text"],
  homepage: ["site", "首页显示模式", "select"],
  homepage_url: ["site", "显示网址URL", "text"],
  support_email: ["site", "客服邮箱", "email"],
  modal: ["anounce", "用户中心弹出公告", "html"],
  zhuce: ["anounce", "用户注册页面公告", "html"],
  footer: ["anounce", "底部版权", "html"],
  registration_open: ["merchant", "开放商户注册", "select"],
  registration_invite_required: ["merchant", "注册必须邀请码", "select"],
  user_review: ["merchant", "新商户审核", "select"],
  enforce_domains: ["merchant", "未授权支付域名禁止支付", "select"],
  payment_fee_enabled: ["pay", "支付手续费", "select"],
  test_open: ["site", "测试支付", "select"],
  test_pay_uid: ["site", "测试支付收款商户ID", "integer"],
  captcha_open_test: ["site", "测试支付开启验证码", "select"],
  captcha_id: ["site", "极验滑动验证码ID", "text"],
  captcha_version: ["site", "极验版本", "select"],
  captcha_open_login: ["site", "登录开启验证码", "select"],
  pay_maxmoney: ["pay", "最大支付金额（元）", "money"],
  pay_minmoney: ["pay", "最小支付金额（元）", "money"],
  refund_fee_payer: ["merchant", "退款手续费承担方", "select"],
  wxcombine_open: ["pay", "微信开启合单支付", "select"],
  wxcombine_minmoney: ["pay", "合单支付最小金额（元）", "money"],
  wxcombine_submoney: ["pay", "合单支付子单最大金额（元）", "money"],
  settlement_minimum_cents: ["settle", "最低结算金额（元）", "money", "cents"],
  settlement_maximum_cents: ["settle", "结算申请上限（元）", "money", "cents"],
  settlement_fee_rate_bps: ["settle", "结算手续费率（%）", "money", "bps"],
  settlement_fee_minimum_cents: [
    "settle",
    "最低结算手续费（元）",
    "money",
    "cents",
  ],
  settlement_fee_maximum_cents: [
    "settle",
    "最高结算手续费（元）",
    "money",
    "cents",
  ],
  settlement_retained_cents: [
    "settle",
    "结算后保留余额（元）",
    "money",
    "cents",
  ],
  settlement_daily_limit: ["settle", "每日结算申请次数", "integer"],
  settle_open: ["settle", "结算开关", "select"],
  settle_type: ["settle", "结算周期", "select"],
  transfer_enabled: ["transfer", "转账付款开关", "select"],
  transfer_minmoney: ["transfer", "最低转账金额（元）", "money"],
  transfer_maxmoney: ["transfer", "最高转账金额（元）", "money"],
  transfer_maxlimit: ["transfer", "每日同账户转账次数", "integer"],
  transfer_desc: ["transfer", "默认转账备注", "text"],
  transfer_name: ["transfer", "支付宝转账标题", "text"],
  auto_settle_money: ["settle", "余额自动转账阈值（元）", "money"],
  transfer_alipay: ["transfer", "支付宝自动转账通道ID", "integer"],
  user_transfer: ["transfer", "代付功能（原系统开关）", "select"],
  user_transfer_red: ["transfer", "用户中心创建红包功能", "select"],
  transfer_wxpay_type: ["transfer", "微信V3转账产品类型", "select"],
  transfer_wxpay_scene_id: ["transfer", "微信商家转账-场景ID", "text"],
  transfer_wxpay_info_type: ["transfer", "微信商家转账-报备信息类型", "text"],
  transfer_wxpay_info_content: [
    "transfer",
    "微信商家转账-报备信息内容",
    "text",
  ],
  telegramnotice: ["notice", "Telegram提醒", "select"],
  qqbotnotice: ["notice", "QQ Bot提醒", "select"],
  voicenotice: ["notice", "语音提醒", "select"],
  mail_recv: ["notice", "管理员邮箱", "email"],
  mail_name: ["notice", "管理员邮箱", "email"],
  msgtelegram_chatid: ["notice", "管理员Telegram会话ID", "text"],
  qqbot_openid: ["notice", "管理员QQ机器人OpenID", "text"],
  admin_funds_authorization_enabled: [
    "advanced",
    "管理员资金操作二次验证",
    "select",
  ],
  proxy: ["proxy", "中转代理开关", "select"],
  proxy_server: ["proxy", "代理IP", "text"],
  proxy_port: ["proxy", "代理端口", "integer"],
  proxy_user: ["proxy", "代理账号", "text"],
  proxy_type: ["proxy", "代理协议", "select"],
  proxy_apiurl: ["proxy", "API接口地址", "text"],
  login_qq: ["oauth", "QQ快捷登录", "select"],
  login_qq_appid: ["oauth", "QQ互联 APPID", "text"],
  login_wx: ["oauth", "微信快捷登录", "select"],
  login_alipay: ["oauth", "支付宝快捷登录", "select"],
  login_appid: ["oauth", "第三方登录中转 APPID", "text"],
  login_apiurl: ["oauth", "第三方登录中转API地址", "text"],
  login_tg: ["oauth", "Telegram快捷登录", "select"],
  settle_alipay: ["settle", "支付宝结算开关", "select"],
  settle_wxpay: ["settle", "微信结算开关", "select"],
  settle_qqpay: ["settle", "QQ钱包结算开关", "select"],
  settle_bank: ["settle", "银行卡结算开关", "select"],
  transfer_wxpay: ["transfer", "微信转账接口通道", "select"],
  transfer_qqpay: ["transfer", "QQ钱包转账接口通道", "select"],
  transfer_bank: ["transfer", "银行卡转账接口通道", "select"],
  user_transfer_batch: ["transfer", "用户中心批量代付功能", "select"],
  subchannel_open: ["merchant", "开启子通道管理功能", "select"],
  user_profitsharing: ["merchant", "开启添加分账规则(进件)", "select"],
  user_totp_open: ["merchant", "开启商户TOTP二次验证", "select"],
  verifytype: ["merchant", "用户验证方式", "select"],
  onecode_domains: ["merchant", "聚合收款码可选域名", "textarea"],
  pay_domain_open: ["merchant", "开启授权支付域名添加", "select"],
  blockname: ["pay", "商品屏蔽关键词", "text"],
  blockalert: ["pay", "商品屏蔽显示内容", "text"],
  blackmsg: ["pay", "黑名单拦截提示内容", "text"],
  pageordername: ["pay", "扫码页面隐藏商品名称", "select"],
  notifyordername: ["pay", "回调参数去除商品名称", "select"],
  forceqq: ["pay", "未填联系QQ禁止支付", "select"],
  pay_force_cashier_pids: ["pay", "强制跳转收银台商户ID", "text"],
  internal_transfer: ["merchant", "开启站内转账", "select"],
  recharge: ["merchant", "开启余额充值", "select"],
  onecode: ["merchant", "开启聚合收款码", "select"],
  complain_open: ["complaint", "开启交易投诉", "select"],
  applyments_open: ["merchant", "开启商户进件功能", "select"],
  mchrisk_open: ["merchant", "开启商户管控记录查看", "select"],
  reg_pay: ["merchant", "注册付费", "select"],
  reg_pay_price: ["merchant", "注册付费金额（元）", "money"],
  reg_pay_uid: ["merchant", "注册/充值/购买会员收款商户ID", "integer"],
};
const scenes: Record<string, string> = {
  order: "订单支付成功",
  settle: "结算完成",
  login: "账号登录",
  complain: "交易投诉",
  mchrisk: "渠道商户管控",
  balance: "余额不足",
  group: "会员到期",
  domain_audit: "授权域名审核",
  daily_report: "每日交易统计",
  regaudit: "新商户待审核",
  apply: "手动提现申请",
  domain: "域名与站内转账待审核",
  risk: "管理员异常事件",
  wxtransfer: "转账确认",
};
const templateFields: Record<string, string> = {
  no: "平台订单号",
  name: "商品名称",
  money: "金额",
  time: "时间",
  outno: "商户订单号",
  type: "类型",
  account: "收款账户",
  realmoney: "实际结算金额",
  user: "商户ID",
  ip: "登录IP",
  iploc: "IP归属地",
  order_no: "平台订单号",
  reason: "投诉原因",
  msg: "消息内容",
};
export function settingDefinition(
  key: string,
  serverLabel = key,
): SettingDefinition {
  const entry = labels[key];
  const d: SettingDefinition = entry
    ? { key, tab: entry[0], label: entry[1], kind: entry[2], storage: entry[3] }
    : { key, tab: "advanced", label: serverLabel, kind: "text" };
  if (
    [
      "registration_open",
      "registration_invite_required",
      "enforce_domains",
      "payment_fee_enabled",
      "transfer_enabled",
      "admin_funds_authorization_enabled",
    ].includes(key)
  )
    d.options = booleanOptions;
  if (key === "user_review")
    d.options = [option("0", "无需审核"), option("1", "需管理员审核")];
  if (key === "homepage")
    d.options = [
      option("0", "默认显示首页"),
      option("1", "直接跳转登录页面"),
      option("2", "显示其它指定网址"),
      option("3", "显示空白页"),
    ];
  if (key === "settle_type")
    d.options = [option("0", "T+0"), option("1", "T+1")];
  if (key === "settle_open") {
    d.options = [
      option("0", "关闭结算"),
      option("1", "只开启每日自动结算"),
      option("2", "只开启手动申请结算"),
      option("3", "开启自动+手动结算"),
    ];
    d.hint =
      "用户组可覆盖此设置。余额阈值自动转账按独立规则执行，仅手动结算组不参与。";
  }
  if (key === "refund_fee_payer")
    d.options = [
      option("platform", "平台承担（退手续费）"),
      option("merchant", "商户承担（保留手续费）"),
    ];
  if (key === "verifytype")
    d.options = [option("0", "邮箱验证"), option("1", "手机验证")];
  if (
    [
      "user_transfer",
      "telegramnotice",
      "qqbotnotice",
      "voicenotice",
      "test_open",
      "captcha_open_test",
      "captcha_open_login",
      "wxcombine_open",
      "user_transfer_red",
      "settle_alipay",
      "settle_wxpay",
      "settle_qqpay",
      "settle_bank",
      "transfer_wxpay",
      "transfer_qqpay",
      "transfer_bank",
      "user_transfer_batch",
      "subchannel_open",
      "user_profitsharing",
      "user_totp_open",
      "pay_domain_open",
      "pageordername",
      "notifyordername",
      "forceqq",
      "internal_transfer",
      "recharge",
      "onecode",
      "complain_open",
      "applyments_open",
      "mchrisk_open",
      "reg_pay",
      "login_qq",
      "login_wx",
      "login_alipay",
      "login_tg",
    ].includes(key)
  )
    d.options = switchOptions;
  if (key === "wxcombine_open")
    d.hint =
      "仅微信官方V3直连通道支持；开启后达到最小金额的订单拆分为3-50个子单合单支付。";
  if (key === "wxcombine_minmoney")
    d.hint = "订单金额达到此值时使用合单支付；0或不填则不使用合单。";
  if (key === "wxcombine_submoney")
    d.hint = "子单金额超过此值时增加子单数，最多50个子单。";
  if (key === "captcha_version")
    d.options = [option("0", "V3.0"), option("1", "V4.0")];
  if (key === "proxy") {
    d.options = [
      option("0", "关闭"),
      option("1", "代理服务器"),
      option("2", "代理API"),
    ];
    d.hint =
      "开启后，支付成功异步回调时使用中转代理访问商户网站，可解决仅限国内访问的回调，并避免本站服务器IP泄露。代理密码、API接口密钥在下方单独加密保存。";
  }
  if (key === "proxy_type")
    d.options = [
      option("http", "HTTP"),
      option("https", "HTTPS"),
      option("sock4", "SOCK4"),
      option("sock5", "SOCK5"),
      option("sock5h", "SOCK5H"),
    ];
  if (key === "proxy_apiurl") d.hint = "填写代理API站点URL，支持子目录。";
  if (key === "login_qq")
    d.hint = "开启后登录页显示QQ快捷登录；需配置QQ互联APPID与下方APPKEY。";
  if (key === "login_wx")
    d.hint = "开启后登录页显示微信快捷登录；需配置中转APPID与下方AppSecret。";
  if (key === "login_alipay")
    d.hint = "开启后登录页显示支付宝快捷登录；需配置中转APPID与下方应用密钥。";
  if (key === "login_tg")
    d.hint = "开启后登录页显示Telegram快捷登录；需配置下方Bot Token。";
  if (key === "login_qq_appid") d.hint = "QQ互联应用的 APP ID。";
  if (key === "login_appid")
    d.hint = "第三方登录中转服务的 APP ID，微信与支付宝共用。";
  if (key === "login_apiurl")
    d.hint = "第三方登录中转服务的API地址，用于微信/支付宝授权换取身份。";
  if (key === "transfer_wxpay_type") {
    d.options = [
      option("0", "商家转账到零钱（旧）"),
      option("1", "商家转账（新）"),
    ];
    d.hint =
      "微信官方V3直连通道的转账产品。本系统V3通道仅支持商家转账（新），收款方需在微信内扫码确认收款；选择旧版时V3通道不提供微信转账。";
  }
  if (key === "transfer_wxpay_scene_id")
    d.hint = "可前往“微信商户平台-产品中心-商家转账”中申请。如：1001-现金营销";
  if (key === "transfer_wxpay_info_type")
    d.hint =
      "请根据产品文档确认当前转账场景下需传入的信息类型，需按要求填入，有多个类型时用|隔开";
  if (key === "transfer_wxpay_info_content")
    d.hint =
      "请根据信息类型，描述当前这笔转账单的转账背景，有多个类型时用|隔开";
  if (
    ["settle_alipay", "settle_wxpay", "settle_qqpay", "settle_bank"].includes(
      key,
    )
  )
    d.hint = "关闭后该收款方式的余额不参与结算；需配合结算总开关使用。";
  if (["transfer_wxpay", "transfer_qqpay"].includes(key))
    d.hint = "关闭后不使用该通道发起转账付款。";
  if (key === "blockname")
    d.hint = "多个关键词用|隔开。命中关键词的商品会被拒绝支付并记入风控记录。";
  if (key === "blockalert")
    d.hint = "命中屏蔽关键词时的提示内容；留空则显示“该商品禁止出售”。";
  if (key === "blackmsg")
    d.hint = "命中黑名单时的提示内容；留空则显示“系统异常无法完成付款”。";
  if (key === "pageordername")
    d.hint =
      "开启后扫码收银页面隐藏真实商品名称，显示onlinepay或自定义商品名称。";
  if (key === "notifyordername")
    d.hint =
      "开启后异步/同步回调参数中的name固定为product，签名按该值计算，验签不受影响。";
  if (key === "forceqq") d.hint = "开启后未填写联系QQ的商户无法收款。";
  if (key === "pay_force_cashier_pids")
    d.hint = "填写需强制跳转收银台的商户ID，多个用|隔开。";
  if (key === "internal_transfer")
    d.hint = "关闭后商户中心隐藏站内转账入口并拒绝相关请求。";
  if (key === "recharge")
    d.hint = "关闭后商户中心隐藏余额充值入口并拒绝充值请求。";
  if (key === "onecode")
    d.hint =
      "关闭后商户中心隐藏聚合收款入口并拒绝相关请求（单独开通聚合码的商户不受影响）。";
  if (key === "complain_open")
    d.hint = "关闭后商户中心隐藏交易投诉入口并拒绝相关请求。";
  if (key === "applyments_open")
    d.hint = "进件/实名相关业务按本轮范围暂缓，此开关当前仅保存配置。";
  if (key === "mchrisk_open")
    d.hint = "进件管控记录按本轮范围暂缓，此开关当前仅保存配置。";
  if (key === "subchannel_open")
    d.hint = "关闭后商户中心隐藏支付通道（子通道）入口并拒绝相关请求。";
  if (key === "user_profitsharing")
    d.hint = "关闭后商户不能新增分账规则或分账接收方；已有规则仍可查看和停用。";
  if (key === "user_totp_open")
    d.hint =
      "开启后，商户可在账户设置中绑定TOTP，登录时进行二次验证。关闭后不能新绑定；已绑定的商户登录仍需验证，也可自行解绑。";
  if (key === "verifytype")
    d.hint =
      "用于商户注册绑定邮箱/手机。选手机验证时需短信接口可用；未配置时两种方式都可用。";
  if (key === "onecode_domains")
    d.hint =
      "一行一个域名，例如 https://pay.example.com，最多20个。商户生成聚合收款码时可选；域名需解析到本站。";
  if (key === "pay_domain_open")
    d.hint = "关闭后商户不能新增授权域名；已添加的域名不受影响。";
  if (key === "enforce_domains")
    d.hint =
      "开启后，订单的异步/同步通知地址须在商户已审核通过的授权域名内，否则拒绝下单。";
  if (key === "user_transfer_batch")
    d.hint = "关闭后商户中心隐藏批量付款入口并拒绝相关请求。";
  if (key === "transfer_bank")
    d.hint = "银行卡转账通道开关；需对应渠道支持银行卡代付才生效。";
  if (key === "reg_pay") d.hint = "开启后新商户注册需先支付注册费用。";
  if (key === "reg_pay_price") d.hint = "注册付费金额（元）。";
  if (key === "reg_pay_uid")
    d.hint = "注册/充值/购买会员订单的收款商户ID，需为本站已注册的商户UID。";
  if (key.startsWith("msgconfig_")) {
    const scene = key.slice(10);
    d.tab = "notice";
    d.kind = "select";
    d.label = (scenes[scene] || scene) + "提醒";
    d.options = ["regaudit", "apply", "domain"].includes(scene)
      ? [
          option("0", "关闭提醒"),
          option("1", "电子邮件"),
          option("2", "群机器人"),
          option("4", "Telegram"),
          option("5", "QQ Bot"),
        ]
      : scene === "risk"
        ? [
            option("0", "电子邮件"),
            option("1", "群机器人"),
            option("2", "Telegram"),
            option("3", "QQ Bot"),
          ]
        : switchOptions;
  }
  const template = key.match(/^wxnotice_tpl_([a-z]+)(?:_(.+))?$/);
  if (template) {
    d.tab = "notice";
    d.label = `微信${scenes[template[1]!] || template[1]} · ${template[2] ? `${templateFields[template[2]] || template[2]}字段` : "模板ID"}`;
    d.hint = template[2]
      ? "填写已配置模板对应的字段名。"
      : "填写实际已配置的微信消息模板ID。";
  }
  if (key === "site_name")
    d.hint = "支付页面等功能使用的网站名称；原消息模板可使用下方原网站名称。";
  if (key === "sitename")
    d.hint = "用于网站首页、登录页面、后台品牌和消息提醒。";
  if (key === "user_transfer")
    d.hint =
      "普通代付兼容原系统开关；配置转账付款开关时优先使用该项。用户组开启可覆盖普通业务开关，商户自身权限和执行开关仍需满足。";
  if (["pay_maxmoney", "pay_minmoney"].includes(key))
    d.hint = "0表示不限制；用户组可覆盖，修改后仅约束新订单。";
  if (key === "admin_funds_authorization_enabled")
    d.hint = "开启后相关资金操作需要独立资金授权；高风险动作始终保持强制验证。";
  if (
    [
      "settlement_maximum_cents",
      "settlement_fee_maximum_cents",
      "settlement_daily_limit",
      "transfer_maxlimit",
    ].includes(key)
  )
    d.hint = "0表示不限制。";
  if (["auto_settle_money", "transfer_alipay"].includes(key))
    d.hint = "0表示关闭该自动转账条件。";
  if (key === "test_pay_uid")
    d.hint =
      "0关闭测试支付。仅使用此固定商户及当前启用通道，测试订单仍执行原支付费率和权限校验。";
  return d;
}
const formatMinor = (n: bigint) =>
  `${n / 100n}.${String(n % 100n).padStart(2, "0")}`;
export function settingDisplay(d: SettingDefinition, value: string): string {
  if (!d.storage) return value;
  if (!/^(0|[1-9]\d*)$/.test(value))
    throw Error(`${d.label}的已存值不是规范整数，请核对数据`);
  return formatMinor(BigInt(value));
}
export function settingStorage(d: SettingDefinition, input: string): string {
  const value = input.trim();
  if (d.key === "captcha_id") {
    if (!/^[A-Za-z0-9_-]{0,128}$/.test(value))
      throw Error("极验滑动验证码ID格式无效");
    return value;
  }
  if (
    !value &&
    (siteKeys191 as readonly string[]).includes(d.key) &&
    !["sitename", "homepage"].includes(d.key)
  )
    return "";
  if (d.kind === "html" && ["modal", "zhuce", "footer"].includes(d.key)) {
    if (new TextEncoder().encode(value).length > 20000)
      throw Error(`${d.label}：内容不能超过20000字节`);
    return value;
  }
  if (!value)
    throw Error(`${d.label}：请填写明确的设置值，未配置的项目无需保存`);
  if (d.kind === "select" && !d.options?.some((o) => o.value === value))
    throw Error(`${d.label}：请选择有效选项`);
  if (d.key === "onecode_domains") {
    const lines = value
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean);
    const loopback = ["127.0.0.1", "localhost", "[::1]"];
    const origin = (line: string) => {
      try {
        const u = new URL(line);
        return (
          (u.protocol === "https:" ||
            (u.protocol === "http:" && loopback.includes(u.hostname))) &&
          !u.username &&
          !u.password &&
          (u.pathname === "/" || u.pathname === "") &&
          !u.search &&
          !u.hash &&
          !/^[a-z]+:\/\/[^/]*@/i.test(line)
        );
      } catch {
        return false;
      }
    };
    if (
      new TextEncoder().encode(value).length > 2048 ||
      lines.length > 20 ||
      !lines.every(origin)
    )
      throw Error(
        `${d.label}：一行一个 https 域名，例如 https://pay.example.com，最多20个`,
      );
    return lines.join("\n");
  }
  if (d.key === "transfer_wxpay_scene_id" && !/^[0-9]{1,8}$/.test(value))
    throw Error(`${d.label}：请填写数字场景ID，如1001`);
  if (
    d.key === "proxy_port" &&
    (!/^[1-9]\d*$/.test(value) || Number(value) > 65535)
  )
    throw Error("代理端口：请输入1-65535的端口号");
  if (
    ["proxy_server", "proxy_user"].includes(d.key) &&
    (value.length > 255 || /[\u0000-\u001f\u007f ]/.test(value))
  )
    throw Error(`${d.label}：请勿包含空格或控制字符`);
  if (d.key === "proxy_apiurl") {
    let u: URL | null = null;
    try {
      u = new URL(value);
    } catch {
      u = null;
    }
    if (!u || !["http:", "https:"].includes(u.protocol) || value.length > 2048)
      throw Error("API接口地址：请填写http(s)开头的完整站点URL");
  }
  if (
    ["transfer_wxpay_info_type", "transfer_wxpay_info_content"].includes(d.key)
  ) {
    const limit = d.key === "transfer_wxpay_info_type" ? 15 : 32,
      parts = value.split("|");
    if (
      parts.length > 8 ||
      parts.some(
        (v) => !v || [...v].length > limit || /[\u0000-\u001f]/.test(v),
      )
    )
      throw Error(`${d.label}：每项1-${limit}个字，最多8项，用|隔开`);
  }
  if (
    d.kind === "integer" &&
    (!/^(0|[1-9]\d*)$/.test(value) || BigInt(value) > 9223372036854775807n)
  )
    throw Error(`${d.label}：请输入非负整数`);
  if (d.kind === "money") {
    if (!/^(0|[1-9]\d*)(\.\d{1,2})?$/.test(value))
      throw Error(`${d.label}：请输入非负金额，最多两位小数`);
    const [whole, fraction = ""] = value.split("."),
      n = BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0"));
    const max =
      d.storage === "bps"
        ? 10000n
        : d.storage === "cents"
          ? 100000000000n
          : 9223372036854775807n;
    if (n > max) throw Error(`${d.label}：数值超过允许上限`);
    return d.storage ? n.toString() : formatMinor(n);
  }
  if (value.length > 1000) throw Error(`${d.label}：设置值过长`);
  return value;
}
