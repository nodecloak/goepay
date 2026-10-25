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
      const rate = rates[type.key]?.rate;
      if (rate === undefined) continue;
      const inherited = rate === "" || /^0+(?:\.0{1,2})?$/.test(rate);
      lines.push({
        label: type.label,
        value: inherited ? "继承统一分成" + unified + "%" : rate + "%",
      });
    }
    return { lines, error: "" };
  } catch {
    return { lines: [], error: "费率配置需核对" };
  }
}
