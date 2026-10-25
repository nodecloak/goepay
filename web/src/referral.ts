import { beijingTimestamp } from "./console-datetime";

export const validPromotionToken = (token: string) =>
  /^rp_[A-Za-z0-9_-]{43}$/.test(token);
export function sanitizePromotionLocation(
  href: string,
  replace: (clean: string) => void,
): string {
  const url = new URL(href),
    fragment = new URLSearchParams(url.hash.slice(1));
  const candidates = [
    ...url.searchParams.getAll("promotion_token"),
    ...fragment.getAll("promotion_token"),
  ];
  if (!candidates.length) return "";
  url.searchParams.delete("promotion_token");
  fragment.delete("promotion_token");
  url.hash = fragment.toString();
  replace(url.pathname + url.search + url.hash);
  return candidates.length === 1 && validPromotionToken(candidates[0]!)
    ? candidates[0]!
    : "";
}
let pendingPromotion = "";
export function capturePromotionPrefill() {
  pendingPromotion = sanitizePromotionLocation(window.location.href, (clean) =>
    window.history.replaceState(window.history.state, "", clean),
  );
}
export function consumePromotionPrefill() {
  const token = pendingPromotion;
  pendingPromotion = "";
  return token;
}

export interface ReferralPolicyView {
  id: string;
  scope: "global" | "group";
  scope_id: string;
  revision: string;
  enabled: boolean | null;
  rate_bps: number | null;
  mode: "immediate" | "daily";
  daily_mode_available?: boolean;
  profit_basis_available?: boolean;
  membership_referral_policy_available?: boolean;
  basis: "fee" | "gross" | "profit";
  refund_policy: "any_successful_refund_voids_reward";
  created_at?: string;
}
export function referralPolicyBody(
  view: ReferralPolicyView,
  enabled: string,
  rate: string,
  basis: ReferralPolicyView["basis"] = view.basis,
  mode: ReferralPolicyView["mode"] = view.mode,
): Record<string, unknown> {
  if (
    !["inherit", "enabled", "disabled"].includes(enabled) ||
    !["immediate", "daily"].includes(view.mode) ||
    !["immediate", "daily"].includes(mode) ||
    ((view.mode === "daily" || mode === "daily") &&
      view.daily_mode_available !== true) ||
    !["fee", "gross", "profit"].includes(view.basis) ||
    !["fee", "gross", "profit"].includes(basis) ||
    ((view.basis === "profit" || basis === "profit") &&
      view.profit_basis_available !== true) ||
    view.refund_policy !== "any_successful_refund_voids_reward" ||
    !/^(0|[1-9]\d*)$/.test(view.revision)
  )
    throw new Error("请重新核对原政策版本");
  if (rate !== "" && (!/^(0|[1-9]\d*)$/.test(rate) || Number(rate) > 10000))
    throw new Error("比例须为0–10000的整数BPS");
  if (view.scope === "global" && (enabled === "inherit" || rate === ""))
    throw new Error("全局政策必须明确启用状态和比例");
  return {
    expected: view.revision,
    enabled: enabled === "inherit" ? null : enabled === "enabled",
    rate_bps: rate === "" ? null : Number(rate),
    mode,
    basis,
    refund_policy: "any_successful_refund_voids_reward",
  };
}
export function referralAmount(value: string): string {
  const n = BigInt(value);
  return `${n / 100n}.${(n % 100n).toString().padStart(2, "0")}`;
}

export interface ReferralDailyQueueItem {
  order_id: string;
  merchant_id: string;
  promoter_id: string;
  trade_no: string;
  amount: string;
  platform_fee: string;
  reward_amount: string;
  pending_amount: string;
  state: "pending" | "awarded" | "blocked" | "voided";
  activated_at: string;
  business_day: string;
  eligible_day: string;
  source_version: 2;
  source_verified: true;
}
export const referralID = (v: unknown, zero = false): v is string =>
  typeof v === "string" &&
  (zero ? /^(0|[1-9]\d{0,18})$/ : /^[1-9]\d{0,18}$/).test(v) &&
  BigInt(v) <= 9223372036854775807n;
function calendarDay(v: unknown): v is string {
  return (
    typeof v === "string" &&
    /^\d{4}-\d{2}-\d{2}$/.test(v) &&
    v.slice(0, 4) !== "0000" &&
    Number.isFinite(Date.parse(v + "T00:00:00Z")) &&
    new Date(v + "T00:00:00Z").toISOString().slice(0, 10) === v
  );
}
export function readReferralDailyQueue(
  value: unknown,
  promoter: string,
  after: string,
): { items: ReferralDailyQueueItem[]; next_cursor: string } {
  const fail = (): never => {
    throw Error("待发奖励来源或归属无法核实，请重新读取");
  };
  if (
    !referralID(promoter) ||
    !referralID(after, true) ||
    !value ||
    typeof value !== "object" ||
    Array.isArray(value)
  )
    return fail();
  const page = value as Record<string, unknown>;
  if (
    !Array.isArray(page.items) ||
    page.items.length > 20 ||
    typeof page.next_cursor !== "string"
  )
    return fail();
  const items: ReferralDailyQueueItem[] = [];
  let previous = after;
  for (const raw of page.items) {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) return fail();
    const r = raw as Record<string, unknown>;
    if (
      !referralID(r.order_id) ||
      BigInt(r.order_id) <= BigInt(previous) ||
      !referralID(r.merchant_id) ||
      r.promoter_id !== promoter ||
      typeof r.trade_no !== "string" ||
      !r.trade_no ||
      r.trade_no.length > 128 ||
      /[\x00-\x1f\x7f]/.test(r.trade_no) ||
      ![r.amount, r.platform_fee, r.reward_amount, r.pending_amount].every(
        (v) => referralID(v, true),
      ) ||
      BigInt(r.amount as string) <= 0n ||
      BigInt(r.platform_fee as string) > BigInt(r.amount as string) ||
      BigInt(r.reward_amount as string) > BigInt(r.platform_fee as string) ||
      BigInt(r.reward_amount as string) <= 0n ||
      BigInt(r.pending_amount as string) > BigInt(r.reward_amount as string) ||
      (r.state === "pending"
        ? r.pending_amount !== r.reward_amount
        : r.pending_amount !== "0") ||
      !["pending", "awarded", "blocked", "voided"].includes(String(r.state)) ||
      r.source_version !== 2 ||
      r.source_verified !== true ||
      typeof r.activated_at !== "string" ||
      beijingTimestamp(r.activated_at) === r.activated_at ||
      !calendarDay(r.business_day) ||
      !calendarDay(r.eligible_day) ||
      beijingTimestamp(r.activated_at).slice(0, 10) !== r.business_day ||
      Date.parse(r.eligible_day + "T00:00:00Z") -
        Date.parse(r.business_day + "T00:00:00Z") !==
        86400000
    )
      return fail();
    // Copy only documented public fields. Extra provider/proof data never reaches the view.
    items.push(
      Object.fromEntries(
        [
          "order_id",
          "merchant_id",
          "promoter_id",
          "trade_no",
          "amount",
          "platform_fee",
          "reward_amount",
          "pending_amount",
          "state",
          "activated_at",
          "business_day",
          "eligible_day",
          "source_version",
          "source_verified",
        ].map((k) => [k, r[k]]),
      ) as unknown as ReferralDailyQueueItem,
    );
    previous = r.order_id;
  }
  if (
    page.next_cursor !== "" &&
    (!items.length || page.next_cursor !== previous)
  )
    return fail();
  return { items, next_cursor: page.next_cursor };
}
