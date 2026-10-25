export interface FundsAvailability {
  available_money: string;
  ledger_available_money: string;
  today_blocked_money: string;
  settle_type: 0 | 1;
}
export function readFundsAvailability(value: unknown): FundsAvailability {
  const invalid = () => new Error("服务端资金政策或余额信息不完整，请重新查询");
  if (!value || typeof value !== "object") throw invalid();
  const data = value as Record<string, unknown>;
  if (data.settle_type !== 0 && data.settle_type !== 1) throw invalid();
  const money = (key: string) => {
    const v = data[key];
    if (typeof v !== "string" || !/^(0|[1-9]\d{0,16})\.\d{2}$/.test(v))
      throw invalid();
    const cents = BigInt(v.replace(".", ""));
    if (cents > 9223372036854775807n) throw invalid();
    return { text: v, cents };
  };
  const available = money("available_money"),
    ledger = money("ledger_available_money"),
    blocked = money("today_blocked_money");
  if (
    available.cents + blocked.cents !== ledger.cents ||
    (data.settle_type === 0 && blocked.cents !== 0n)
  )
    throw invalid();
  return {
    available_money: available.text,
    ledger_available_money: ledger.text,
    today_blocked_money: blocked.text,
    settle_type: data.settle_type,
  };
}
