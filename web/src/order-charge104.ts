const max = 9223372036854775807n;
export function orderCents104(value: unknown): bigint | undefined {
  if (typeof value !== "string" || !/^(0|[1-9][0-9]{0,18})$/.test(value))
    return;
  const n = BigInt(value);
  return n <= max ? n : undefined;
}
// Older console fixtures have no dual-amount fields. Explicit new-source
// uncertainty never downgrades to requested M, even if the values are equal.
export function orderActualAmount104(
  row: Record<string, unknown>,
): bigint | undefined {
  const original = orderCents104(row.amount);
  if (row.charge_verified104 === false) return;
  if (
    Object.hasOwn(row, "realmoney") ||
    Object.hasOwn(row, "charge_verified104")
  ) {
    const actual = orderCents104(row.realmoney);
    return row.charge_verified104 === true &&
      actual !== undefined &&
      actual > 0n &&
      original !== undefined &&
      actual >= original
      ? actual
      : undefined;
  }
  return original;
}
export function orderRefundRemaining104(
  row: Record<string, unknown>,
): bigint | undefined {
  const actual = orderActualAmount104(row),
    used = orderCents104(row.refunded_amount ?? "0");
  return actual !== undefined && used !== undefined && used <= actual
    ? actual - used
    : undefined;
}
