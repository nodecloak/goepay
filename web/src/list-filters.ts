import type { Area } from "./menu";
import { validDateRange } from "./reports";
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
export const refundSearchFields = [
  { value: "refund_no", label: "退款单号" },
  { value: "out_refund_no", label: "商户退款单号" },
  { value: "trade_no", label: "关联订单号" },
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

// URL state is a typed read context. An invalid original link must never fall
// back to an unfiltered platform query or supply a different merchant identity.
export function readListRouteQuery(
  raw: Record<string, unknown>,
  kind: string,
  area: Area,
  selfID: string,
) {
  const filters = freshListFilters();
  let search = "",
    page = 1,
    limit = 20,
    includeArchived = false;
  const result = (error = "") => ({
    filters,
    search,
    page,
    limit,
    includeArchived,
    error,
  });
  const fail = () => result("列表筛选参数无效，请清空后重新查询");
  const allowed = new Set([
    "q",
    "value",
    "column",
    "uid",
    "page",
    "limit",
    ...Object.keys(filters),
  ]);
  if (kind === "orders")
    for (const key of ["include_archived", "trade_no", "out_trade_no"])
      allowed.add(key);
  if (kind === "refunds")
    for (const key of ["refund_no", "out_refund_no", "trade_no"])
      allowed.add(key);
  const values: Record<string, string> = {};
  for (const [key, v] of Object.entries(raw)) {
    if (!allowed.has(key) || typeof v !== "string" || /[\x00\r\n]/.test(v))
      return fail();
    values[key] = v;
  }
  for (const key of Object.keys(filters) as (keyof ListFilters)[])
    filters[key] = values[key] || "";
  const id = (v: string) =>
    /^[1-9]\d{0,18}$/.test(v) && BigInt(v) <= 9223372036854775807n;
  if (values.uid !== undefined) {
    if (
      !id(values.uid) ||
      (filters.merchant_id && filters.merchant_id !== values.uid)
    )
      return fail();
    filters.merchant_id = values.uid;
  }
  if (
    (filters.merchant_id && !id(filters.merchant_id)) ||
    (filters.channel_id && !id(filters.channel_id))
  )
    return fail();
  if (
    area === "merchant" &&
    filters.merchant_id &&
    filters.merchant_id !== selfID
  )
    return result("商户后台只能查看本人记录，请清空筛选");
  if (
    values.q !== undefined &&
    values.value !== undefined &&
    values.q !== values.value
  )
    return fail();
  search = values.value ?? values.q ?? "";
  if (values.column !== undefined) {
    if (filters.search_field && filters.search_field !== values.column)
      return fail();
    filters.search_field = values.column;
  }
  for (const field of kind === "refunds"
    ? ["refund_no", "out_refund_no", "trade_no"]
    : ["trade_no", "out_trade_no"]) {
    if (values[field] !== undefined) {
      if (
        !values[field] ||
        (search && search !== values[field]) ||
        (filters.search_field && filters.search_field !== field)
      )
        return fail();
      search = values[field];
      filters.search_field = field;
    }
  }
  if (
    new TextEncoder().encode(search).length > 256 ||
    new TextEncoder().encode(filters.buyer).length > 256
  )
    return fail();
  if (
    filters.search_field &&
    !(kind === "refunds" ? refundSearchFields : orderSearchFields).some(
      (v) => v.value === filters.search_field,
    )
  )
    return fail();
  const statuses =
    kind === "orders"
      ? [
          "created",
          "submitting",
          "pending",
          "paid",
          "closed",
          "failed",
          "unknown",
        ]
      : kind === "refunds"
        ? ["pending", "unknown", "succeeded", "failed"]
        : [];
  if (filters.status && !statuses.includes(filters.status)) return fail();
  if (
    filters.type &&
    (!["orders", "refunds"].includes(kind) ||
      !["alipay", "wxpay", "sandbox"].includes(filters.type))
  )
    return fail();
  if (filters.channel_id && !["orders", "refunds"].includes(kind))
    return fail();
  if (
    kind !== "orders" &&
    [
      filters.buyer,
      filters.client_ip,
      filters.income_status,
      filters.notification_state,
    ].some(Boolean)
  )
    return fail();
  if (
    kind !== "orders" &&
    kind !== "refunds" &&
    [filters.search_field, values.column, values.value].some(Boolean)
  )
    return fail();
  if (
    kind === "refunds" &&
    filters.search_field === "refund_no" &&
    search &&
    !id(search)
  )
    return fail();
  if (
    filters.income_status &&
    !["none", "held", "released"].includes(filters.income_status)
  )
    return fail();
  if (
    filters.notification_state &&
    !["none", "pending", "running", "succeeded", "dead"].includes(
      filters.notification_state,
    )
  )
    return fail();
  if (
    filters.client_ip.length > 45 ||
    filters.from.startsWith("0000-") ||
    filters.to.startsWith("0000-") ||
    !validDateRange(filters.from, filters.to)
  )
    return fail();
  for (const [key, max] of [
    ["page", 100000],
    ["limit", 100],
  ] as const) {
    if (values[key] !== undefined) {
      if (!/^[1-9]\d*$/.test(values[key]) || Number(values[key]) > max)
        return fail();
      if (key === "page") page = Number(values[key]);
      else limit = Number(values[key]);
    }
  }
  if (values.include_archived !== undefined) {
    if (
      area !== "admin" ||
      !["true", "false"].includes(values.include_archived)
    )
      return fail();
    includeArchived = values.include_archived === "true";
  }
  return result();
}
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
  if (
    kind === "refunds" &&
    refundSearchFields.some((f) => f.value === filters.search_field)
  )
    keys.push("search_field");
  for (const key of keys) {
    const value = filters[key].trim();
    if (value) query.set(key, value);
  }
}
