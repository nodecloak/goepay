import { referralID } from "./referral";

export interface ReferralCurrentPolicy {
  merchant_id: string;
  status:
    | "active"
    | "conditional"
    | "disabled"
    | "budget_disabled"
    | "merchant_disabled"
    | "zero_rate"
    | "unknown";
  enabled: boolean | null;
  rate_bps: number | null;
  basis: "fee" | "gross" | "profit" | "unknown";
  mode: "immediate" | "daily" | "unknown";
  payment_fee_enabled: boolean;
  payer_group_dependent: boolean;
  current_group: {
    id: string | null;
    base_group_id: string | null;
    source: "base" | "membership" | "unknown";
    expires_at?: string;
  };
  inheritance: {
    enabled: string;
    rate_bps: string;
    basis: string;
    mode: string;
  };
  refund_policy: "any_successful_refund_voids_reward";
  example: {
    basis_amount: string;
    percentage_amount: string;
    order_estimate_status: "unknown";
    order_estimate: null;
  } | null;
}

export function readReferralCurrentPolicy(
  value: unknown,
  merchant: string,
): ReferralCurrentPolicy {
  const fail = (): never => {
    throw Error("当前政策或本人归属无法确认，请重新读取");
  };
  const record = (v: unknown): Record<string, unknown> =>
    v && typeof v === "object" && !Array.isArray(v)
      ? (v as Record<string, unknown>)
      : fail();
  const p = record(value),
    group = record(p.current_group),
    source = record(p.inheritance);
  if (
    !referralID(merchant) ||
    p.merchant_id !== merchant ||
    ![
      "active",
      "conditional",
      "disabled",
      "budget_disabled",
      "merchant_disabled",
      "zero_rate",
      "unknown",
    ].includes(String(p.status)) ||
    typeof p.payment_fee_enabled !== "boolean" ||
    typeof p.payer_group_dependent !== "boolean" ||
    p.refund_policy !== "any_successful_refund_voids_reward"
  )
    fail();
  for (const k of ["enabled", "rate_bps", "basis", "mode"])
    if (!["default", "global", "promoter_group"].includes(String(source[k])))
      fail();
  const unknown = p.status === "unknown";
  if (unknown) {
    if (
      p.enabled !== null ||
      p.rate_bps !== null ||
      p.basis !== "unknown" ||
      p.mode !== "unknown" ||
      p.example !== null ||
      group.id !== null ||
      group.base_group_id !== null ||
      group.source !== "unknown" ||
      group.expires_at !== undefined
    )
      fail();
  } else {
    if (
      typeof p.enabled !== "boolean" ||
      !Number.isInteger(p.rate_bps) ||
      (p.rate_bps as number) < 0 ||
      (p.rate_bps as number) > 10000 ||
      !["fee", "gross", "profit"].includes(String(p.basis)) ||
      !["immediate", "daily"].includes(String(p.mode)) ||
      !referralID(group.id, true) ||
      !referralID(group.base_group_id, true) ||
      !["base", "membership"].includes(String(group.source))
    )
      fail();
    if (
      group.source === "membership" &&
      (typeof group.expires_at !== "string" ||
        !Number.isFinite(Date.parse(group.expires_at)))
    )
      fail();
    if (group.source === "base" && group.expires_at !== undefined) fail();
    const example = record(p.example);
    if (
      example.basis_amount !== "10000" ||
      example.percentage_amount !== String(p.rate_bps) ||
      example.order_estimate_status !== "unknown" ||
      example.order_estimate !== null
    )
      fail();
    if (
      (p.status === "budget_disabled" && p.payment_fee_enabled !== false) ||
      (p.status === "conditional" && p.payer_group_dependent !== true) ||
      (p.status === "active" &&
        (p.enabled !== true ||
          p.payment_fee_enabled !== true ||
          p.payer_group_dependent !== false ||
          p.rate_bps === 0))
    )
      fail();
  }
  return {
    merchant_id: merchant,
    status: p.status as ReferralCurrentPolicy["status"],
    enabled: p.enabled as boolean | null,
    rate_bps: p.rate_bps as number | null,
    basis: p.basis as ReferralCurrentPolicy["basis"],
    mode: p.mode as ReferralCurrentPolicy["mode"],
    payment_fee_enabled: p.payment_fee_enabled as boolean,
    payer_group_dependent: p.payer_group_dependent as boolean,
    current_group: {
      id: group.id as string | null,
      base_group_id: group.base_group_id as string | null,
      source: group.source as ReferralCurrentPolicy["current_group"]["source"],
      ...(group.expires_at !== undefined
        ? { expires_at: group.expires_at as string }
        : {}),
    },
    inheritance: {
      enabled: source.enabled as string,
      rate_bps: source.rate_bps as string,
      basis: source.basis as string,
      mode: source.mode as string,
    },
    refund_policy: "any_successful_refund_voids_reward",
    example: unknown
      ? null
      : {
          basis_amount: "10000",
          percentage_amount: String(p.rate_bps),
          order_estimate_status: "unknown",
          order_estimate: null,
        },
  };
}
