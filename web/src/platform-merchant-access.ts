export type AccessFlag = "0" | "1";
export type AccessField = "close_keylogin" | "user_refund";
export interface PlatformMerchantAccess {
  close_keylogin: AccessFlag;
  user_refund: AccessFlag;
  fingerprint: string;
  can_update: true;
}
export interface PublicMerchantAccess {
  close_keylogin: 0 | 1;
  user_refund: 0 | 1;
}
const invalid = () => Error("平台功能状态无法确认，请重新读取");
function record(value: unknown, keys: string[]): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw invalid();
  const v = value as Record<string, unknown>;
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(v, k))
  )
    throw invalid();
  return v;
}
export function parsePlatformMerchantAccess(
  value: unknown,
): PlatformMerchantAccess {
  const v = record(value, [
    "close_keylogin",
    "user_refund",
    "fingerprint",
    "can_update",
  ]);
  if (
    [v.close_keylogin, v.user_refund].some((x) => x !== "0" && x !== "1") ||
    v.can_update !== true ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint)
  )
    throw invalid();
  return { ...v } as unknown as PlatformMerchantAccess;
}
export function parsePublicMerchantAccess(
  value: unknown,
): PublicMerchantAccess {
  const v = record(value, ["close_keylogin", "user_refund"]);
  if ([v.close_keylogin, v.user_refund].some((x) => x !== 0 && x !== 1))
    throw invalid();
  return { ...v } as unknown as PublicMerchantAccess;
}
export function platformMerchantAccessBody(
  state: PlatformMerchantAccess,
  field: AccessField,
  enabled: boolean,
) {
  parsePlatformMerchantAccess(state);
  if (
    (field !== "close_keylogin" && field !== "user_refund") ||
    typeof enabled !== "boolean"
  )
    throw invalid();
  return {
    close_keylogin:
      field === "close_keylogin" ? (enabled ? "0" : "1") : state.close_keylogin,
    user_refund:
      field === "user_refund" ? (enabled ? "1" : "0") : state.user_refund,
    expected_fingerprint: state.fingerprint,
  };
}
export async function fetchPublicMerchantAccess(
  signal?: AbortSignal,
): Promise<PublicMerchantAccess> {
  const r = await fetch("/console/v1/public/merchant-access", {
    method: "GET",
    headers: { Accept: "application/json" },
    cache: "no-store",
    credentials: "omit",
    signal,
  });
  if (!r.ok) throw invalid();
  const v = record(await r.json(), ["code", "message", "data"]);
  if (v.code !== 0 || typeof v.message !== "string") throw invalid();
  return parsePublicMerchantAccess(v.data);
}
