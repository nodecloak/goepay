import type { Area } from "./menu";
export const reportPages: Record<Area, Record<string, string>> = {
  admin: {
    "/trade/stat/daily": "daily",
    "/trade/stat/user": "merchants",
    "/trade/stat/channel": "channels",
    "/trade/stat/province": "provinces",
    "/trade/stat/buyer": "buyers",
    "/transfer/stat": "transfer-accounts",
    "/transfer/channel-stat": "transfer-channels",
    "/transfer/user-stat": "transfer-merchants",
    "/complain/charts": "complaints",
  },
  merchant: {
    "/trade/daily-stat": "daily",
    "/trade/province-stat": "provinces",
  },
};
export const reportFor = (area: Area, path: string) => reportPages[area][path];
function validDate(value: string) {
  if (value === "") return true;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const d = new Date(value + "T00:00:00Z");
  return !Number.isNaN(d.valueOf()) && d.toISOString().slice(0, 10) === value;
}
export const validDateRange = (from: string, to: string) =>
  validDate(from) &&
  validDate(to) &&
  (!from ||
    !to ||
    (from <= to && Date.parse(to) - Date.parse(from) <= 366 * 86400000));
export interface ReportColumn {
  key: string;
  label: string;
}
export function reportDateRange(
  preset: string,
  now = new Date(),
): { from: string; to: string } | null {
  const days = (
    { today: 0, yesterday: 0, last7: 6, last30: 29 } as Record<string, number>
  )[preset];
  if (days === undefined) return null;
  const beijingDay = new Date(now.getTime() + 8 * 3600000)
    .toISOString()
    .slice(0, 10);
  const end = Date.parse(beijingDay) + (preset === "yesterday" ? -86400000 : 0);
  return {
    from: new Date(end - days * 86400000).toISOString().slice(0, 10),
    to: new Date(end).toISOString().slice(0, 10),
  };
}
export function reportColumns(
  report: string,
  columns: ReportColumn[],
): ReportColumn[] {
  const order =
    report === "transfer-accounts"
      ? ["dimension", "name", "orders", "paid_money"]
      : report === "transfer-channels"
        ? ["dimension", "paid_money", "fee_money", "orders", "paid_orders"]
        : [
            "dimension",
            "paid_money",
            "net_money",
            "fee_money",
            "orders",
            "paid_orders",
          ];
  const labels: Record<string, string> =
    report === "transfer-accounts"
      ? {
          dimension: "付款账号",
          name: "姓名",
          orders: "付款笔数",
          paid_money: "付款金额",
        }
      : report === "transfer-channels"
        ? {
            dimension: "付款通道",
            paid_money: "付款金额",
            orders: "付款总数",
            paid_orders: "成功数量",
          }
        : {};
  // Transfer channel fee_money is the platform fee, not profit after upstream
  // costs. Preserve its server label instead of borrowing Pro's profit label.
  const aligned = order.flatMap((key) =>
    columns
      .filter((c) => c.key === key)
      .map((c) => ({ ...c, label: labels[c.key] || c.label })),
  );
  const rest = columns.filter((c) => !order.includes(c.key));
  if (
    report !== "complaints" &&
    columns.some((c) => c.key === "orders") &&
    columns.some((c) => c.key === "paid_orders")
  )
    rest.push({
      key: "success_rate",
      label: report.startsWith("transfer-") ? "成功率" : "支付成功率",
    });
  return [...aligned, ...rest];
}
export function reportRows(
  items: Record<string, unknown>[],
): Record<string, unknown>[] {
  return items.map((row) => {
    const total = String(row.orders ?? ""),
      paid = String(row.paid_orders ?? "");
    let rate = "—";
    const safe = [row.orders, row.paid_orders].every(
      (v) =>
        typeof v === "string" ||
        (typeof v === "number" && Number.isSafeInteger(v)),
    );
    if (
      safe &&
      /^(0|[1-9]\d*)$/.test(total) &&
      /^(0|[1-9]\d*)$/.test(paid) &&
      BigInt(total) > 0n &&
      BigInt(paid) <= BigInt(total)
    ) {
      const scaled =
        (BigInt(paid) * 10000n + BigInt(total) / 2n) / BigInt(total);
      rate = `${scaled / 100n}.${String(scaled % 100n).padStart(2, "0")}%`;
    }
    return { ...row, success_rate: rate };
  });
}
export const csvExport = (
  columns: ReportColumn[],
  items: Record<string, unknown>[],
) => {
  const quote = (value: unknown) => {
    let s = value === null || value === undefined ? "" : String(value);
    if (/^[\s]*[=+\-@\t\r]/.test(s)) s = "'" + s;
    return '"' + s.replaceAll('"', '""') + '"';
  };
  return (
    "\uFEFF" +
    [
      columns.map((c) => quote(c.label)).join(","),
      ...items.map((row) => columns.map((c) => quote(row[c.key])).join(",")),
    ].join("\r\n")
  );
};
