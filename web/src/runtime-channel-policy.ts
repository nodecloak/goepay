import { readCoreRiskStatus, type CoreRiskStatus } from "./core-risk-status";
export interface RuntimeChannelPolicy {
  core_risk?: CoreRiskStatus;
  channel_id: string;
  pay_type: "alipay" | "wxpay";
  methods: string[];
  configured: boolean;
  rate: string | null;
  fee_rate_bps: number | null;
  mode: 0;
  enabled: boolean;
  revision: string;
  fingerprint: string;
  can_update: boolean;
}
export interface RuntimeChannelPolicyChange {
  rate: string;
  mode: 0;
  enabled: boolean;
}
const invalid = () => new Error("支付通道配置无法核实，请刷新后重新确认");
function record(raw: unknown, keys: string[]): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
  const v = raw as Record<string, unknown>;
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(v, k))
  )
    throw invalid();
  return v;
}
function id(v: unknown, zero = false): v is string {
  return (
    typeof v === "string" &&
    (zero ? /^(0|[1-9]\d{0,18})$/ : /^[1-9]\d{0,18}$/).test(v) &&
    BigInt(v) <= 9223372036854775807n
  );
}
export function runtimeChannelPolicyPath(channelID: string): string {
  if (!id(channelID)) throw invalid();
  return "/channel-policies/" + channelID;
}
export function channelRetainedRate(raw: string): {
  rate: string;
  retainedBPS: number;
  feeBPS: number;
} {
  const value = raw.trim() || "100";
  if (!/^(0|[1-9]\d{0,2})(?:\.\d{1,2})?$/.test(value)) throw invalid();
  const [whole, fraction = ""] = value.split("."),
    bps = Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
  if (bps > 10000) throw invalid();
  return {
    rate: whole + "." + fraction.padEnd(2, "0"),
    retainedBPS: bps,
    feeBPS: 10000 - bps,
  };
}
export function readRuntimeChannelPolicy(raw: unknown): RuntimeChannelPolicy {
  const hasRisk =
    !!raw && typeof raw === "object" && Object.hasOwn(raw, "core_risk");
  const v = record(raw, [
    "channel_id",
    "pay_type",
    "methods",
    "configured",
    "rate",
    "fee_rate_bps",
    "mode",
    "enabled",
    "revision",
    "fingerprint",
    "can_update",
    ...(hasRisk ? ["core_risk"] : []),
  ]);
  if (
    !id(v.channel_id) ||
    !(v.pay_type === "alipay" || v.pay_type === "wxpay") ||
    !Array.isArray(v.methods) ||
    v.methods.length === 0 ||
    v.methods.length > 32 ||
    v.methods.some(
      (m) => typeof m !== "string" || !/^[a-z][a-z0-9_]{0,31}$/.test(m),
    ) ||
    new Set(v.methods).size !== v.methods.length ||
    typeof v.configured !== "boolean" ||
    typeof v.enabled !== "boolean" ||
    typeof v.can_update !== "boolean" ||
    v.mode !== 0 ||
    !id(v.revision, true) ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint)
  )
    throw invalid();
  if (v.configured) {
    if (
      !id(v.revision) ||
      typeof v.rate !== "string" ||
      !/^\d{1,3}\.\d{2}$/.test(v.rate)
    )
      throw invalid();
    const p = channelRetainedRate(v.rate);
    if (p.rate !== v.rate || p.feeBPS !== v.fee_rate_bps) throw invalid();
  } else if (
    v.revision !== "0" ||
    v.rate !== null ||
    v.fee_rate_bps !== null ||
    v.enabled !== true
  )
    throw invalid();
  if (hasRisk) v.core_risk = readCoreRiskStatus(v.core_risk);
  return { ...v, methods: [...v.methods] } as unknown as RuntimeChannelPolicy;
}
export function readRuntimeChannelCatalog(
  raw: unknown,
): RuntimeChannelPolicy[] {
  const v = record(raw, ["items", "total"]);
  if (
    !Array.isArray(v.items) ||
    !Number.isSafeInteger(v.total) ||
    v.total !== v.items.length
  )
    throw invalid();
  const items = v.items.map(readRuntimeChannelPolicy);
  if (new Set(items.map((v) => v.channel_id)).size !== items.length)
    throw invalid();
  return items;
}
export function readRuntimeChannelPolicyResult(
  raw: unknown,
  before: RuntimeChannelPolicy,
  desired: RuntimeChannelPolicyChange,
): RuntimeChannelPolicy {
  const after = readRuntimeChannelPolicy(raw);
  if (
    after.channel_id !== before.channel_id ||
    after.pay_type !== before.pay_type ||
    JSON.stringify(after.methods) !== JSON.stringify(before.methods) ||
    !after.configured ||
    !after.can_update ||
    BigInt(after.revision) !== BigInt(before.revision) + 1n ||
    after.fingerprint === before.fingerprint ||
    after.rate !== channelRetainedRate(desired.rate).rate ||
    after.enabled !== desired.enabled ||
    after.mode !== desired.mode
  )
    throw invalid();
  return after;
}
