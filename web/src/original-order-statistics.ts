import { validDateRange } from "./reports";
export interface OriginalStatisticsFilters {
  type: string;
  channel: string;
  uid: string;
  from: string;
  to: string;
}
export function originalStatisticsQuery(
  report: string,
  f: OriginalStatisticsFilters,
): { query?: URLSearchParams; error?: string } {
  const bad = (error: string) => ({ error });
  if (
    !["daily", "channels"].includes(report) ||
    !["", "alipay", "wxpay", "sandbox"].includes(f.type)
  )
    return bad("请选择有效支付方式");
  const id = (s: string) =>
    s === "" ||
    (/^[1-9]\d{0,18}$/.test(s) && BigInt(s) <= 9223372036854775807n);
  if (!id(f.uid) || (report === "daily" && !id(f.channel)))
    return bad("商户号和通道ID须为有效正整数");
  const q = new URLSearchParams({ view: "original" });
  if (f.type) q.set("type", f.type);
  if (f.uid) q.set("uid", f.uid);
  if (report === "daily" && f.channel) q.set("channel", f.channel);
  if (report === "channels") {
    if (!f.from || !f.to || !validDateRange(f.from, f.to))
      return bad("请选择有效查询日期，范围最多367天");
    q.set("from", f.from);
    q.set("to", f.to);
  }
  return { query: q };
}
function cents(value: unknown): bigint | null {
  if (typeof value !== "string" || !/^-?(0|[1-9]\d{0,37})\.\d{2}$/.test(value))
    return null;
  const negative = value.startsWith("-"),
    [whole, part] = value.replace(/^-/, "").split(".");
  const n = BigInt(whole) * 100n + BigInt(part);
  return negative ? -n : n;
}
export function statisticsMoney(value: unknown): string {
  return cents(value) === null ? "未知" : String(value);
}
export function statisticsTrend(rows: Record<string, unknown>[]) {
  const definitions = [
    { key: "realmoney", name: "支付金额", color: "#1677ff" },
    { key: "refundmoney", name: "退款金额", color: "#e6a23c" },
    { key: "profitmoney", name: "手续费利润", color: "#18a058" },
  ];
  let low = 0n,
    high = 1n;
  for (const row of rows)
    for (const d of definitions) {
      const n = cents(row[d.key]);
      if (n !== null) {
        if (n < low) low = n;
        if (n > high) high = n;
      }
    }
  return definitions.map((d) => {
    const points: { x: number; y: number; day: string; value: string }[] = [],
      segments: string[] = [];
    let segment: string[] = [];
    const flush = () => {
      if (segment.length) segments.push(segment.join(" "));
      segment = [];
    };
    rows.forEach((row, index) => {
      const n = cents(row[d.key]);
      if (n === null) {
        flush();
        return;
      }
      const p = {
        x: 48 + (index * 544) / Math.max(1, rows.length - 1),
        y: 220 - Number(((n - low) * 20000n) / (high - low)) / 100,
        day: String(row.day),
        value: String(row[d.key]),
      };
      points.push(p);
      segment.push(p.x + "," + p.y);
    });
    flush();
    return { ...d, points, segments };
  });
}
