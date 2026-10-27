import { orderActualAmount104 } from "./order-charge104";
export type OrderActionID =
  | "mark-paid"
  | "mark-unpaid"
  | "api-refund"
  | "manual-refund"
  | "freeze"
  | "unfreeze"
  | "notify"
  | "fill"
  | "delete"
  | "restore";
export interface OrderAction {
  id: OrderActionID;
  label: string;
  separatorBefore?: boolean;
}
export type MerchantOrderActionID =
  | "detail"
  | "ledger"
  | "query"
  | "notify"
  | "refund"
  | "print"
  | "refund-history";
export function merchantOrderActions(
  row: Record<string, unknown>,
): { id: MerchantOrderActionID; label: string; href?: string }[] {
  const trade = row.trade_no;
  if (typeof trade !== "string" || !/^[A-Za-z0-9_-]{1,64}$/.test(trade))
    return [];
  const caps = (
    row.merchant_actions && typeof row.merchant_actions === "object"
      ? row.merchant_actions
      : {}
  ) as Record<string, unknown>;
  const out: { id: MerchantOrderActionID; label: string; href?: string }[] = [
    { id: "detail", label: "查看详情" },
    {
      id: "ledger",
      label: "资金明细",
      href: "/merchant/record?trade_no=" + encodeURIComponent(trade),
    },
  ];
  if (row.status === "pending" && caps.query === true)
    out.push({ id: "query", label: "手动补单" });
  const money = (v: unknown) =>
    typeof v === "string" &&
    /^(0|[1-9]\d{0,18})$/.test(v) &&
    BigInt(v) <= 9223372036854775807n
      ? BigInt(v)
      : undefined;
  const amount = orderActualAmount104(row),
    refunded = money(row.refunded_amount);
  if (row.status === "paid") {
    if (caps.notify === true) out.push({ id: "notify", label: "重新通知" });
    if (
      caps.refund === true &&
      amount !== undefined &&
      amount > 0n &&
      refunded !== undefined &&
      refunded < amount
    )
      out.push({ id: "refund", label: "退款" });
    if (caps.print === true) out.push({ id: "print", label: "打印小票" });
    if (refunded !== undefined && refunded > 0n)
      out.push({
        id: "refund-history",
        label: "退款记录",
        href: "/merchant/trade/refund?trade_no=" + encodeURIComponent(trade),
      });
  }
  return out;
}
const labels: Record<OrderActionID, string> = {
  "mark-paid": "改已完成",
  "mark-unpaid": "改未完成",
  "api-refund": "API退款",
  "manual-refund": "手动退款",
  freeze: "冻结订单",
  unfreeze: "解冻订单",
  notify: "重新通知",
  fill: "手动补单",
  delete: "删除订单",
  restore: "恢复订单",
};
export function adminOrderActions(row: Record<string, unknown>): OrderAction[] {
  const item = (id: OrderActionID, separatorBefore = false): OrderAction => ({
    id,
    label: labels[id],
    separatorBefore,
  });
  if (row.admin_archived === true) return [item("restore")];
  const status = String(row.admin_status || row.status || "pending");
  if (status !== "paid" && status !== "1" && status !== "2" && status !== "3")
    return [item("mark-paid"), item("fill", true), item("delete")];
  if (row.income_status === "held" || status === "3")
    return [
      item("unfreeze"),
      item("api-refund"),
      item("notify", true),
      item("delete"),
    ];
  let refunded = status === "2";
  const actual104 = orderActualAmount104(row);
  if (actual104 !== undefined && /^\d+$/.test(String(row.refunded_amount)))
    refunded ||=
      actual104 > 0n && BigInt(String(row.refunded_amount)) >= actual104;
  if (refunded)
    return [
      item("mark-unpaid"),
      item("api-refund"),
      item("mark-paid"),
      item("notify", true),
      item("delete"),
    ];
  return [
    item("mark-unpaid"),
    item("api-refund"),
    item("manual-refund"),
    item("freeze"),
    item("notify", true),
    item("delete"),
  ];
}
