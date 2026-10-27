import { isPersonalMerchantID } from "./merchant-personal-settings";
const invalid = () => Error("手续费设置无法确认，请重新读取");
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
function metadata(v: Record<string, unknown>) {
  if (
    typeof v.configured !== "boolean" ||
    v.can_update !== true ||
    typeof v.fingerprint !== "string" ||
    !/^[0-9a-f]{64}$/.test(v.fingerprint) ||
    typeof v.revision !== "string" ||
    !/^(0|[1-9][0-9]{0,18})$/.test(v.revision) ||
    BigInt(v.revision) > 9223372036854775807n ||
    v.configured !== (v.revision !== "0")
  )
    throw invalid();
}
export interface BuyerFee104 {
  merchant_id: string;
  mode: 0 | 1;
  mode_feerate: string;
  configured: boolean;
  revision: string;
  fingerprint: string;
  can_update: true;
  partial_available: true;
}
export function parseBuyerFee104(
  value: unknown,
  merchant: string,
): BuyerFee104 {
  const v = record(value, [
    "merchant_id",
    "mode",
    "mode_feerate",
    "configured",
    "revision",
    "fingerprint",
    "can_update",
    "partial_available",
  ]);
  metadata(v);
  if (
    !isPersonalMerchantID(merchant) ||
    v.merchant_id !== merchant ||
    (v.mode !== 0 && v.mode !== 1) ||
    typeof v.mode_feerate !== "string" ||
    v.partial_available !== true ||
    (v.mode_feerate !== "" &&
      buyerRate104(v.mode_feerate) !== v.mode_feerate) ||
    (v.mode === 0 && v.mode_feerate !== "") ||
    (!v.configured && (v.mode !== 0 || v.mode_feerate !== ""))
  )
    throw invalid();
  return v as unknown as BuyerFee104;
}
export function buyerFeeBody104(
  state: BuyerFee104,
  draft: { mode: number; mode_feerate: string },
) {
  parseBuyerFee104(state, state.merchant_id);
  if (draft.mode !== 0 && draft.mode !== 1) throw invalid();
  const rate = draft.mode === 0 ? "" : draft.mode_feerate.trim();
  const canonical = rate === "" ? "" : buyerRate104(rate);
  return {
    mode: draft.mode,
    mode_feerate: canonical,
    expected_fingerprint: state.fingerprint,
  };
}
export interface Minimum104 {
  kind: "global" | "parent";
  channel_id: string;
  payfee_lessthan: string;
  payfee_mincost: string;
  effective_payfee_lessthan: string;
  effective_payfee_mincost: string;
  configured: boolean;
  revision: string;
  fingerprint: string;
  can_update: true;
}
function currency(value: unknown, blank: boolean) {
  if (typeof value !== "string") throw invalid();
  const text = value.trim();
  if (text === "" && blank) return "";
  if (!/^(0|[1-9][0-9]{0,16})(\.[0-9]{1,2})?$/.test(text))
    throw Error("手续费金额须为非负元金额，最多两位小数");
  const [whole, fraction = ""] = text.split(".");
  if (
    BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0")) >
    9223372036854775807n
  )
    throw Error("手续费金额超出允许范围");
  return `${whole}.${fraction.padEnd(2, "0")}`;
}
export function parseMinimum104(
  value: unknown,
  kind: "global" | "parent",
  id: string,
): Minimum104 {
  const v = record(value, [
    "kind",
    "channel_id",
    "payfee_lessthan",
    "payfee_mincost",
    "effective_payfee_lessthan",
    "effective_payfee_mincost",
    "configured",
    "revision",
    "fingerprint",
    "can_update",
  ]);
  metadata(v);
  if (
    v.kind !== kind ||
    v.channel_id !== id ||
    (kind === "global" && id !== "0") ||
    (kind === "parent" && !isPersonalMerchantID(id))
  )
    throw invalid();
  for (const key of ["payfee_lessthan", "payfee_mincost"] as const) {
    if (
      currency(v[key], kind === "parent") !== v[key] ||
      currency(v[`effective_${key}`], false) !== v[`effective_${key}`]
    )
      throw invalid();
    if (kind === "global" && v[key] !== v[`effective_${key}`]) throw invalid();
  }
  if (
    kind === "parent" &&
    v.payfee_lessthan !== "" &&
    v.payfee_lessthan !== "0.00" &&
    v.payfee_mincost !== "" &&
    v.payfee_mincost !== "0.00" &&
    (v.effective_payfee_lessthan !== v.payfee_lessthan ||
      v.effective_payfee_mincost !== v.payfee_mincost)
  )
    throw invalid();
  if (
    !v.configured &&
    (kind === "global"
      ? v.payfee_lessthan !== "0.00" || v.payfee_mincost !== "0.00"
      : v.payfee_lessthan !== "" || v.payfee_mincost !== "")
  )
    throw invalid();
  return v as unknown as Minimum104;
}
export function minimumBody104(
  state: Minimum104,
  draft: { payfee_lessthan: string; payfee_mincost: string },
) {
  parseMinimum104(state, state.kind, state.channel_id);
  return {
    payfee_lessthan: currency(draft.payfee_lessthan, state.kind === "parent"),
    payfee_mincost: currency(draft.payfee_mincost, state.kind === "parent"),
    expected_fingerprint: state.fingerprint,
  };
}

function buyerRate104(text: string): string {
  if (!/^(0|[1-9][0-9]{0,2})(\.[0-9]{1,12})?$/.test(text))
    throw Error("买家承担费率须为0到100的百分比，最多12位小数");
  const [whole, fraction = ""] = text.split(".");
  if (
    BigInt(whole!) * 1000000000000n + BigInt(fraction.padEnd(12, "0")) >
    100000000000000n
  )
    throw Error("买家承担费率不能超过100%");
  const tail = fraction.replace(/0+$/, "");
  return tail ? whole + "." + tail : whole!;
}
