import type { Area } from "./menu";
export function browserRechargeMethods(type: string): string[] {
  return type === "alipay"
    ? ["page", "wap"]
    : type === "wxpay"
      ? ["native", "h5"]
      : [];
}
export type CommerceKind = "memberships" | "deposits" | "recharges";
export const commercePages: Record<Area, Record<string, CommerceKind>> = {
  admin: {
    "/merchant/group-buy": "memberships",
    "/merchant/deposit-review": "deposits",
  },
  merchant: {
    "/account/groupbuy": "memberships",
    "/account/deposit": "deposits",
    "/account/recharge": "recharges",
  },
};
export const commerceFor = (area: Area, path: string) =>
  commercePages[area][path];
export function safePaymentUrl(value: unknown): string | undefined {
  try {
    const url = new URL(String(value), location.origin);
    return ["http:", "https:"].includes(url.protocol) ? url.href : undefined;
  } catch {
    return undefined;
  }
}
