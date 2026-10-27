export interface WelcomeContext {
  merchantId: string;
  dataMerchantId: string;
  businessDate: string;
  content: string;
  ownSession: boolean;
}
interface PreferenceStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}
const fallback = new Map<string, string>();
export function welcomeOnce(
  c: WelcomeContext,
  storage?: PreferenceStorage,
): boolean {
  if (
    !c.ownSession ||
    !c.content.trim() ||
    c.content.length > 20000 ||
    !/^[1-9]\d{0,18}$/.test(c.merchantId) ||
    BigInt(c.merchantId) > 9223372036854775807n ||
    c.merchantId !== c.dataMerchantId ||
    !/^\d{4}-\d{2}-\d{2}$/.test(c.businessDate)
  )
    return false;
  const date = new Date(c.businessDate + "T00:00:00Z");
  if (
    !Number.isFinite(date.getTime()) ||
    date.toISOString().slice(0, 10) !== c.businessDate
  )
    return false;
  const key = "goepay:welcome:v1:" + c.merchantId;
  if (fallback.get(key) === c.businessDate) return false;
  try {
    const s = storage || window.localStorage;
    if (s.getItem(key) === c.businessDate) return false;
    s.setItem(key, c.businessDate);
  } catch {
    /* Display preference only; payment does not depend on storage. */
  }
  fallback.delete(key);
  fallback.set(key, c.businessDate);
  if (fallback.size > 64) fallback.delete(fallback.keys().next().value!);
  return true;
}
export interface PublicAnnouncementLayout {
  zhuce: string;
  footer: string;
  format: "html";
}
export function publicAnnouncementLayout(
  value: unknown,
): PublicAnnouncementLayout {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("页面排版格式不正确");
  const v = value as Record<string, unknown>;
  if (
    typeof v.zhuce !== "string" ||
    typeof v.footer !== "string" ||
    v.zhuce.length > 20000 ||
    v.footer.length > 20000 ||
    v.format !== "html"
  )
    throw Error("页面排版格式不正确");
  return { zhuce: v.zhuce, footer: v.footer, format: "html" };
}
