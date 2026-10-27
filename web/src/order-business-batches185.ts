import { orderActualAmount104 } from "./order-charge104";
export const batchBusinessLabels185 = {
  refund: "API退款",
  "freeze-income": "冻结订单",
  "unfreeze-income": "解冻订单",
  notify: "重新通知",
  "confirm-settlement": "确认结算",
  query: "手动补单",
} as const;
export type BusinessBatchAction185 = keyof typeof batchBusinessLabels185;
export const isBusinessBatchAction185 = (
  v: string,
): v is BusinessBatchAction185 => Object.hasOwn(batchBusinessLabels185, v);
const terminal = new Set([
  "succeeded",
  "unpaid",
  "unsupported",
  "failed",
  "queued",
  "cancelled",
]);
const itemStates = new Set(["unsent", "running", "unknown", ...terminal]);
const exactID = (v: unknown) =>
  typeof v === "string" &&
  /^[1-9]\d{0,18}$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
function record(
  v: unknown,
  required: string[],
  optional: string[] = [],
): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw Error("批次响应不完整");
  const r = v as Record<string, unknown>;
  if (
    required.some((k) => !Object.hasOwn(r, k)) ||
    Object.keys(r).some((k) => !required.includes(k) && !optional.includes(k))
  )
    throw Error("批次响应字段无法确认");
  return r;
}
export interface BusinessBatchItem185 {
  position: number;
  order_id: string;
  merchant_id: string;
  trade_no: string;
  state: string;
  code?: string;
  order_status?: string;
}
export interface BusinessBatch185 {
  id: string;
  external_no: string;
  action: BusinessBatchAction185;
  reason: string;
  state: string;
  created_at: string;
  items: BusinessBatchItem185[];
  replayed: boolean;
  fingerprint: string;
  counts: {
    total: number;
    processed: number;
    succeeded: number;
    failed: number;
    unknown: number;
    cancelled: number;
  };
}
export function parseOrderBusinessBatch185(raw: unknown): BusinessBatch185 {
  const v = record(raw, [
    "id",
    "external_no",
    "action",
    "reason",
    "state",
    "created_at",
    "items",
    "replayed",
    "fingerprint",
    "counts",
  ]);
  if (
    !exactID(v.id) ||
    typeof v.external_no !== "string" ||
    v.external_no.length < 8 ||
    v.external_no.length > 96 ||
    typeof v.action !== "string" ||
    !isBusinessBatchAction185(v.action) ||
    v.action === "confirm-settlement" ||
    typeof v.reason !== "string" ||
    !v.reason ||
    typeof v.replayed !== "boolean" ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint) ||
    typeof v.created_at !== "string" ||
    !Number.isFinite(Date.parse(v.created_at)) ||
    !Array.isArray(v.items) ||
    v.items.length < 1 ||
    v.items.length > 100
  )
    throw Error("原批次身份或选集无法确认");
  const seen = new Set<string>();
  const items = v.items.map((raw, i) => {
    const r = record(
      raw,
      ["position", "order_id", "merchant_id", "trade_no", "state"],
      ["code", "order_status"],
    );
    if (
      r.position !== i + 1 ||
      !exactID(r.order_id) ||
      !exactID(r.merchant_id) ||
      typeof r.trade_no !== "string" ||
      !/^[A-Za-z0-9_-]{1,64}$/.test(r.trade_no) ||
      seen.has(r.trade_no) ||
      typeof r.state !== "string" ||
      !itemStates.has(r.state) ||
      (r.code !== undefined && typeof r.code !== "string") ||
      (r.order_status !== undefined && typeof r.order_status !== "string")
    )
      throw Error("原批次订单无法确认");
    seen.add(r.trade_no);
    return r as unknown as BusinessBatchItem185;
  });
  const c = record(v.counts, [
    "total",
    "processed",
    "succeeded",
    "failed",
    "unknown",
    "cancelled",
  ]);
  for (const n of Object.values(c))
    if (!Number.isSafeInteger(n) || Number(n) < 0 || Number(n) > 100)
      throw Error("批次进度无法确认");
  const counts = {
    total: items.length,
    processed: items.filter((x) => terminal.has(x.state)).length,
    succeeded: items.filter(
      (x) => x.state === "succeeded" || x.state === "queued",
    ).length,
    failed: items.filter((x) =>
      ["unpaid", "unsupported", "failed"].includes(x.state),
    ).length,
    unknown: items.filter((x) => x.state === "unknown").length,
    cancelled: items.filter((x) => x.state === "cancelled").length,
  };
  if (
    Object.keys(counts).some((k) => c[k] !== counts[k as keyof typeof counts])
  )
    throw Error("批次进度与逐笔结果不一致");
  if (
    typeof v.state !== "string" ||
    !["prepared", "running", "attention", "completed", "cancelled"].includes(
      v.state,
    ) ||
    (v.state === "prepared" && items.some((x) => x.state !== "unsent")) ||
    (["completed", "cancelled"].includes(v.state) &&
      counts.processed !== counts.total) ||
    (v.state === "attention" && counts.unknown === 0) ||
    (v.state === "cancelled" && counts.cancelled !== counts.total)
  )
    throw Error("批次状态与逐笔结果不一致");
  return { ...v, items, counts } as unknown as BusinessBatch185;
}
export function businessBatchEligible185(
  row: Record<string, unknown>,
  action: BusinessBatchAction185,
): boolean {
  if (
    row.admin_archived === true ||
    (typeof row.trade_no !== "string" && row.trade_no !== undefined)
  )
    return false;
  if (action === "confirm-settlement") return false;
  if (action === "query") return row.status === "pending";
  if (row.status !== "paid") return false;
  if (action === "freeze-income") return row.income_status !== "held";
  if (action === "unfreeze-income") return row.income_status === "held";
  if (action === "notify")
    return (
      row.can_batch_notify === true &&
      ["none", "pending", "running", "dead"].includes(
        String(row.notification_state),
      )
    );
  if (
    orderActualAmount104(row) === undefined ||
    !/^\d+$/.test(String(row.refunded_amount))
  )
    return false;
  return orderActualAmount104(row)! > BigInt(String(row.refunded_amount));
}
export const financialBusinessBatch185 = (action: BusinessBatchAction185) =>
  ["refund", "freeze-income", "unfreeze-income"].includes(action);
