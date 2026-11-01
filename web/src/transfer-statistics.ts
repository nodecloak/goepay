export interface TransferStatistics {
  count: string;
  amount: string;
  fee_amount: string;
  cost_amount: string;
  succeeded_count: string;
  failed_count: string;
  pending_count: string;
}
export function readTransferStatistics(raw: unknown): TransferStatistics {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("付款统计响应无效，请重新查询");
  const value = raw as TransferStatistics;
  for (const key of [
    "count",
    "amount",
    "fee_amount",
    "cost_amount",
    "succeeded_count",
    "failed_count",
    "pending_count",
  ] as const)
    if (
      typeof value[key] !== "string" ||
      !/^(0|[1-9]\d{0,39})$/.test(value[key])
    )
      throw Error("付款统计响应无效，请重新查询");
  if (
    BigInt(value.amount) + BigInt(value.fee_amount) !==
      BigInt(value.cost_amount) ||
    BigInt(value.succeeded_count) +
      BigInt(value.failed_count) +
      BigInt(value.pending_count) >
      BigInt(value.count)
  )
    throw Error("付款统计金额或状态无法核对，请重新查询");
  return { ...value };
}
export function transferStatisticsMoney(value: string): string {
  const amount = BigInt(value);
  return `${amount / 100n}.${String(amount % 100n).padStart(2, "0")}`;
}
