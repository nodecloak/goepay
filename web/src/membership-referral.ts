import { referralID } from "./referral";
export interface MembershipReferralPolicyView {
  available: true;
  id: string;
  scope: "global" | "group";
  scope_id: string;
  revision: string;
  enabled: boolean | null;
  rate_bps: number | null;
}
export function readMembershipReferralPolicy(
  value: unknown,
  scope: string,
  id: string,
): MembershipReferralPolicyView {
  const v = value as Partial<MembershipReferralPolicyView> | null;
  if (
    !v ||
    typeof v !== "object" ||
    Array.isArray(v) ||
    v.available !== true ||
    !["global", "group"].includes(scope) ||
    (scope === "global" ? id !== "0" : !referralID(id)) ||
    v.scope !== scope ||
    v.scope_id !== id ||
    !referralID(v.id, true) ||
    !referralID(v.revision, true) ||
    (v.enabled !== null && typeof v.enabled !== "boolean") ||
    (v.rate_bps !== null &&
      (!Number.isInteger(v.rate_bps) ||
        Number(v.rate_bps) < 0 ||
        Number(v.rate_bps) > 10000)) ||
    (scope === "global" &&
      (v.enabled === null || v.rate_bps === null) &&
      !(
        v.id === "0" &&
        v.revision === "0" &&
        v.enabled === null &&
        v.rate_bps === null
      ))
  )
    throw new Error("会员奖励政策响应不匹配，请重新读取");
  return v as MembershipReferralPolicyView;
}
export function membershipReferralPolicyBody(
  view: MembershipReferralPolicyView,
  enabled: string,
  rate: string,
): Record<string, unknown> {
  readMembershipReferralPolicy(view, view.scope, view.scope_id);
  if (
    !["inherit", "enabled", "disabled"].includes(enabled) ||
    (rate !== "" &&
      (!/^(0|[1-9]\d{0,4})$/.test(rate) || Number(rate) > 10000)) ||
    (view.scope === "global" && (enabled === "inherit" || rate === ""))
  )
    throw new Error("请明确全局启用状态和0–10000整数BPS；用户组可留空继承");
  return {
    expected: view.revision,
    enabled: enabled === "inherit" ? null : enabled === "enabled",
    rate_bps: rate === "" ? null : Number(rate),
  };
}
