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
export interface SettingDefinition {
  key: string;
  label: string;
  tab: SettingsTab;
  kind: "text" | "email" | "select" | "integer" | "money" | "html";
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
  title: ["site", "网站标题", "text"],
  keywords: ["site", "网站关键词", "text"],
  description: ["site", "网站描述", "text"],
  orgname: ["site", "公司名称", "text"],
  email: ["site", "联系邮箱", "email"],
  kfqq: ["site", "客服QQ", "text"],
  kfqq_url: ["site", "客服QQ链接", "text"],
  wework_url: ["site", "企业微信链接", "text"],
  qqqun: ["site", "加群链接", "text"],
  login_subtitle: ["site", "登录页面标语", "text"],
  homepage: ["site", "网站首页", "select"],
  homepage_url: ["site", "自定义首页地址", "text"],
  support_email: ["site", "客服邮箱", "email"],
  modal: ["anounce", "用户中心弹出公告", "html"],
  zhuce: ["anounce", "用户注册页面公告", "html"],
  footer: ["anounce", "底部版权", "html"],
  registration_open: ["merchant", "开放商户注册", "select"],
  registration_invite_required: ["merchant", "注册必须邀请码", "select"],
  user_review: ["merchant", "新商户审核", "select"],
  enforce_domains: ["pay", "校验授权域名", "select"],
  payment_fee_enabled: ["pay", "支付手续费", "select"],
  test_open: ["site", "测试支付", "select"],
  test_pay_uid: ["site", "测试支付收款商户ID", "integer"],
  captcha_open_test: ["site", "测试支付开启验证码", "select"],
  captcha_id: ["site", "极验滑动验证码ID", "text"],
  captcha_version: ["site", "极验版本", "select"],
  captcha_open_login: ["site", "登录开启验证码", "select"],
  pay_maxmoney: ["pay", "最大支付金额（元）", "money"],
  pay_minmoney: ["pay", "最小支付金额（元）", "money"],
  refund_fee_payer: ["pay", "退款手续费承担方", "select"],
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
  auto_settle_money: ["transfer", "余额自动转账阈值（元）", "money"],
  transfer_alipay: ["transfer", "支付宝自动转账通道ID", "integer"],
  user_transfer: ["transfer", "代付功能（原系统开关）", "select"],
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
      option("0", "默认首页"),
      option("1", "商户登录页面"),
      option("2", "自定义首页"),
      option("3", "空白页面"),
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
  if (
    [
      "user_transfer",
      "telegramnotice",
      "qqbotnotice",
      "voicenotice",
      "test_open",
      "captcha_open_test",
      "captcha_open_login",
    ].includes(key)
  )
    d.options = switchOptions;
  if (key === "captcha_version")
    d.options = [option("0", "V3.0"), option("1", "V4.0")];
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
