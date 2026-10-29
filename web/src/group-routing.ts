export type RoutingChannel = { id: string; type: string; methods: string[] };
export type GroupRouteOption = { value: string; label: string; type: string };
import {
  readGroupPaymentRates,
  groupPaymentTypes,
  type GroupPaymentSetting,
} from "./group-payment-rates";
const object = (v: unknown): Record<string, unknown> => {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw Error("通道配置无法核对");
  return v as Record<string, unknown>;
};
const id = (v: unknown): string => {
  if (
    typeof v !== "string" ||
    !/^[1-9][0-9]{0,18}$/.test(v) ||
    BigInt(v) > 9223372036854775807n
  )
    throw Error("通道编号无法精确核对");
  return v;
};
export function readGroupRoutingCatalog(raw: unknown): RoutingChannel[] {
  const data = object(raw);
  if (
    data.available !== true ||
    !Array.isArray(data.items) ||
    data.items.length > 1000
  )
    throw Error("运行通道目录不可用");
  const seen = new Set<string>();
  return data.items.map((row) => {
    const item = object(row),
      key = id(item.id);
    if (
      seen.has(key) ||
      ![
        "alipay",
        "wxpay",
        "qqpay",
        "bank",
        "jdpay",
        "paypal",
        "douyinpay",
        "ecny",
      ].includes(String(item.type)) ||
      typeof item.type !== "string" ||
      !Array.isArray(item.methods) ||
      !item.methods.length ||
      item.methods.length > 32 ||
      item.methods.some(
        (v) =>
          typeof v !== "string" ||
          ![
            "page",
            "wap",
            "native",
            "h5",
            "app",
            "jsapi",
            "mini",
            "applet",
            "scan",
            "preauth",
          ].includes(v),
      ) ||
      new Set(item.methods).size !== item.methods.length
    )
      throw Error("运行通道目录格式错误");
    seen.add(key);
    return { id: key, type: item.type, methods: [...item.methods] as string[] };
  });
}
export function readGroupRoutingRolls(
  raw: unknown,
  channels: RoutingChannel[],
): GroupRouteOption[] {
  if (!Array.isArray(raw) || raw.length > 2000) throw Error("轮询目录无法核对");
  const out: GroupRouteOption[] = [],
    seen = new Set<string>();
  for (const row of raw) {
    const r = object(row),
      key = id(r.id);
    if (seen.has(key)) throw Error("轮询编号重复");
    seen.add(key);
    if (r.enabled === false || r.merchant_id !== "0") continue;
    if (
      r.enabled !== true ||
      typeof r.name !== "string" ||
      !r.name.trim() ||
      r.name.length > 100 ||
      typeof r.type !== "string" ||
      ![
        "alipay",
        "wxpay",
        "qqpay",
        "bank",
        "jdpay",
        "paypal",
        "douyinpay",
        "ecny",
      ].includes(r.type) ||
      typeof r.channel_ids !== "string" ||
      r.channel_ids.length > 1000 ||
      !["first", "round_robin", "weighted", "priority"].includes(
        String(r.strategy),
      )
    )
      throw Error("轮询配置无法核对");
    const entries = r.channel_ids.split(","),
      ids = new Set<string>();
    let total = 0n;
    if (entries.length > 100) throw Error("轮询通道过多");
    for (const entry of entries) {
      const parts = entry.trim().split(":"),
        channel = id(parts[0]);
      if (
        parts.length > 2 ||
        ids.has(channel) ||
        !channels.some((c) => c.id === channel && c.type === r.type)
      )
        throw Error("轮询引用未加载的运行通道");
      ids.add(channel);
      if (parts.length === 2) {
        const weight = parts[1]!;
        if (!/^(0|[1-9][0-9]*)$/.test(weight) || BigInt(weight) > 1000000n)
          throw Error("轮询权重无法核对");
        total += BigInt(weight);
      }
    }
    if (r.strategy === "weighted" && total === 0n)
      throw Error("轮询权重无法核对");
    out.push({
      value: "roll:" + key,
      label: r.name + "（轮询组 #" + key + "）",
      type: r.type,
    });
  }
  return out;
}
export function groupRouteValue(raw: unknown, kind: string): string {
  const value = readGroupPaymentRates(raw)[kind];
  if (value?.channel === undefined) return "default";
  if (value.channel === "0") return "disabled";
  if (value.channel === "-1") return "random";
  if (value.channel === "-2") return "subchannels";
  if (value.channel === "-3") return "random_roll";
  if (value.channel === "-4") return "sequential";
  if (value.channel === "-5") return "first";
  return value.type + ":" + value.channel;
}
export function setGroupPaymentRoute(
  raw: unknown,
  kind: string,
  value: string,
): string {
  if (!groupPaymentTypes.some((t) => t.key === kind))
    throw Error("支付方式无效");
  const current = readGroupPaymentRates(raw);
  if (value === "default") {
    const { channel: _channel, type: _type, ...setting } = current[kind] || {};
    return JSON.stringify({ ...current, [kind]: setting });
  }
  if (kind === "sandbox") throw Error("本地模拟支付仅使用默认通道");
  let channel = "0",
    type: GroupPaymentSetting["type"] = "";
  if (value === "subchannels") {
    channel = "-2";
    type = "channel";
  } else if (value === "random_roll") {
    channel = "-3";
    type = "roll";
  } else if (["first", "random", "sequential"].includes(value)) {
    channel = value === "first" ? "-5" : value === "random" ? "-1" : "-4";
    type = "channel";
  } else if (value !== "disabled") {
    const parts = value.split(":");
    if (parts.length !== 2 || !["channel", "roll"].includes(parts[0]!))
      throw Error("通道选项无效");
    channel = id(parts[1]);
    type = parts[0] as "channel" | "roll";
  }
  return JSON.stringify({
    ...current,
    [kind]: { ...current[kind], channel, type },
  });
}
