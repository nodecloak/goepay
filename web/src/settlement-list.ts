import type { Area } from "./menu";
import { beijingTimestamp } from "./console-datetime";
import { centsMoney } from "./settlement-terms";
import {
  readPayoutDetail,
  payoutFundingLabel,
  payoutStateLabel,
} from "./settlement-payout";

export function originalSettlementColumns(area: Area) {
  return [
    ...(area === "admin"
      ? [
          { key: "id", label: "ID" },
          { key: "merchantID", label: "商户号" },
        ]
      : []),
    { key: "method", label: "结算方式" },
    { key: "account", label: area === "admin" ? "结算账号/姓名" : "结算账号" },
    { key: "money", label: "结算金额/实际到账" },
    { key: "createdAt", label: "创建时间" },
    { key: "completedAt", label: "完成时间" },
    { key: "status", label: "状态" },
  ];
}
export function hasOriginalSettlementProjection(
  items: Record<string, unknown>[],
) {
  return items.every((row) =>
    [
      "amount_cents",
      "settlement",
      "settlement_funding",
      "settlement_recipient",
      "payout",
    ].every((key) => Object.hasOwn(row, key)),
  );
}
function recordedTime(value: unknown): string {
  if (typeof value !== "string" || !value || value.startsWith("0000-"))
    return "未记录";
  const formatted = beijingTimestamp(value);
  if (
    formatted !== value &&
    /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(formatted)
  )
    return formatted;
  // A provider's valid timezone-less local value is kept verbatim. Never infer
  // its offset by passing it through the browser's local timezone parser.
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(value)) {
    const normalized = value.replace(" ", "T"),
      date = new Date(normalized + "Z");
    if (
      Number.isFinite(date.getTime()) &&
      date.toISOString().slice(0, 19) === normalized
    )
      return value;
  }
  return "未记录";
}
export function readSettlementListRow(value: unknown) {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("结算记录格式无效");
  const raw = value as Record<string, unknown>;
  if (typeof raw.id !== "string" || typeof raw.merchant_id !== "string")
    throw Error("结算记录编号格式无效");
  const detail = readPayoutDetail(
    {
      request: {
        id: raw.id,
        merchant_id: raw.merchant_id,
        status: raw.status,
        amount_cents: raw.amount_cents,
        settlement: raw.settlement,
        funding: raw.settlement_funding,
        recipient: raw.settlement_recipient,
      },
      payout: raw.payout,
    },
    raw.id,
  );
  const r = detail.request,
    p = detail.payout;
  if (p) {
    if (
      !r.recipient ||
      (
        [
          "config_id",
          "type",
          "app_id",
          "account_masked",
          "name_masked",
        ] as const
      ).some((key) => r.recipient![key] !== p.recipient[key]) ||
      (p.transfer.type && p.transfer.type !== r.recipient.type)
    )
      throw Error("结算原收款快照不一致，请刷新核对");
    if (
      r.funding &&
      r.funding.state !== "legacy_unscoped" &&
      (r.funding.account_id !== p.funding?.account_id ||
        r.funding.gross_amount !== p.funding?.gross_amount)
    )
      throw Error("结算原资金归属不一致，请刷新核对");
  }
  const paid =
    r.status === "approved" &&
    p?.state === "succeeded" &&
    p.send_state === "claimed" &&
    p.transfer.status === "succeeded" &&
    ["paid", "paid_fee_held"].includes(p.funding?.state || "") &&
    !!r.funding &&
    r.funding.state !== "legacy_unscoped" &&
    (p.funding?.state === "paid_fee_held" ||
      p.funding?.reserved_amount === "0");
  let status = "待结算",
    hint = r.status === "approved" ? "已核准，尚未出款" : "待审核",
    tone = "waiting";
  if (r.status === "rejected") {
    status = "结算失败";
    hint = "申请已拒绝";
    tone = "danger";
  } else if (p) {
    const labels: Record<string, string> = {
      unsent: "待结算",
      pending: "正在结算",
      unknown: "正在结算",
      succeeded: paid ? "已完成" : "正在结算",
      failed: "结算失败",
      cancelled: "已取消",
      reversal_pending: "退回处理中",
      reversed: "已退回",
    };
    status = labels[p.state]!;
    hint = paid
      ? "原出款已确认"
      : p.state === "succeeded"
        ? "出款与资金结果需核对"
        : p.state === "unknown"
          ? "结果待核对"
          : payoutStateLabel(p.state);
    tone = paid
      ? "success"
      : p.state === "failed"
        ? "danger"
        : ["cancelled", "reversed"].includes(p.state)
          ? "neutral"
          : "processing";
  }
  const sourcePayout = raw.payout as {
    transfer?: { pay_date?: unknown };
  } | null;
  const gross = centsMoney(r.amount_cents),
    expected = r.settlement ? centsMoney(r.settlement.net_amount) : "未记录",
    fee = r.settlement ? centsMoney(r.settlement.fee_amount) : "未记录";
  const method =
    (
      { alipay: "支付宝", wxpay: "微信", bank: "银行卡" } as Record<
        string,
        string
      >
    )[r.recipient?.type || ""] || "历史未记录";
  const result = {
    raw,
    id: r.id,
    merchantID: r.merchant_id,
    externalNo:
      typeof raw.external_no === "string" ? raw.external_no : "未记录",
    method,
    account: r.recipient?.account_masked || "历史未记录",
    name: r.recipient?.name_masked || "历史未记录",
    gross,
    expected,
    actual: paid ? centsMoney(p!.transfer.amount) : "—",
    fee,
    status,
    hint,
    tone,
    createdAt: recordedTime(raw.created_at),
    completedAt: paid ? recordedTime(sourcePayout?.transfer?.pay_date) : "—",
    canReview: r.status === "pending" && !p,
    canInspectPayout: r.status === "approved",
    funding: payoutFundingLabel(r.funding),
    payout: p ? payoutStateLabel(p.state) : "尚未出款",
    requestStatus: (
      { pending: "待审核", approved: "已核准", rejected: "已拒绝" } as Record<
        string,
        string
      >
    )[r.status]!,
  };
  return result;
}
export type SettlementListRow = ReturnType<typeof readSettlementListRow>;
