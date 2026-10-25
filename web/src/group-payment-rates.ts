export const groupPaymentTypes = [
  { key: "alipay", label: "支付宝" },
  { key: "wxpay", label: "微信支付" },
  { key: "sandbox", label: "本地模拟支付" },
] as const;
type Rates = Record<string, { rate?: string }>;
export function readGroupPaymentRates(raw: unknown): Rates {
  const data =
    raw === "" || raw === undefined
      ? {}
      : typeof raw === "string"
        ? JSON.parse(raw)
        : raw;
  if (!data || typeof data !== "object" || Array.isArray(data))
    throw Error("支付方式分成比例格式不正确");
  for (const [key, value] of Object.entries(data)) {
    if (
      !groupPaymentTypes.some((t) => t.key === key) ||
      !value ||
      typeof value !== "object" ||
      Array.isArray(value)
    )
      throw Error("支付方式分成比例包含未识别的配置");
    for (const [field, rate] of Object.entries(value))
      if (field !== "rate" || typeof rate !== "string")
        throw Error("分成比例须为明确的百分数字符串");
  }
  return data as Rates;
}
export function groupPaymentRatesError(raw: unknown): string {
  try {
    const rates = readGroupPaymentRates(raw);
    for (const value of Object.values(rates)) {
      const rate = value.rate ?? "";
      if (rate === "") continue;
      if (!/^(0|[1-9]\d{0,2})(\.\d{1,2})?$/.test(rate))
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
  return JSON.stringify({
    ...readGroupPaymentRates(raw),
    [kind]: { rate: value },
  });
}
