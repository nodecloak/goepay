import {
  groupPaymentTypes,
  readGroupPaymentRates,
  groupPaymentRatesError,
} from "./group-payment-rates";
export function groupUsersHref(id: unknown): string | undefined {
  const raw =
    typeof id === "string"
      ? id
      : typeof id === "number" && Number.isSafeInteger(id)
        ? String(id)
        : "";
  if (!/^(0|[1-9]\d{0,18})$/.test(raw) || BigInt(raw) > 9223372036854775807n)
    return;
  return "/admin/merchant/merchant-list?" + new URLSearchParams({ gid: raw });
}
export function groupDirectoryRates(row: Record<string, unknown>): {
  lines: { label: string; value: string }[];
  error: string;
} {
  try {
    const raw = String(row.rate_bps ?? "0");
    if (!/^\d{1,32}$/.test(raw) || BigInt(raw) > 10000n) throw Error();
    if (groupPaymentRatesError(row.info)) throw Error();
    const retained = 10000n - BigInt(raw),
      unified =
        (retained / 100n).toString() +
        "." +
        (retained % 100n).toString().padStart(2, "0"),
      rates = readGroupPaymentRates(row.info);
    const lines = [{ label: "统一分成", value: unified + "%" }];
    for (const type of groupPaymentTypes) {
      const setting = rates[type.key];
      if (setting === undefined) continue;
      const rate = setting.rate ?? "";
      const inherited = rate === "" || /^0+(?:\.0{1,2})?$/.test(rate);
      const route =
        setting.channel === undefined
          ? ""
          : setting.channel === "0"
            ? "关闭"
            : setting.channel === "-1"
              ? "随机可用"
              : setting.channel === "-3"
                ? "随机可用轮询组"
                : setting.channel === "-4"
                  ? "顺序可用"
                  : setting.channel === "-5"
                    ? "首个可用通道"
                    : setting.type === "roll"
                      ? "轮询组 #" + setting.channel
                      : "通道 #" + setting.channel;
      lines.push({
        label: type.label,
        value:
          (route ? route + " · " : "") +
          (inherited ? "继承统一分成" + unified + "%" : rate + "%"),
      });
    }
    return { lines, error: "" };
  } catch {
    return { lines: [], error: "费率配置需核对" };
  }
}
