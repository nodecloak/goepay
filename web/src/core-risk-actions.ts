export interface CoreRiskAction {
  id: string;
  kind: "ip" | "buyer" | "merchant" | "channel" | "subchannel";
  target_id: string;
  revision: string;
  rule: string;
  active: boolean;
  until: string | null;
  changed_at: string;
  source_count: number;
  source_order_ids: string[];
  fingerprint: string;
}
const invalid = () => Error("风控阻断记录无法核实");
const id = (v: unknown, zero = false): v is string =>
  typeof v === "string" &&
  (zero ? /^(0|[1-9]\d{0,18})$/ : /^[1-9]\d{0,18}$/).test(v) &&
  BigInt(v) <= 9223372036854775807n;
const time = (v: unknown) =>
  typeof v === "string" &&
  /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(v) &&
  Number.isFinite(Date.parse(v));
export function readCoreRiskAction(raw: unknown): CoreRiskAction {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
  const v = raw as Record<string, unknown>;
  if (
    Object.keys(v).sort().join() !==
      [
        "id",
        "kind",
        "target_id",
        "revision",
        "rule",
        "active",
        "until",
        "changed_at",
        "source_count",
        "source_order_ids",
        "fingerprint",
      ]
        .sort()
        .join() ||
    !id(v.id) ||
    !id(v.target_id, true) ||
    !id(v.revision) ||
    !["ip", "buyer", "merchant", "channel", "subchannel"].includes(
      String(v.kind),
    ) ||
    ![
      "auto_check_payip",
      "auto_check_payspeed",
      "auto_check_sucrate",
      "auto_check_channel",
    ].includes(String(v.rule)) ||
    typeof v.active !== "boolean" ||
    (v.until !== null && !time(v.until)) ||
    !time(v.changed_at) ||
    !Number.isSafeInteger(v.source_count) ||
    (v.source_count as number) < 0 ||
    (v.source_count as number) > 100000 ||
    !Array.isArray(v.source_order_ids) ||
    v.source_order_ids.length > 20 ||
    v.source_order_ids.length > (v.source_count as number) ||
    v.source_order_ids.some((x) => !id(x)) ||
    new Set(v.source_order_ids).size !== v.source_order_ids.length ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint)
  )
    throw invalid();
  if (
    (v.kind === "merchant" ||
      v.kind === "channel" ||
      v.kind === "subchannel") &&
    v.target_id === "0"
  )
    throw invalid();
  return {
    ...v,
    source_order_ids: [...v.source_order_ids],
  } as unknown as CoreRiskAction;
}
export function readCoreRiskActions(raw: unknown): CoreRiskAction[] {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
  const v = raw as Record<string, unknown>;
  if (
    Object.keys(v).sort().join() !== "items,total" ||
    !Array.isArray(v.items) ||
    v.items.length > 10000 ||
    v.total !== v.items.length
  )
    throw invalid();
  const items = v.items.map(readCoreRiskAction);
  if (new Set(items.map((i) => i.id)).size !== items.length) throw invalid();
  return items;
}
export const coreRiskKind = (kind: string) =>
  ({
    ip: "IP（已脱敏）",
    buyer: "买家（已脱敏）",
    merchant: "商户",
    channel: "父通道",
    subchannel: "子通道",
  })[kind] || "风控对象";
export const coreRiskRule = (rule: string) =>
  ({
    auto_check_payip: "IP 连续未付",
    auto_check_payspeed: "买家支付频率",
    auto_check_sucrate: "商户支付成功率",
    auto_check_channel: "通道连续未付",
  })[rule] || "核心付款规则";
export function readCoreRiskRelease(
  raw: unknown,
  before: CoreRiskAction,
): CoreRiskAction {
  const v = readCoreRiskAction(raw);
  if (
    v.kind !== before.kind ||
    v.target_id !== before.target_id ||
    v.rule !== before.rule ||
    v.active ||
    v.until !== null ||
    v.revision !== String(BigInt(before.revision) + 1n) ||
    v.id === before.id ||
    v.fingerprint === before.fingerprint ||
    JSON.stringify(v.source_order_ids) !==
      JSON.stringify(before.source_order_ids) ||
    v.source_count !== before.source_count
  )
    throw invalid();
  return v;
}
