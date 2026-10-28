import { runtimeChannelPolicyPath } from "./runtime-channel-policy";

export const channelLimitKeys = [
  "daytop",
  "daymaxorder",
  "paymin",
  "paymax",
  "timestart",
  "timestop",
  "rate_limit_seconds",
  "rate_limit_count",
] as const;
export type ChannelLimitFields = Record<
  (typeof channelLimitKeys)[number],
  string
>;
export interface RuntimeChannelLimits extends ChannelLimitFields {
  channel_id: string;
  pay_type: "alipay" | "wxpay";
  configured: boolean;
  revision: string;
  fingerprint: string;
  can_update: boolean;
  timezone: "UTC+08:00";
  semantics: "successful-threshold-v1";
  usage: {
    day: string;
    successful_amount: string;
    successful_count: string;
    recent_starts: string;
    day_paused: boolean;
    rate_limited: boolean;
    as_of: string;
  };
}
const invalid = () => Error("通道限额无法核实，请刷新后重新确认。");
function record(
  raw: unknown,
  keys: readonly string[],
): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
  const v = raw as Record<string, unknown>;
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(v, k))
  )
    throw invalid();
  return v;
}
function count(raw: unknown, maximum = 9223372036854775807n): string {
  if (
    typeof raw !== "string" ||
    !/^(0|[1-9]\d*)$/.test(raw) ||
    raw.length > 19 ||
    BigInt(raw) > maximum
  )
    throw invalid();
  return raw;
}
function money(raw: unknown): string {
  if (typeof raw !== "string" || !/^(0|[1-9]\d*)(?:\.\d{1,2})?$/.test(raw))
    throw invalid();
  const [whole, fraction = ""] = raw.split(".");
  if (
    whole.length > 17 ||
    BigInt(whole) * 100n + BigInt(fraction.padEnd(2, "0")) >
      9223372036854775807n
  )
    throw invalid();
  return whole + "." + fraction.padEnd(2, "0");
}
export function normalizeChannelLimits(
  raw: ChannelLimitFields,
): ChannelLimitFields {
  const out = {} as ChannelLimitFields;
  for (const key of channelLimitKeys) {
    if (typeof raw[key] !== "string") throw invalid();
    const v = raw[key].trim();
    out[key] = ["daytop", "paymin", "paymax"].includes(key)
      ? money(v || "0")
      : ["timestart", "timestop"].includes(key)
        ? v === ""
          ? ""
          : count(v, 23n)
        : count(
            v || "0",
            key === "rate_limit_seconds" ? 2147483647n : 9223372036854775807n,
          );
  }
  const cents = (v: string) => BigInt(v.replace(".", ""));
  if (
    (out.timestart === "") !== (out.timestop === "") ||
    (cents(out.paymax) > 0n && cents(out.paymin) > cents(out.paymax))
  )
    throw invalid();
  return out;
}
export const runtimeChannelLimitsPath = (id: string) =>
  runtimeChannelPolicyPath(id) + "/limits";
export function readRuntimeChannelLimits(raw: unknown): RuntimeChannelLimits {
  const v = record(raw, [
    ...channelLimitKeys,
    "channel_id",
    "pay_type",
    "configured",
    "revision",
    "fingerprint",
    "can_update",
    "timezone",
    "semantics",
    "usage",
  ]);
  if (typeof v.channel_id !== "string") throw invalid();
  runtimeChannelLimitsPath(v.channel_id);
  const normalized = normalizeChannelLimits(v as unknown as ChannelLimitFields);
  if (
    channelLimitKeys.some((k) => normalized[k] !== v[k]) ||
    ![
      "alipay",
      "wxpay",
      "qqpay",
      "bank",
      "jdpay",
      "paypal",
      "douyinpay",
      "ecny",
    ].includes(v.pay_type as string) ||
    typeof v.configured !== "boolean" ||
    typeof v.can_update !== "boolean" ||
    v.timezone !== "UTC+08:00" ||
    v.semantics !== "successful-threshold-v1" ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint)
  )
    throw invalid();
  const revision = count(v.revision);
  if (
    v.configured
      ? revision === "0"
      : revision !== "0" ||
        channelLimitKeys.some(
          (k) =>
            v[k] !==
            (["timestart", "timestop"].includes(k)
              ? ""
              : ["daytop", "paymin", "paymax"].includes(k)
                ? "0.00"
                : "0"),
        )
  )
    throw invalid();
  const u = record(v.usage, [
    "day",
    "successful_amount",
    "successful_count",
    "recent_starts",
    "day_paused",
    "rate_limited",
    "as_of",
  ]);
  if (
    typeof u.day !== "string" ||
    !/^\d{4}-\d{2}-\d{2}$/.test(u.day) ||
    !Number.isFinite(Date.parse(u.day)) ||
    new Date(u.day).toISOString().slice(0, 10) !== u.day ||
    typeof u.successful_amount !== "string" ||
    !/^(0|[1-9]\d{0,38})\.\d{2}$/.test(u.successful_amount) ||
    typeof u.day_paused !== "boolean" ||
    typeof u.rate_limited !== "boolean" ||
    typeof u.as_of !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(u.as_of) ||
    !Number.isFinite(Date.parse(u.as_of))
  )
    throw invalid();
  count(u.successful_count);
  count(u.recent_starts);
  const cents = (v: string) => BigInt(v.replace(".", ""));
  const dayPaused =
    (cents(normalized.daytop) > 0n &&
      cents(u.successful_amount) >= cents(normalized.daytop)) ||
    (BigInt(normalized.daymaxorder) > 0n &&
      BigInt(u.successful_count as string) >= BigInt(normalized.daymaxorder));
  const rateLimited =
    BigInt(normalized.rate_limit_seconds) > 0n &&
    BigInt(normalized.rate_limit_count) > 0n &&
    BigInt(u.recent_starts as string) >= BigInt(normalized.rate_limit_count);
  if (u.day_paused !== dayPaused || u.rate_limited !== rateLimited)
    throw invalid();
  return { ...v, usage: { ...u } } as unknown as RuntimeChannelLimits;
}
export function readRuntimeChannelLimitsResult(
  raw: unknown,
  before: RuntimeChannelLimits,
  desired: ChannelLimitFields,
): RuntimeChannelLimits {
  const after = readRuntimeChannelLimits(raw),
    normalized = normalizeChannelLimits(desired);
  if (
    after.channel_id !== before.channel_id ||
    after.pay_type !== before.pay_type ||
    !after.configured ||
    !after.can_update ||
    BigInt(after.revision) !== BigInt(before.revision) + 1n ||
    after.fingerprint === before.fingerprint ||
    channelLimitKeys.some((k) => after[k] !== normalized[k])
  )
    throw invalid();
  return after;
}
