export interface SystemLogFilter {
  column: "uid" | "ip";
  value: string;
  error?: string;
}
function validUID(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^(0|[1-9]\d{0,18})$/.test(value) &&
    BigInt(value) <= 9223372036854775807n
  );
}
export function systemLogRouteQuery(
  query: Record<string, unknown>,
): SystemLogFilter {
  const bad = (): SystemLogFilter => ({
    column: "uid",
    value: "",
    error: "日志链接参数不正确，请重置后查询",
  });
  for (const [key, value] of Object.entries(query))
    if (!["uid", "column", "value"].includes(key) || typeof value !== "string")
      return bad();
  if (query.uid !== undefined) {
    if (
      query.column !== undefined ||
      query.value !== undefined ||
      !validUID(query.uid)
    )
      return bad();
    return { column: "uid", value: query.uid };
  }
  const column = query.column ?? "uid",
    value = query.value ?? "";
  if (
    (column !== "uid" && column !== "ip") ||
    typeof value !== "string" ||
    value.length > 253 ||
    /[\u0000\r\n]/.test(value) ||
    (column === "uid" && value !== "" && !validUID(value))
  )
    return bad();
  return { column, value };
}
export function systemLogMerchantHref(
  row: Record<string, unknown>,
): string | undefined {
  return row.subject_kind === "merchant" && validUID(row.uid) && row.uid !== "0"
    ? "/admin/merchant/merchant-list?" + new URLSearchParams({ uid: row.uid })
    : undefined;
}
export function systemLogIPHref(value: unknown): string | undefined {
  if (typeof value !== "string" || value.length > 64) return;
  if (/^(\d{1,3}\.){3}\d{1,3}$/.test(value)) {
    if (
      !value
        .split(".")
        .every((part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255)
    )
      return;
  } else {
    if (!/^[0-9a-f:.]+$/i.test(value) || !value.includes(":")) return;
    try {
      new URL("http://[" + value + "]/");
    } catch {
      return;
    }
  }
  return (
    "https://m.ip138.com/iplookup.asp?" + new URLSearchParams({ ip: value })
  );
}
