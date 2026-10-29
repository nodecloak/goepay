import type { Area } from "./menu";
import { readLinkedMerchantRoute } from "./list-filters";
import { validDateRange } from "./reports";
export const transferSearchFields = [
  { value: "", label: "商户交易号（包含）" },
  { value: "biz_no", label: "交易号" },
  { value: "out_biz_no", label: "商户交易号" },
  { value: "pay_order_no", label: "接口交易号" },
  { value: "account", label: "付款账号" },
  { value: "username", label: "姓名" },
  { value: "money", label: "付款金额" },
  { value: "desc", label: "备注" },
] as const;
export const transferStatusFields = [
  { value: "", label: "全部状态" },
  { value: "pending", label: "正在处理" },
  { value: "succeeded", label: "转账成功" },
  { value: "failed", label: "转账失败" },
  { value: "unsent", label: "待处理" },
  { value: "unknown", label: "结果待核对" },
  { value: "cancelled", label: "已取消" },
  { value: "reversed", label: "已退回" },
  { value: "reversal_pending", label: "退回处理中" },
] as const;
// 付款方式 filter — the rails GOEPAY supports. 记账本调拨 is Pro-only alipayfund.
export const transferTypeFields = [
  { value: "", label: "全部方式" },
  { value: "alipay", label: "支付宝" },
  { value: "wxpay", label: "微信" },
  { value: "qqpay", label: "QQ钱包" },
  { value: "bank", label: "银行卡" },
] as const;
export const freshTransferFilters = () => ({
  merchant_id: "",
  search_field: "biz_no",
  q: "",
  status: "",
  type: "",
  channel_id: "",
  from: "",
  to: "",
});
export type TransferReadFilters = ReturnType<typeof freshTransferFilters>;
const positiveID = (value: string) =>
  /^[1-9]\d{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n;
function moneySearch(value: string) {
  if (!/^(0|[1-9]\d{0,16})(\.\d{1,2})?$/.test(value)) return false;
  const [whole, fraction = ""] = value.split(".");
  return (
    BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0")) <=
    9223372036854775807n
  );
}
export function transferFiltersError(value: TransferReadFilters) {
  for (const key of Object.keys(
    freshTransferFilters(),
  ) as (keyof TransferReadFilters)[])
    if (typeof value[key] !== "string" || /[\x00\r\n]/.test(value[key]))
      return "付款筛选格式无效";
  if (!transferSearchFields.some((f) => f.value === value.search_field))
    return "请选择已支持的付款搜索字段";
  if (new TextEncoder().encode(value.q).length > 256)
    return "搜索内容不能超过256字节";
  if (value.q && value.search_field === "money" && !moneySearch(value.q))
    return "付款金额请输入有效的元金额，最多两位小数";
  if (
    (value.merchant_id && !positiveID(value.merchant_id)) ||
    (value.channel_id && !positiveID(value.channel_id))
  )
    return "商户号与通道ID须为有效正整数";
  if (!transferStatusFields.some((f) => f.value === value.status))
    return "付款状态无效";
  if (!transferTypeFields.some((f) => f.value === value.type))
    return "付款方式无效";
  if (
    !validDateRange(value.from, value.to) ||
    [value.from, value.to].some((v) => v.startsWith("0000-"))
  )
    return "日期范围无效，起止日期最多相隔366天";
  return "";
}
export function transferFiltersQuery(
  value: TransferReadFilters,
  page: number,
  limit: number,
) {
  if (
    transferFiltersError(value) ||
    !Number.isSafeInteger(page) ||
    page < 1 ||
    page > 100000 ||
    !Number.isSafeInteger(limit) ||
    limit < 1 ||
    limit > 100
  )
    throw Error("付款筛选参数无效");
  const q = new URLSearchParams({
    page: String(page),
    limit: String(limit),
    q: value.q,
  });
  for (const key of [
    "merchant_id",
    "search_field",
    "status",
    "type",
    "channel_id",
    "from",
    "to",
  ] as const)
    if (value[key]) q.set(key, value[key]);
  return q;
}
export function readTransferRoute(
  raw: Record<string, unknown>,
  area: Area,
  selfID: string,
) {
  const filters = freshTransferFilters();
  let page = 1,
    limit = 20;
  const result = (error = "") => ({
    filters,
    page,
    limit,
    merchantID: filters.merchant_id,
    search: filters.q,
    error,
  });
  const fail = () => result("付款关联记录筛选参数无效，请清空后重新查询");
  const allowed = [
      "uid",
      "merchant_id",
      "page",
      "limit",
      "q",
      "value",
      "column",
      "search_field",
      "channel_id",
      "status",
      "type",
      "from",
      "to",
    ],
    values: Record<string, string> = {};
  for (const [key, value] of Object.entries(raw)) {
    if (
      !allowed.includes(key) ||
      typeof value !== "string" ||
      /[\x00\r\n]/.test(value)
    )
      return fail();
    values[key] = value;
  }
  if (
    values.column !== undefined &&
    values.search_field !== undefined &&
    values.column !== values.search_field
  )
    return fail();
  if (
    values.q !== undefined &&
    values.value !== undefined &&
    values.q !== values.value
  )
    return fail();
  const common: Record<string, string> = {};
  for (const key of ["uid", "merchant_id", "page", "limit"])
    if (values[key] !== undefined) common[key] = values[key]!;
  common.q = values.q ?? values.value ?? "";
  const base = readLinkedMerchantRoute(common, area, selfID, true);
  if (base.error) return fail();
  page = base.page;
  limit = base.limit;
  filters.merchant_id = base.merchantID;
  filters.q = base.search;
  filters.search_field =
    values.search_field ??
    values.column ??
    (values.q !== undefined ? "" : "biz_no");
  for (const key of ["status", "type", "channel_id", "from", "to"] as const)
    filters[key] = values[key] ?? "";
  if (transferFiltersError(filters)) return fail();
  return result();
}
