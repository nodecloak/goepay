import type { Area } from "./menu";
export const orderSearchFields = [
  { value: "trade_no", label: "系统订单号" },
  { value: "out_trade_no", label: "商户订单号" },
  { value: "api_trade_no", label: "接口订单号" },
  { value: "name", label: "商品名称" },
  { value: "money", label: "订单金额（元）" },
  { value: "realmoney", label: "实付金额（元）" },
  { value: "getmoney", label: "分成金额（元）" },
  { value: "buyer", label: "支付账号" },
  { value: "ip", label: "支付 IP" },
  { value: "param", label: "扩展参数" },
] as const;
export function incomeStatusLabel(value: unknown): string {
  return (
    (
      { none: "未冻结", held: "收入冻结中", released: "收入已解冻" } as Record<
        string,
        string
      >
    )[String(value)] || String(value ?? "—")
  );
}
export const freshListFilters = () => ({
  status: "",
  type: "",
  merchant_id: "",
  channel_id: "",
  from: "",
  to: "",
  buyer: "",
  client_ip: "",
  income_status: "",
  notification_state: "",
  search_field: "",
});
export type ListFilters = ReturnType<typeof freshListFilters>;
export function appendListFilters(
  query: URLSearchParams,
  filters: ListFilters,
  kind: string,
  area: Area,
) {
  const keys: (keyof ListFilters)[] = ["from", "to"];
  if (area === "admin") keys.push("merchant_id");
  if (kind === "orders" || kind === "refunds")
    keys.push("status", "type", "channel_id");
  if (kind === "orders")
    keys.push(
      "buyer",
      "client_ip",
      "income_status",
      "notification_state",
      "search_field",
    );
  for (const key of keys) {
    const value = filters[key].trim();
    if (value) query.set(key, value);
  }
}
