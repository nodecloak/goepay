import { validAmount } from "./api";
import { centsMoney } from "./settlement-terms";

export function readHeldRefundFunding(
  raw: unknown,
  amount: string,
): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || !validAmount(amount))
    throw new Error("退款资金来源响应无效，请重新核对");
  const view = raw as Record<string, unknown>;
  if (
    view.funding_mode !== "income_hold" ||
    typeof view.hold_id !== "string" ||
    !/^[1-9]\d*$/.test(view.hold_id) ||
    BigInt(view.hold_id) > 9223372036854775807n
  )
    throw new Error("退款原冻结批次无效，请重新读取订单");
  for (const key of [
    "held_amount",
    "available_amount",
    "fee_amount",
    "held_remaining",
  ])
    centsMoney(view[key]);
  const [whole, fraction = ""] = amount.split(".");
  const requested = BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0"));
  const held = BigInt(view.held_amount as string),
    available = BigInt(view.available_amount as string),
    fee = BigInt(view.fee_amount as string);
  if (
    held + available + fee !== requested ||
    held > BigInt(view.held_remaining as string)
  )
    throw new Error("退款金额与原资金来源不一致，请重新核对");
  return { ...view };
}
