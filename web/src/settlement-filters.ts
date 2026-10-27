import type { Area } from "./menu";
import { validDateRange } from "./reports";
export const freshSettlementFilters = () => ({
  value: "",
  merchant_id: "",
  type: "",
  dstatus: "",
  from: "",
  to: "",
});
export type SettlementFilters = ReturnType<typeof freshSettlementFilters>;
export const settlementStates = [
  ["0", "待结算"],
  ["1", "已完成"],
  ["2", "正在结算"],
  ["3", "结算失败"],
  ["4", "已取消"],
  ["5", "退回处理中"],
  ["6", "已退回"],
] as const;
const id = (v: string) =>
  /^[1-9]\d{0,18}$/.test(v) && BigInt(v) <= 9223372036854775807n;
export function settlementFiltersError(f: SettlementFilters) {
  if (
    typeof f.value !== "string" ||
    /[\x00\r\n]/.test(f.value) ||
    new TextEncoder().encode(f.value).length > 256
  )
    return "搜索内容格式无效，最多256字节";
  if (f.merchant_id && !id(f.merchant_id)) return "请输入有效的商户号";
  if (f.type && !["alipay", "wxpay", "qqpay", "bank"].includes(f.type))
    return "结算方式无效";
  if (f.dstatus && !settlementStates.some(([v]) => v === f.dstatus))
    return "结算状态无效";
  if (
    !!f.from !== !!f.to ||
    !validDateRange(f.from, f.to) ||
    (f.from && f.from < "1970-01-01") ||
    f.to > "9998-12-31"
  )
    return "请选择有效的完整日期范围，最多366天";
  return "";
}
export function readSettlementRoute(
  raw: Record<string, unknown>,
  area: Area,
  self: string,
) {
  const filters = freshSettlementFilters(),
    out = { filters, page: 1, limit: 20, error: "" };
  const fail = () => ({
    ...out,
    error: "关联记录筛选参数无效，请重置后重新查询",
  });
  const q: Record<string, string> = {};
  for (const [k, v] of Object.entries(raw)) {
    if (
      ![
        "uid",
        "merchant_id",
        "type",
        "dstatus",
        "from",
        "to",
        "starttime",
        "endtime",
        "page",
        "limit",
        "value",
      ].includes(k) ||
      typeof v !== "string" ||
      /[\x00\r\n]/.test(v) ||
      (v === "" && k !== "value")
    )
      return fail();
    q[k] = v;
  }
  for (const [a, b] of [
    ["uid", "merchant_id"],
    ["from", "starttime"],
    ["to", "endtime"],
  ])
    if (Object.hasOwn(q, a) && Object.hasOwn(q, b)) return fail();
  filters.merchant_id = q.merchant_id ?? q.uid ?? "";
  filters.value = q.value ?? "";
  filters.type =
    (
      { "1": "alipay", "2": "wxpay", "3": "qqpay", "4": "bank" } as Record<
        string,
        string
      >
    )[q.type] ??
    q.type ??
    "";
  filters.dstatus = q.dstatus ?? "";
  filters.from = q.from ?? q.starttime ?? "";
  filters.to = q.to ?? q.endtime ?? "";
  if (
    settlementFiltersError(filters) ||
    (area === "merchant" &&
      filters.merchant_id &&
      (!id(self) || filters.merchant_id !== self))
  )
    return fail();
  for (const [k, max] of [
    ["page", 100000],
    ["limit", 100],
  ] as const) {
    if (q[k] !== undefined) {
      if (!/^[1-9]\d*$/.test(q[k]) || Number(q[k]) > max) return fail();
      out[k] = Number(q[k]);
    }
  }
  return out;
}
export function settlementFilterQuery(
  f: SettlementFilters,
  area: Area,
  page?: number,
  limit?: number,
) {
  const error = settlementFiltersError(f);
  if (error) throw Error(error);
  const q = new URLSearchParams();
  if (area === "admin" && f.merchant_id) q.set("merchant_id", f.merchant_id);
  for (const k of ["type", "dstatus", "from", "to", "value"] as const)
    if (f[k] !== "") q.set(k, f[k]);
  if (page !== undefined) q.set("page", String(page));
  if (limit !== undefined) q.set("limit", String(limit));
  return q;
}
export function readSettlementStatistics(raw: unknown) {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("结算统计格式无效");
  const r = raw as Record<string, unknown>;
  const keys = [
    "total_count",
    "waiting_count",
    "completed_count",
    "processing_count",
    "failed_count",
    "cancelled_count",
    "returning_count",
    "returned_count",
  ] as const;
  const counts: Record<string, string> = {};
  for (const k of keys) {
    const v = r[k];
    if (typeof v !== "string" || !/^(0|[1-9]\d{0,37})$/.test(v))
      throw Error("结算数量格式无效");
    counts[k] = v;
  }
  if (
    keys.slice(1).reduce((n, k) => n + BigInt(counts[k]!), 0n) !==
    BigInt(counts.total_count!)
  )
    throw Error("结算统计数量不一致");
  if (
    typeof r.total_money !== "string" ||
    !/^(0|[1-9]\d{0,35})\.\d{2}$/.test(r.total_money)
  )
    throw Error("结算金额格式无效");
  return {
    ...(counts as Record<(typeof keys)[number], string>),
    total_money: r.total_money,
  };
}
