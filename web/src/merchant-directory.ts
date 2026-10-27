export interface MerchantDirectoryDraft {
  column: string;
  dstatus: string;
  order: string;
  from: string;
  to: string;
  gid: string;
  upid: string;
}
export const merchantDirectoryColumns = [
  { value: "", label: "综合搜索" },
  { value: "uid", label: "商户号" },
  { value: "name", label: "名称" },
  { value: "email", label: "邮箱" },
  { value: "login_username", label: "登录账号" },
  { value: "phone", label: "手机号" },
  { value: "qq", label: "QQ" },
  { value: "url", label: "网站域名" },
  { value: "domain", label: "已审核域名" },
];
export const merchantDirectoryStatuses = [
  { value: "", label: "全部用户" },
  { value: "pay_2", label: "待审核用户" },
  { value: "status_1", label: "用户状态正常" },
  { value: "status_0", label: "用户状态封禁" },
  { value: "pay_1", label: "支付状态正常" },
  { value: "pay_0", label: "支付状态关闭" },
  { value: "settle_1", label: "结算状态正常" },
  { value: "settle_0", label: "结算状态关闭" },
];
export const merchantDirectorySorts = [
  { value: "", label: "商户号倒序" },
  { value: "money_desc", label: "余额倒序" },
  { value: "money_asc", label: "余额正序" },
  { value: "lasttime_desc", label: "登录时间倒序" },
  { value: "lasttime_asc", label: "登录时间正序" },
];
export const newMerchantDirectory = (): MerchantDirectoryDraft => ({
  column: "",
  dstatus: "",
  order: "",
  from: "",
  to: "",
  gid: "",
  upid: "",
});
function validID(s: string, zero = false) {
  return (
    (zero ? /^(0|[1-9]\d{0,18})$/ : /^[1-9]\d{0,18}$/).test(s) &&
    BigInt(s) <= 9223372036854775807n
  );
}
function date(s: string) {
  if (!s) return true;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s) || s.startsWith("0000-")) return false;
  const d = new Date(s + "T00:00:00Z");
  return Number.isFinite(d.valueOf()) && d.toISOString().slice(0, 10) === s;
}
export function merchantDirectoryError(
  d: MerchantDirectoryDraft,
  search: string,
): string {
  if (
    !merchantDirectoryColumns.some((v) => v.value === d.column) ||
    !merchantDirectoryStatuses.some((v) => v.value === d.dstatus) ||
    !merchantDirectorySorts.some((v) => v.value === d.order)
  )
    return "商户筛选参数无效";
  if (
    new TextEncoder().encode(search).length > 256 ||
    /[\x00\r\n]/.test(search) ||
    (d.column === "uid" && search && !validID(search))
  )
    return "请填写规范商户号或搜索内容";
  if ((d.gid && !validID(d.gid, true)) || (d.upid && !validID(d.upid)))
    return "请填写规范用户组ID和上级商户号";
  if (!date(d.from) || !date(d.to) || (d.from && d.to && d.from > d.to))
    return "请核对注册起止日期";
  return "";
}
export function readMerchantDirectoryQuery(raw?: Record<string, unknown>) {
  const draft = newMerchantDirectory();
  let search = "",
    page = 1,
    limit = 20;
  const result = (error: string) => ({ draft, search, page, limit, error });
  if (!raw) return result("");
  const v: Record<string, string> = {},
    allowed = new Set([
      "column",
      "q",
      "value",
      "uid",
      "pay",
      "dstatus",
      "order",
      "from",
      "to",
      "gid",
      "upid",
      "page",
      "limit",
    ]);
  for (const [k, value] of Object.entries(raw)) {
    if (
      !allowed.has(k) ||
      typeof value !== "string" ||
      /[\x00\r\n]/.test(value)
    )
      return result("商户筛选参数无效，请清空后重新查询");
    v[k] = value;
  }
  for (const k of Object.keys(draft) as (keyof MerchantDirectoryDraft)[])
    draft[k] = v[k] || "";
  if (draft.order === "id_desc") draft.order = "";
  for (const [key, max] of [
    ["page", 100000],
    ["limit", 100],
  ] as const) {
    if (!v[key]) continue;
    if (!/^[1-9]\d*$/.test(v[key]) || Number(v[key]) > max)
      return result("商户筛选参数无效：页码或每页数量无效");
    if (key === "page") page = Number(v[key]);
    else limit = Number(v[key]);
  }
  if (v.q !== undefined && v.value !== undefined && v.q !== v.value)
    return result("商户筛选参数无效");
  search = v.value ?? v.q ?? "";
  if (v.uid !== undefined) {
    if (
      !validID(v.uid) ||
      (search && search !== v.uid) ||
      (draft.column && draft.column !== "uid")
    )
      return result("商户筛选参数无效");
    draft.column = "uid";
    search = v.uid;
  }
  if (v.pay) {
    if (
      !/^[012]$/.test(v.pay) ||
      (draft.dstatus && draft.dstatus !== "pay_" + v.pay)
    )
      return result("商户筛选参数无效");
    draft.dstatus = "pay_" + v.pay;
  }
  const error = merchantDirectoryError(draft, search);
  return result(error ? "商户筛选参数无效：" + error : "");
}
export function appendMerchantDirectory(
  q: URLSearchParams,
  d: MerchantDirectoryDraft,
) {
  for (const [k, v] of Object.entries(d)) if (v) q.set(k, v);
  return q;
}
