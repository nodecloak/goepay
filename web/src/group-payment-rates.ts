export const groupPaymentTypes = [
  { key: "alipay", label: "支付宝" },
  { key: "wxpay", label: "微信支付" },
  { key: "sandbox", label: "本地模拟支付" },
] as const;
export type GroupPaymentSetting = {
  rate?: string;
  channel?: string;
  type?: "" | "channel" | "roll";
};
type Rates = Record<string, GroupPaymentSetting>;
export function readGroupPaymentRates(raw: unknown): Rates {
  const data =
    raw === "" || raw === undefined
      ? {}
      : typeof raw === "string"
        ? JSON.parse(raw)
        : raw;
  if (!data || typeof data !== "object" || Array.isArray(data))
    throw Error("支付方式分成比例格式不正确");
  const out: Rates = {};
  for (const [key, value] of Object.entries(data)) {
    if (
      !groupPaymentTypes.some((t) => t.key === key) ||
      !value ||
      typeof value !== "object" ||
      Array.isArray(value)
    )
      throw Error("支付方式分成比例包含未识别的配置");
    const fields = value as Record<string, unknown>,
      setting: GroupPaymentSetting = {};
    for (const [field, content] of Object.entries(fields)) {
      if (!["rate", "channel", "type"].includes(field))
        throw Error("支付方式包含未识别的配置");
      if (field === "rate") {
        if (typeof content !== "string")
          throw Error("分成比例须为明确的百分数字符串");
        setting.rate = content;
      }
    }
    if (Object.hasOwn(fields, "channel") !== Object.hasOwn(fields, "type"))
      throw Error("通道编号与类型须同时提供");
    if (Object.hasOwn(fields, "channel")) {
      const raw = fields.channel;
      if (
        (typeof raw !== "string" &&
          (typeof raw !== "number" || !Number.isSafeInteger(raw))) ||
        Object.is(raw, -0)
      )
        throw Error("通道编号无法精确核对");
      const channel = String(raw),
        type = fields.type;
      if (
        !/^(?:0|-1|-2|-3|-4|-5|[1-9][0-9]*)$/.test(channel) ||
        BigInt(channel) > 9223372036854775807n ||
        typeof type !== "string" ||
        !["", "channel", "roll"].includes(type)
      )
        throw Error("通道配置无效");
      if (
        (channel === "-3"
          ? type !== "roll"
          : BigInt(channel) < 0n && type !== "channel" && type !== "") ||
        (BigInt(channel) > 0n && type !== "channel" && type !== "roll") ||
        key === "sandbox"
      )
        throw Error("通道配置尚不支持");
      setting.channel = channel;
      setting.type = type as GroupPaymentSetting["type"];
    }
    out[key] = setting;
  }
  return out;
}
export function groupPaymentRatesError(raw: unknown): string {
  try {
    const rates = readGroupPaymentRates(raw);
    for (const value of Object.values(rates)) {
      const rate = value.rate ?? "";
      if (rate === "") continue;
      if (!/^\d+(\.\d{1,2})?$/.test(rate))
        return "分成比例请输入0至100的百分数，最多两位小数";
      const [whole, fraction = ""] = rate.split(".");
      if (BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0")) > 10000n)
        return "分成比例不得大于100%";
    }
    return "";
  } catch {
    return "支付方式分成比例格式不正确，请核对原配置";
  }
}
export function setGroupPaymentRate(
  raw: unknown,
  kind: string,
  value: string,
): string {
  if (!groupPaymentTypes.some((t) => t.key === kind))
    throw Error("支付方式无效");
  const current = readGroupPaymentRates(raw);
  return JSON.stringify({
    ...current,
    [kind]: { ...current[kind], rate: value },
  });
}
