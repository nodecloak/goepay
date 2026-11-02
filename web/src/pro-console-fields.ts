import { displayValue } from "./api";
import { orderActualAmount104 } from "./order-charge104";
import type { Area } from "./menu";
import type { ResourceField } from "./resources";
import { readSettlementRecipient } from "./settlement-recipient";
import { beijingTimestamp, isConsoleTimestamp } from "./console-datetime";

// Guide-named aliases for the kit helpers (see S\review\kit-guide.md): money()
// formats integer minor units to yuan; datetime() formats an RFC3339 instant to
// Beijing time. Same functions the console already uses — no behaviour change.
export { beijingTimestamp as datetime } from "./console-datetime";

export interface ConsoleColumn {
  key: string;
  label: string;
}

// These fields are integer minor units in the existing console API. Report
// *_money fields and schema-driven `money` inputs already use yuan strings.
const minorFields = new Set([
  "amount",
  "realmoney",
  "remaining_refund_amount",
  "amount_minor",
  "merchant_amount",
  "platform_fee",
  "fee_amount",
  "refunded_amount",
  "cost_amount",
  "available_minor",
  "frozen_minor",
  "income_held_minor",
  "balance_before",
  "balance_after",
  "bank_fee_amount",
  "bank_service_fee_held",
  "bank_returned_principal",
  "bank_returned_service_fee",
  "bank_returned_fee",
]);
export function minorYuan(value: unknown): string {
  if (typeof value === "number" && !Number.isSafeInteger(value)) return "—";
  if (
    typeof value !== "string" &&
    typeof value !== "number" &&
    typeof value !== "bigint"
  )
    return "—";
  const raw = String(value);
  if (!/^-?(0|[1-9]\d*)$/.test(raw)) return "—";
  const n = BigInt(raw),
    absolute = n < 0n ? -n : n;
  return `${n < 0n ? "-" : ""}${absolute / 100n}.${String(absolute % 100n).padStart(2, "0")}`;
}
// Kit money() (S\review\kit-guide.md): yuan with thousands separators, e.g.
// "1,232.00". Built on minorYuan so values/rounding are identical; only the
// integer part is grouped for display. minorYuan itself stays ungrouped (raw).
export function money(value: unknown): string {
  return groupYuan(minorYuan(value));
}
// Thousands separators for an exact decimal string that is already in yuan
// ("1232.00" -> "1,232.00", report money included). Anything that is not a
// plain decimal ("—", "未记录", text) is returned unchanged.
export function groupYuan(text: string): string {
  const m = /^(-?)(\d+)(\.\d+)?$/.exec(text);
  if (!m) return text;
  return m[1] + m[2]!.replace(/\B(?=(\d{3})+(?!\d))/g, ",") + (m[3] ?? "");
}
export function isCredentialField(key: string): boolean {
  return /(?:^|_)(?:password|api_key|appsecret|secret|private_key|access_token|refresh_token|authorization_token|token)(?:$|_)/i.test(
    key,
  );
}
// Kit header wording: short nouns. Drop the unit/timezone suffixes the Pro
// columns carried — （元）/（分）from money headers and （北京时间）from
// timestamps — since DataTable prints ¥ before money and dates are Beijing
// time by definition. Values stay exact (grouped yuan, UTC+8 timestamps).
export function consoleColumnLabel(key: string, label: string): string {
  return label.replace(
    /（元）|\(元\)|（分）|\(分\)|（北京时间）|\(北京时间\)/g,
    "",
  );
}
const statuses: Record<string, string> = {
  created: "已创建",
  submitting: "提交中",
  processing: "处理中",
  pending: "待处理",
  paid: "已支付",
  succeeded: "成功",
  failed: "明确失败",
  unknown: "结果待核对",
  closed: "已关闭",
  none: "未冻结",
  held: "收入冻结中",
  released: "收入已解冻",
  queued: "排队中",
  running: "执行中",
  completed: "已完成",
  dead: "需人工处理",
  ready: "已就绪",
  revealed: "已查看",
  cancelled: "已取消",
  reversed: "已退回",
  reversal_pending: "退回处理中",
};
export function consoleValue(
  key: string,
  value: unknown,
  orders = false,
): string {
  if (isCredentialField(key)) return "—";
  if (isConsoleTimestamp(key)) return beijingTimestamp(value);
  if (minorFields.has(key)) return minorYuan(value);
  if (key === "type")
    return (
      (
        {
          alipay: "支付宝",
          wxpay: "微信支付",
          qqpay: "QQ钱包",
          bank: "银联支付",
          jdpay: "京东支付",
          paypal: "PayPal",
          douyinpay: "抖音支付",
          ecny: "数字人民币",
          sandbox: "本地模拟",
        } as Record<string, string>
      )[String(value)] ?? displayValue(value)
    );
  if (key === "fee_policy")
    return (
      (
        {
          none: "不收取手续费",
          merchant_deduct: "从商户收入扣除",
          buyer_additive: "付款方承担",
        } as Record<string, string>
      )[String(value)] ?? displayValue(value)
    );
  if (key === "refund_fee_payer")
    return (
      (
        { platform: "平台承担", merchant: "商户承担" } as Record<string, string>
      )[String(value)] ?? displayValue(value)
    );
  if (key === "enabled")
    return value === true ? "正常" : value === false ? "已禁用" : "—";
  if (key === "notification_state")
    return (
      (
        {
          none: "未创建通知",
          pending: "待通知",
          running: "通知中",
          succeeded: "已通知",
          dead: "通知失败",
        } as Record<string, string>
      )[String(value)] ?? displayValue(value)
    );
  if (key === "status" || key.endsWith("_status") || key.endsWith("_state"))
    return orders && value === "pending"
      ? "待支付"
      : (statuses[String(value)] ?? displayValue(value));
  return displayValue(value);
}
// Provider attempt state describes the request, not the order's paid status.
export function orderProviderValue(key: string, value: unknown): string {
  const labels: Record<string, Record<string, string>> = {
    method: {
      page: "网页支付",
      wap: "手机网站支付",
      native: "扫码支付",
      jsapi: "JSAPI 支付",
      app: "应用支付",
      mini: "小程序支付",
      preauth: "预授权支付",
      scan: "付款码支付",
    },
    state: {
      processing: "请求处理中",
      pending: "已发起，等待支付结果",
      unknown: "结果待核对",
      failed: "请求失败",
      succeeded: "请求已完成",
    },
    material_type: {
      html: "网页表单",
      qrcode: "二维码",
      jsapi: "JSAPI 参数",
      app: "应用调起参数",
      preauth: "预授权参数",
      wxapp: "微信小程序参数",
      urlscheme: "应用跳转链接",
      jump: "跳转链接",
      url: "跳转链接",
    },
  };
  return labels[key]?.[String(value)] ?? displayValue(value);
}
const orderColumns: ConsoleColumn[] = [
  { key: "trade_no", label: "系统订单号" },
  { key: "merchant_id", label: "商户号" },
  { key: "name", label: "商品名称" },
  { key: "realmoney", label: "实际支付" },
  { key: "type", label: "支付方式" },
  { key: "created_at", label: "创建时间" },
  { key: "status", label: "支付状态" },
];
const refundColumns: ConsoleColumn[] = [
  { key: "refund_no", label: "退款单号" },
  { key: "trade_no", label: "关联订单号" },
  { key: "merchant_id", label: "商户号" },
  { key: "amount", label: "退款金额（元）" },
  { key: "created_at", label: "申请时间" },
  { key: "status", label: "状态" },
];
const ledgerColumns: ConsoleColumn[] = [
  { key: "id", label: "流水 ID" },
  { key: "merchant_id", label: "商户号" },
  { key: "ledger_type", label: "类型" },
  { key: "description", label: "业务说明" },
  { key: "amount", label: "变更金额（元）" },
  { key: "balance_before", label: "账户变更前（元）" },
  { key: "balance_after", label: "账户变更后（元）" },
  { key: "kind", label: "账务账户" },
  { key: "account_id", label: "账户 ID" },
  { key: "created_at", label: "时间" },
  { key: "event_key", label: "业务请求号" },
];
// App supplies base columns for other capabilities. Do not append arbitrary
// response properties: a new backend key is not permission to expose it.
export function proListColumns(
  kind: string,
  area: Area,
  items: Record<string, unknown>[],
  base: ConsoleColumn[] = [],
): ConsoleColumn[] {
  const planned =
    kind === "orders"
      ? orderColumns
      : kind === "refunds"
        ? refundColumns
        : kind === "ledger"
          ? ledgerColumns
          : base;
  const present = new Set(items.flatMap(Object.keys));
  return planned
    .filter(
      (c) =>
        !isCredentialField(c.key) &&
        (area === "admin" ||
          !["merchant_id", "client_ip", "param"].includes(c.key)) &&
        (!items.length || present.has(c.key)),
    )
    .map((c) => ({ ...c, label: consoleColumnLabel(c.key, c.label) }));
}
export function canRefundListOrder(row: Record<string, unknown>): boolean {
  if (
    !["paid", "partially_refunded"].includes(String(row.status)) ||
    row.income_status === "held"
  )
    return false;
  const actual = orderActualAmount104(row);
  if (
    (Object.hasOwn(row, "charge_verified104") ||
      Object.hasOwn(row, "realmoney")) &&
    actual === undefined
  )
    return false;
  if (
    actual !== undefined &&
    typeof row.refunded_amount === "string" &&
    /^\d+$/.test(row.refunded_amount) &&
    BigInt(row.refunded_amount) >= actual
  )
    return false;
  return true;
}
export function hasOriginalMerchantListFields(
  fields: ResourceField[],
): boolean {
  const names = new Set(
    fields
      .filter(
        (f) =>
          !f.write_only && f.type !== "password" && !isCredentialField(f.name),
      )
      .map((f) => f.name),
  );
  return (
    [
      "name",
      "effective_group_id",
      "effective_group_name",
      "available_minor",
      "settlement_recipient",
      "created_at",
      "pay",
      "enabled",
    ].every((name) => names.has(name)) &&
    ["email", "phone", "qq"].some((name) => names.has(name))
  );
}
export function resourceListFields(
  resource: string,
  fields: ResourceField[],
): ResourceField[] {
  const visible = fields.filter(
    (f) =>
      f.name !== "id" &&
      !f.write_only &&
      f.type !== "password" &&
      !isCredentialField(f.name),
  );
  if (resource === "groups")
    return visible
      .filter((f) => f.name === "name")
      .map((f) => ({ ...f, label: "用户组名称" }));
  // 支付通道轮询: the original console lists five short columns (显示名称 /
  // 支付方式 / 轮询方式 / 轮询规则 / 状态); the dialog keeps every field.
  if (resource === "pay-rolls") {
    const labels: Record<string, string> = {
      name: "显示名称",
      type: "支付方式",
      strategy: "轮询方式",
      channel_ids: "轮询规则",
      enabled: "状态",
    };
    return ["name", "type", "strategy", "channel_ids", "enabled"].flatMap(
      (name) =>
        visible
          .filter((f) => f.name === name)
          .map((f) => ({ ...f, label: labels[name]! })),
    );
  }
  // 公众号小程序: the original console lists 类别 / 名称 / APPID (no 启用 column);
  // the dialog keeps 启用.
  if (resource === "weixin-apps") {
    const labels: Record<string, string> = {
      kind: "类别",
      name: "名称",
      app_id: "APPID",
    };
    return ["kind", "name", "app_id"].flatMap((name) =>
      visible
        .filter((f) => f.name === name)
        .map((f) => ({ ...f, label: labels[name]! })),
    );
  }
  // Channel form definitions belong in the editor/detail, not in each list row.
  if (resource === "onboarding-channels")
    return visible.filter((f) => f.name !== "form_schema");
  if (
    resource === "admin-users" &&
    ["created_at", "last_login_at", "totp_enabled"].every((name) =>
      visible.some((f) => f.name === name),
    )
  ) {
    const labels: Record<string, string> = {
      username: "用户名",
      admin_class: "权限",
      enabled: "状态",
    };
    return [
      "username",
      "admin_class",
      "created_at",
      "last_login_at",
      "totp_enabled",
      "enabled",
    ].flatMap((name) =>
      visible
        .filter((f) => f.name === name)
        .map((f) => ({ ...f, label: labels[name] || f.label })),
    );
  }
  if (
    resource === "risk-records" &&
    visible.some((f) => f.name === "created_at")
  )
    return ["merchant_id", "decision", "reason", "url", "created_at"].flatMap(
      (name) => visible.filter((f) => f.name === name),
    );
  if (resource !== "merchants") return visible;
  const hasEffectiveGroup = visible.some((f) =>
    ["effective_group_id", "effective_group_name"].includes(f.name),
  );
  const contact =
    visible.find((f) => f.name === "email") ||
    visible.find((f) => ["phone", "qq"].includes(f.name));
  if (hasOriginalMerchantListFields(visible)) {
    const labels: Record<string, string> = {
      available_minor: "余额",
      settlement_recipient: "结算账号/姓名",
      created_at: "注册时间/域名",
      pay: "状态",
      [contact!.name]: "联系方式",
    };
    return [
      "available_minor",
      "settlement_recipient",
      contact!.name,
      "created_at",
      "pay",
    ].flatMap((name) =>
      visible
        .filter((f) => f.name === name)
        .map((f) => ({ ...f, label: labels[name]! })),
    );
  }
  const names = [
    "name",
    ...(hasEffectiveGroup ? [] : ["group_id"]),
    "available_minor",
    "settlement_recipient",
    ...(contact ? [contact.name] : []),
    ...(visible.some((f) => f.name === "created_at") ? [] : ["url"]),
    "created_at",
    "pay",
    "enabled",
    "login_username",
    "signature_algorithm",
  ];
  return names.flatMap((name) => visible.filter((f) => f.name === name));
}

export function merchantGroupLabel(row: Record<string, unknown>): string {
  const id = row.effective_group_id;
  if (
    typeof id !== "string" ||
    !/^(0|[1-9]\d{0,18})$/.test(id) ||
    BigInt(id) > 9223372036854775807n
  )
    return "用户组未知";
  const name = row.effective_group_name;
  return typeof name === "string" &&
    !!name &&
    name.length <= 128 &&
    !/[\x00-\x1f\x7f]/.test(name)
    ? `${name}（${id}）`
    : `用户组 ID：${id}`;
}
export function merchantRecipientLabel(raw: unknown): string {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    return "资料不可用";
  if ((raw as Record<string, unknown>).state === "missing") return "未配置";
  if ((raw as Record<string, unknown>).state !== "configured")
    return "资料不可用";
  try {
    const recipient = readSettlementRecipient(raw);
    if (
      ![3, 7].includes(Array.from(recipient.account_masked).length) ||
      /[\x00-\x1f\x7f]/.test(recipient.account_masked)
    )
      return "资料不可用";
    return `${{ alipay: "支付宝", wxpay: "微信支付", bank: "银行卡", "": "" }[recipient.type]} · ${recipient.account_masked} / ${recipient.name_masked}`;
  } catch {
    return "资料不可用";
  }
}
export function approvedMerchantDomains(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  const domains = raw.flatMap((value) => {
    if (
      typeof value !== "string" ||
      !value ||
      value.length > 253 ||
      /[\s\x00-\x1f\x7f/\\?:#@]/.test(value)
    )
      return [];
    try {
      const url = new URL(`https://${value}`);
      if (
        !url.hostname.includes(".") ||
        !/^[a-z0-9.-]+$/i.test(url.hostname) ||
        url.hostname
          .split(".")
          .some(
            (label) => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(label),
          )
      )
        return [];
      return [url.hostname];
    } catch {
      return [];
    }
  });
  return [...new Set(domains)].sort();
}
export function merchantFieldValue(key: string, value: unknown): string {
  if (key === "settle")
    return (
      ({ "0": "关闭", "1": "正常" } as Record<string, string>)[String(value)] ??
      "未识别"
    );
  if (key === "pay")
    return (
      (
        { "0": "禁止收款", "1": "正常", "2": "待审核" } as Record<
          string,
          string
        >
      )[String(value)] ?? "未知"
    );
  if (key === "settlement_recipient") return merchantRecipientLabel(value);
  if (key === "approved_domains")
    return approvedMerchantDomains(value).join("、") || "暂无已审核域名";
  if (key === "group_membership_active")
    return value === true ? "生效中" : value === false ? "未生效" : "未知";
  return consoleValue(key, value);
}

// 资金明细 shows the ledger's own description and account codes. The service
// writes them in English; show the Chinese name, keep unknown text as it is.
const ledgerAccounts: Record<string, string> = {
  available: "可用余额",
  reserved: "冻结预留",
  clearing: "清算账户",
  fees: "平台手续费",
  fee_reserved: "手续费预留",
  deposit: "保证金",
  deposit_reserved: "保证金冻结",
  order_income_hold: "收入冻结",
  bank_fee_expense: "银行手续费支出",
  bank_service_fee_hold: "银行服务费预留",
};
const ledgerDescriptions: Record<string, string> = {
  "payment net credit": "订单收入入账",
  "historical payment net credit": "历史订单收入入账",
  "payment platform fee": "订单平台手续费",
  "refund platform fee": "退款手续费",
  "refund platform fee reserve": "退款手续费预留",
  "legacy refund": "退款",
  "transfer reserve": "付款预留",
  "transfer succeeded": "付款成功",
  "transfer failed": "付款失败退回",
  "transfer platform fee": "付款手续费",
  "deposit release": "保证金释放",
  "original payment fee referral reward": "邀请返现奖励",
  "unfreeze original legacy income": "历史收入解冻",
  "bank transfer principal": "银行转账本金",
  "bank transfer service fee held": "银行转账服务费预留",
  "bank provider fee expense": "银行通道手续费",
  "alipay bank principal": "支付宝转银行卡本金",
  "alipay bank service fee held": "支付宝转银行卡服务费预留",
  "verified alipay bank refund principal": "支付宝转银行卡退回本金",
  "verified alipay bank refund reservation": "支付宝转银行卡退回预留",
  "verified alipay bank refund service fee": "支付宝转银行卡退回服务费",
  "verified bank provider fee return": "银行通道手续费退回",
  "verified bank return original reservation": "银行退回原预留",
  "verified bank return principal": "银行退回本金",
  "verified bank return service fee": "银行退回服务费",
};
const ledgerOutcomes: Record<string, string> = {
  approved: "已通过",
  rejected: "已驳回",
  succeeded: "成功",
  failed: "失败",
};
// 资金明细 original record types (原 pre_record.type). The service projects a
// `ledger_type` code (see internal/server/ledger_reporting_type.go); show the
// original Chinese name the PHP 资金明细 used.
export const ledgerTypeLabels: Record<string, string> = {
  order_income: "订单收入",
  order_fee: "订单服务费",
  order_freeze: "订单冻结",
  order_unfreeze: "订单解冻",
  order_refund: "订单退款",
  refund_fee: "服务费退款",
  transfer: "代付",
  transfer_return: "代付退回",
  settle_auto: "自动结算",
  profit_sharing: "订单分账",
  internal_transfer: "站内转账",
  deposit_in: "充值保证金",
  deposit_out: "提取保证金",
  membership_buy: "购买会员",
  invite_reward: "邀请返现",
  certification: "实名认证",
  admin_credit: "后台加款",
  admin_debit: "后台扣款",
  other: "其他",
};
export function ledgerTerm(key: string, value: unknown): string {
  if (key === "ledger_type")
    return ledgerTypeLabels[String(value)] ?? displayValue(value);
  if (key === "kind")
    return ledgerAccounts[String(value)] ?? consoleValue(key, value);
  if (key !== "description" || typeof value !== "string")
    return consoleValue(key, value);
  const exact = ledgerDescriptions[value];
  if (exact) return exact;
  // "deposit release approved", "legacy refund failed", …
  const cut = value.lastIndexOf(" "),
    base = ledgerDescriptions[value.slice(0, cut)],
    outcome = ledgerOutcomes[value.slice(cut + 1)];
  return cut > 0 && base && outcome ? `${base}（${outcome}）` : value;
}
