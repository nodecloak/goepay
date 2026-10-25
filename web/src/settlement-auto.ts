import type { AuthorizationDraft } from "./settlement-plans";
export interface AutoPlan {
  id: string;
  revision: string;
  last_authorization_id: string;
  merchant_ids: string[];
}
export interface AutoWalletChannel {
  id: string;
  provider: string;
  product: string;
  app_id: string;
  payer_masked: string;
  merchant_ids: string[];
}
export interface AutoAuthorization {
  id: string;
  plan_id: string;
  plan_revision?: string;
  generation_authorization_id?: string;
  revoked?: boolean;
  available: boolean;
  wallet_channels: AutoWalletChannel[];
  valid_from?: string;
  expires_at?: string;
  max_item_gross?: string;
  max_run_gross?: string;
  max_day_gross?: string;
  max_day_items?: string;
}
const integer = (v: unknown, zero = false): v is string =>
  typeof v === "string" &&
  (zero ? /^(0|[1-9]\d*)$/ : /^[1-9]\d*$/).test(v) &&
  BigInt(v) <= 9223372036854775807n;
const object = (v: unknown): v is Record<string, unknown> =>
  v !== null && typeof v === "object" && !Array.isArray(v);
function instant(v: unknown): string {
  if (
    typeof v !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(
      v,
    ) ||
    !Number.isFinite(Date.parse(v))
  )
    throw new Error("请输入带时区的RFC3339时间");
  return v;
}
export function readAutoAuthorization(
  raw: unknown,
  plan: AutoPlan,
): AutoAuthorization {
  if (
    !object(raw) ||
    !integer(raw.id, true) ||
    raw.plan_id !== plan.id ||
    typeof raw.available !== "boolean" ||
    !Array.isArray(raw.wallet_channels)
  )
    throw new Error("自动出款授权身份或格式不匹配，请重新读取");
  const seen = new Set<string>();
  for (const c of raw.wallet_channels) {
    if (
      !object(c) ||
      !integer(c.id) ||
      seen.has(c.id) ||
      typeof c.app_id !== "string" ||
      !c.app_id ||
      typeof c.payer_masked !== "string" ||
      !/^\*{3}/.test(c.payer_masked) ||
      !Array.isArray(c.merchant_ids) ||
      !c.merchant_ids.length ||
      !c.merchant_ids.every((v) => integer(v)) ||
      new Set(c.merchant_ids).size !== c.merchant_ids.length ||
      !(
        (c.provider === "alipay" &&
          [
            "alipay.fund.trans.toaccount.transfer",
            "alipay.fund.trans.uni.transfer",
          ].includes(String(c.product))) ||
        (c.provider === "wxpay" &&
          c.product === "wechat.v2.promotion.transfers")
      )
    )
      throw new Error("通道缺少明确的钱包产品或商户准入范围");
    seen.add(c.id);
  }
  if (raw.id !== "0") {
    if (
      !integer(raw.plan_revision) ||
      !integer(raw.generation_authorization_id) ||
      typeof raw.revoked !== "boolean"
    )
      throw new Error("原自动出款授权版本不完整");
    instant(raw.valid_from);
    instant(raw.expires_at);
    for (const key of [
      "max_item_gross",
      "max_run_gross",
      "max_day_gross",
      "max_day_items",
    ])
      if (!integer(raw[key])) throw new Error("原授权上限不完整");
  }
  return raw as unknown as AutoAuthorization;
}
export function autoAuthorizationBody(
  plan: AutoPlan,
  current: AutoAuthorization,
  draft: AuthorizationDraft,
  channels: Record<string, string>,
  costAcknowledged: boolean,
): Record<string, unknown> {
  if (!costAcknowledged)
    throw new Error("请先确认机构另收费不包含在本授权限额内");
  if (
    !integer(plan.id) ||
    !integer(plan.revision) ||
    !integer(plan.last_authorization_id) ||
    current.plan_id !== plan.id ||
    !integer(current.id, true) ||
    !current.available
  )
    throw new Error("请先读取当前计划及其有效的原生成授权");
  if (
    !plan.merchant_ids.length ||
    plan.merchant_ids.length > 100 ||
    new Set(plan.merchant_ids).size !== plan.merchant_ids.length ||
    Object.keys(channels).length !== plan.merchant_ids.length
  )
    throw new Error("请选择原计划每个商户的明确通道");
  const selected = plan.merchant_ids
    .map((mid) => {
      if (
        !integer(mid) ||
        !integer(channels[mid]) ||
        !current.wallet_channels.some(
          (c) => c.id === channels[mid] && c.merchant_ids.includes(mid),
        )
      )
        throw new Error("所选通道未准入原商户");
      return { merchant_id: mid, channel_id: channels[mid] };
    })
    .sort((a, b) => (BigInt(a.merchant_id) < BigInt(b.merchant_id) ? -1 : 1));
  const from = instant(draft.valid_from),
    to = instant(draft.expires_at),
    duration = Date.parse(to) - Date.parse(from);
  if (duration <= 0 || duration > 31 * 86400000)
    throw new Error("自动出款授权最长31天，截止须晚于开始");
  for (const key of [
    "max_item_gross",
    "max_run_gross",
    "max_day_gross",
    "max_day_items",
  ] as const)
    if (!integer(draft[key])) throw new Error("毛额及条数须为规范正整数");
  if (
    BigInt(draft.max_item_gross) > BigInt(draft.max_run_gross) ||
    BigInt(draft.max_run_gross) > BigInt(draft.max_day_gross) ||
    BigInt(draft.max_day_items) > 100n
  )
    throw new Error("请核对单项、每次、每日上限；每日最多100条");
  return {
    expected_revision: plan.revision,
    expected_authorization_id: current.id,
    generation_authorization_id: plan.last_authorization_id,
    valid_from: from,
    expires_at: to,
    max_item_gross: draft.max_item_gross,
    max_run_gross: draft.max_run_gross,
    max_day_gross: draft.max_day_gross,
    max_day_items: draft.max_day_items,
    cost_policy: "provider_managed_excluded",
    channels: selected,
  };
}
