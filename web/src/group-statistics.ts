import {
  readPaymentStatisticsDay,
  type PaymentStatisticsRow,
  type PaymentStatisticsDay,
} from "./payment-statistics";
export type GroupStatisticsRow = PaymentStatisticsRow;
export type GroupStatisticsDay = PaymentStatisticsDay;
export type GroupStatistics = {
  group_id: string;
  group_name: string;
  timezone: string;
  cohort: string;
  days: Record<"today" | "yesterday", GroupStatisticsDay>;
};
const invalid = (): never => {
  throw Error("用户组统计响应格式无效，请刷新核对");
};
export function readGroupStatistics(
  value: unknown,
  id: string,
): GroupStatistics {
  const d = value as any;
  if (
    !d ||
    d.group_id !== id ||
    d.timezone !== "Asia/Shanghai" ||
    d.cohort !== "current_effective_group" ||
    typeof d.group_name !== "string" ||
    d.group_name.length > 100 ||
    /[\x00-\x1f\x7f]/.test(d.group_name) ||
    !d.days
  )
    return invalid();
  for (const key of ["today", "yesterday"]) {
    try {
      readPaymentStatisticsDay(d.days[key]);
    } catch {
      return invalid();
    }
  }

  return d as GroupStatistics;
}
