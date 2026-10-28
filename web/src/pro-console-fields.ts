import { displayValue } from "./api";
import { orderActualAmount104 } from "./order-charge104";
import type { Area } from "./menu";
import type { ResourceField } from "./resources";
import { readSettlementRecipient } from "./settlement-recipient";
import {
  beijingTimestamp,
  consoleTimeLabel,
  isConsoleTimestamp,
} from "./console-datetime";

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
export function isCredentialField(key: string): boolean {
  return /(?:^|_)(?:password|api_key|appsecret|secret|private_key|access_token|refresh_token|authorization_token|token)(?:$|_)/i.test(
    key,
  );
}
export function consoleColumnLabel(key: string, label: string): string {
  return minorFields.has(key)
    ? label.replace(/（分）|\(分\)/g, "（元）")
    : consoleTimeLabel(key, label);
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
        { alipay: "支付宝", wxpay: "微信支付", sandbox: "本地模拟" } as Record<
          string,
          string
        >
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
const orderColumns: ConsoleColumn[] = [
  { key: "trade_no", label: "系统订单号" },
  { key: "merchant_id", label: "商户号" },
  { key: "name", label: "商品名称" },
  { key: "amount", label: "订单金额（元）" },
  { key: "realmoney", label: "实际支付金额（元）" },
  { key: "type", label: "支付方式" },
  { key: "client_ip", label: "支付 IP" },
  { key: "param", label: "扩展参数" },
  { key: "created_at", label: "创建时间" },
  { key: "status", label: "支付状态" },
  { key: "notification_state", label: "通知状态" },
  { key: "income_status", label: "订单收入状态" },
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
export function ledgerTerm(key: string, value: unknown): string {
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
