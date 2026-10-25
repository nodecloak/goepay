export type GroupStatisticsRow = {
  type: string;
  showname: string;
  money: string;
  count: string;
  success_count: string;
  rate: string;
};
export type GroupStatisticsDay = {
  date: string;
  items: GroupStatisticsRow[];
  summary: GroupStatisticsRow;
};
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
function count(v: unknown): bigint {
  if (typeof v !== "string" || v.length > 40 || !/^(0|[1-9]\d*)$/.test(v))
    return invalid();
  return BigInt(v);
}
function minor(v: unknown): bigint {
  if (
    typeof v !== "string" ||
    v.length > 80 ||
    !/^(0|[1-9]\d*)\.\d{2}$/.test(v)
  )
    return invalid();
  return BigInt(v.replace(".", ""));
}
function row(v: any): { money: bigint; count: bigint; success: bigint } {
  if (
    !v ||
    typeof v.type !== "string" ||
    typeof v.showname !== "string" ||
    v.showname.length > 100 ||
    /[\x00-\x1f\x7f]/.test(v.showname)
  )
    return invalid();
  const n = count(v.count),
    success = count(v.success_count),
    money = minor(v.money);
  if (success > n) return invalid();
  const r = n === 0n ? 0n : (success * 20000n + n) / (n * 2n),
    expected = `${r / 100n}.${String(r % 100n).padStart(2, "0")}`;
  if (v.rate !== expected) return invalid();
  return { money, count: n, success };
}
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
    const day = d.days[key];
    if (
      !day ||
      typeof day.date !== "string" ||
      !/^\d{4}-\d{2}-\d{2}$/.test(day.date) ||
      !Number.isFinite(Date.parse(day.date + "T00:00:00Z")) ||
      new Date(day.date + "T00:00:00Z").toISOString().slice(0, 10) !==
        day.date ||
      !Array.isArray(day.items) ||
      day.items.length !== 3
    )
      return invalid();
    const types = new Set<string>(),
      sum = { money: 0n, count: 0n, success: 0n };
    for (const item of day.items) {
      const v = row(item);
      if (
        !["alipay", "wxpay", "sandbox"].includes(item.type) ||
        types.has(item.type)
      )
        return invalid();
      types.add(item.type);
      sum.money += v.money;
      sum.count += v.count;
      sum.success += v.success;
    }
    const total = row(day.summary);
    if (
      day.summary.type !== "total" ||
      day.summary.showname !== "总计" ||
      total.money !== sum.money ||
      total.count !== sum.count ||
      total.success !== sum.success
    )
      return invalid();
  }
  return d as GroupStatistics;
}
