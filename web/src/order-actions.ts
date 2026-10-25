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
  if (
    /^\d+$/.test(String(row.amount)) &&
    /^\d+$/.test(String(row.refunded_amount))
  )
    refunded ||=
      BigInt(String(row.amount)) > 0n &&
      BigInt(String(row.refunded_amount)) >= BigInt(String(row.amount));
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
