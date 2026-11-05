import type { Session } from "./api";
import { isPersonalMerchantID } from "./merchant-personal-settings";
export interface MerchantAPISwitches {
  merchant_id: string;
  keylogin: 0 | 1;
  refund: 0 | 1;
  transfer: 0 | 1;
  configured: boolean;
  revision: string;
  fingerprint: string;
  can_update: true;
  transfer_granted: boolean;
}
export interface MerchantAPIDraft {
  keylogin: boolean;
  refund: boolean;
  transfer: boolean;
}
const invalid = () => new Error("功能开关数据无法确认，请刷新页面后核对");
export function parseMerchantAPISwitches(
  value: unknown,
  mid: string,
): MerchantAPISwitches {
  if (
    !isPersonalMerchantID(mid) ||
    !value ||
    typeof value !== "object" ||
    Array.isArray(value)
  )
    throw invalid();
  const v = value as Record<string, unknown>,
    keys = [
      "merchant_id",
      "keylogin",
      "refund",
      "transfer",
      "configured",
      "revision",
      "fingerprint",
      "can_update",
      "transfer_granted",
    ];
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(v, k)) ||
    v.merchant_id !== mid ||
    v.can_update !== true ||
    typeof v.configured !== "boolean" ||
    typeof v.transfer_granted !== "boolean" ||
    typeof v.revision !== "string" ||
    !/^(0|[1-9][0-9]{0,18})$/.test(v.revision) ||
    BigInt(v.revision) > 9223372036854775807n ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint) ||
    [v.keylogin, v.refund, v.transfer].some((x) => x !== 0 && x !== 1)
  )
    throw invalid();
  if (
    v.configured
      ? v.revision === "0"
      : v.revision !== "0" ||
        v.keylogin !== 1 ||
        v.refund !== 1 ||
        v.transfer !== (v.transfer_granted ? 1 : 0)
  )
    throw invalid();
  return { ...v } as unknown as MerchantAPISwitches;
}
export function merchantAPISwitchBody(
  state: MerchantAPISwitches,
  draft: MerchantAPIDraft,
) {
  parseMerchantAPISwitches(state, state.merchant_id);
  if (
    [draft.keylogin, draft.refund, draft.transfer].some(
      (x) => typeof x !== "boolean",
    )
  )
    throw invalid();
  return {
    keylogin: draft.keylogin ? 1 : 0,
    refund: draft.refund ? 1 : 0,
    transfer: draft.transfer ? 1 : 0,
    expected_fingerprint: state.fingerprint,
  };
}
export function parseMerchantKeySession(value: unknown, mid: string): Session {
  const bad = () => new Error("密钥登录结果无法确认，请重新检查登录状态");
  if (
    !isPersonalMerchantID(mid) ||
    !value ||
    typeof value !== "object" ||
    Array.isArray(value)
  )
    throw bad();
  const v = value as Record<string, unknown>,
    u = v.user as Record<string, unknown> | undefined;
  if (
    Object.keys(v).length !== 2 ||
    !Object.hasOwn(v, "user") ||
    !Object.hasOwn(v, "csrf_token") ||
    typeof v.csrf_token !== "string" ||
    !/^[A-Za-z0-9_-]{43}$/.test(v.csrf_token) ||
    !u ||
    typeof u !== "object" ||
    Array.isArray(u) ||
    Object.keys(u).length !== 3 ||
    u.id !== mid ||
    typeof u.name !== "string" ||
    !u.name ||
    u.role !== "merchant"
  )
    throw bad();
  return {
    user: { id: mid, name: u.name, role: "merchant" },
    csrf_token: v.csrf_token,
  };
}
