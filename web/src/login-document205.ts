import { resolvePage, viewKind } from "./menu";

const fallback205 = "/merchant/dashboard";
const secretKeys205 =
  /^(?:return|promotion_token|api_key|merchant_key|key|password|csrf_token|access_token|refresh_token|auth_code|code|ticket|sign|authorization|captcha_token|geetest_validate|geetest_seccode|lot_number|pass_token|captcha_output)$/i;

export function merchantLoginReturn205(value: unknown): string {
  if (
    typeof value !== "string" ||
    !value.startsWith("/merchant/") ||
    /[\u0000-\u0020\u007f-\u009f\\]/.test(value)
  )
    return fallback205;
  try {
    const base = "https://goepay-return.invalid",
      target = new URL(value, base);
    if (
      target.origin !== base ||
      !target.pathname.startsWith("/merchant/") ||
      /^\/merchant\/login\/?$/i.test(target.pathname)
    )
      return fallback205;
    const page = target.pathname.slice("/merchant".length);
    if (
      target.pathname !== "/merchant/scanpay" &&
      !resolvePage("merchant", page) &&
      viewKind("merchant", page) === "unavailable"
    )
      return fallback205;
    const seen = new Set<string>();
    for (const [key, entry] of target.searchParams) {
      if (
        seen.has(key) ||
        secretKeys205.test(key) ||
        /[\u0000-\u001f\u007f-\u009f\\]/.test(key + entry)
      )
        return fallback205;
      seen.add(key);
    }
    if (
      /(?:^|[?&#])(?:promotion_token|api_key|access_token|refresh_token|ticket|authorization)=/i.test(
        decodeURIComponent(target.hash),
      )
    )
      return fallback205;
    return target.pathname + target.search + target.hash;
  } catch {
    return fallback205;
  }
}

export function merchantLoginEntry205(current: string, reason: "password" | "security" | "" = ""): string {
  return (
    "/merchant/login?return=" +
    encodeURIComponent(merchantLoginReturn205(current)) +
    (reason ? "&reauth=" + reason : "")
  );
}
