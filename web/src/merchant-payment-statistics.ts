import {
  readPaymentStatisticsDay,
  type PaymentStatisticsDay,
} from "./payment-statistics";
import { FILTER_PAY_TYPES, payTypeLabel } from "./pay-types";

export type MerchantPaymentStatistics = {
  merchant_id: string;
  timezone: "Asia/Shanghai";
  cohort: "merchant_orders_created";
  days: Record<"today" | "yesterday", PaymentStatisticsDay>;
  trend: PaymentStatisticsDay[];
};
const invalid = (): never => {
  throw Error("商户统计响应格式无效，请刷新核对");
};
export function merchantStatisticsID(id: unknown): id is string {
  return (
    typeof id === "string" &&
    /^[1-9]\d{0,18}$/.test(id) &&
    BigInt(id) <= 9223372036854775807n
  );
}
function comparable(day: PaymentStatisticsDay) {
  const row = (r: PaymentStatisticsDay["summary"]) => [
    r.type,
    r.showname,
    r.money,
    r.count,
    r.success_count,
    r.rate,
  ];
  return JSON.stringify([
    day.date,
    day.items.map(row).sort((a, b) => a[0]!.localeCompare(b[0]!)),
    row(day.summary),
  ]);
}
export function readMerchantPaymentStatistics(
  value: unknown,
  id: string,
): MerchantPaymentStatistics {
  const d = value as MerchantPaymentStatistics;
  if (
    !merchantStatisticsID(id) ||
    !d ||
    d.merchant_id !== id ||
    d.timezone !== "Asia/Shanghai" ||
    d.cohort !== "merchant_orders_created" ||
    !d.days ||
    !Array.isArray(d.trend) ||
    d.trend.length !== 7
  )
    return invalid();
  try {
    const trend = d.trend.map(readPaymentStatisticsDay);
    for (let i = 1; i < trend.length; i++)
      if (
        Date.parse(trend[i]!.date) - Date.parse(trend[i - 1]!.date) !==
        86400000
      )
        return invalid();
    const today = readPaymentStatisticsDay(d.days.today),
      yesterday = readPaymentStatisticsDay(d.days.yesterday);
    if (
      comparable(today) !== comparable(trend[6]!) ||
      comparable(yesterday) !== comparable(trend[5]!)
    )
      return invalid();
    return {
      merchant_id: id,
      timezone: "Asia/Shanghai",
      cohort: "merchant_orders_created",
      days: { today, yesterday },
      trend,
    };
  } catch {
    return invalid();
  }
}

// Integer pixel coordinates are presentation only. The exact amount string is
// retained at every point for focus/hover labels and the financial table.
export function paymentTrendSeries(days: PaymentStatisticsDay[]) {
  const present = new Set(days.flatMap((d) => d.items.map((r) => r.type)));
  const methods = FILTER_PAY_TYPES.filter((t) => present.has(t));
  const minor = (money: string) => BigInt(money.replace(".", ""));
  const maximum = days
    .flatMap((d) => d.items)
    .reduce((m, r) => (minor(r.money) > m ? minor(r.money) : m), 1n);
  return methods.map((type) => ({
    type,
    name: payTypeLabel(type),
    points: days.map((d, i) => {
      const money = d.items.find((r) => r.type === type)!.money;
      return {
        x: 50 + i * 100,
        y: Number(250n - (minor(money) * 220n) / maximum),
        date: d.date,
        money,
      };
    }),
  }));
}
