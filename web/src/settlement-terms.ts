import { validAmount } from "./api";
export interface SettlementPreview {
  fee_cents: string;
  net_cents: string;
  fee_money: string;
  net_money: string;
  retained_money: string;
}
export function centsMoney(raw: unknown): string {
  if (
    typeof raw !== "string" ||
    !/^(0|[1-9]\d{0,18})$/.test(raw) ||
    BigInt(raw) > 9223372036854775807n
  )
    throw new Error("服务端结算金额格式无效，请重新核对");
  const n = BigInt(raw);
  return `${n / 100n}.${String(n % 100n).padStart(2, "0")}`;
}
export function readSettlementPreview(
  raw: unknown,
  amount: string,
): SettlementPreview {
  if (!raw || typeof raw !== "object" || !validAmount(amount))
    throw new Error("结算报价无效");
  const q = raw as Record<string, unknown>,
    t = q.terms as Record<string, unknown> | null;
  if (q.external_payout !== false || q.can_apply !== true || !t)
    throw new Error(
      "当前日次数、留存余额或资金条件不允许该结算申请，请重新查询",
    );
  for (const key of [
    "gross_amount",
    "fee_amount",
    "net_amount",
    "retained_cents",
  ])
    centsMoney(t[key]);
  const [whole, fraction = ""] = amount.split(".");
  const expected = BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0"));
  const gross = BigInt(t.gross_amount as string),
    fee = BigInt(t.fee_amount as string),
    net = BigInt(t.net_amount as string);
  if (gross !== expected || net <= 0n || fee + net !== gross)
    throw new Error("服务端结算报价与申请金额不一致，请重新核对");
  return {
    fee_cents: t.fee_amount as string,
    net_cents: t.net_amount as string,
    fee_money: centsMoney(t.fee_amount),
    net_money: centsMoney(t.net_amount),
    retained_money: centsMoney(t.retained_cents),
  };
}
