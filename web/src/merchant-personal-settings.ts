export interface PersonalDraft {
  qq: string;
  pay_minmoney: string;
  pay_maxmoney: string;
}
export interface PersonalSettings extends PersonalDraft {
  merchant_id: string;
  bounds_valid: boolean;
  fingerprint: string;
  can_update: true;
}
const maxInt64 = 9223372036854775807n;
const invalid = () => new Error("个人设置数据无法确认，请重新读取");
export function isPersonalMerchantID(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[1-9][0-9]{0,18}$/.test(value) &&
    BigInt(value) <= maxInt64
  );
}
function amount(value: unknown): { text: string; cents: bigint } {
  if (typeof value !== "string") throw invalid();
  const input = value.trim();
  if (!input) return { text: "", cents: 0n };
  if (input.length > 21 || !/^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$/.test(input))
    throw new Error("金额须为非负元金额，最多两位小数");
  const [whole, part = ""] = input.split(".");
  const fraction = part.padEnd(2, "0");
  const cents = BigInt(whole) * 100n + BigInt(fraction);
  if (cents > maxInt64) throw new Error("金额超出允许范围");
  return { text: `${whole}.${fraction}`, cents };
}
function qq(value: unknown): string {
  if (typeof value !== "string") throw invalid();
  const input = value.trim();
  if (input !== "" && !/^[1-9][0-9]{4,19}$/.test(input))
    throw new Error("QQ须为5至20位数字");
  return input;
}
export function parsePersonalSettings(
  value: unknown,
  merchantID: string,
): PersonalSettings {
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    !isPersonalMerchantID(merchantID)
  )
    throw invalid();
  const v = value as Record<string, unknown>;
  const keys = [
    "merchant_id",
    "qq",
    "pay_minmoney",
    "pay_maxmoney",
    "bounds_valid",
    "fingerprint",
    "can_update",
  ];
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((key) => !Object.hasOwn(v, key)) ||
    !isPersonalMerchantID(v.merchant_id) ||
    v.merchant_id !== merchantID ||
    v.can_update !== true ||
    typeof v.bounds_valid !== "boolean" ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint)
  )
    throw invalid();
  const contact = qq(v.qq),
    min = amount(v.pay_minmoney),
    max = amount(v.pay_maxmoney);
  if (
    contact !== v.qq ||
    min.text !== v.pay_minmoney ||
    max.text !== v.pay_maxmoney ||
    v.bounds_valid !== (max.cents === 0n || min.cents <= max.cents)
  )
    throw invalid();
  return {
    merchant_id: merchantID,
    qq: contact,
    pay_minmoney: min.text,
    pay_maxmoney: max.text,
    bounds_valid: v.bounds_valid,
    fingerprint: v.fingerprint,
    can_update: true,
  };
}
export function personalSettingsBody(
  state: PersonalSettings,
  draft: PersonalDraft,
) {
  parsePersonalSettings(state, state.merchant_id);
  const contact = qq(draft.qq),
    min = amount(draft.pay_minmoney),
    max = amount(draft.pay_maxmoney);
  if (max.cents > 0n && min.cents > max.cents)
    throw new Error("最大支付金额不能小于最小支付金额");
  return {
    qq: contact,
    pay_minmoney: min.text,
    pay_maxmoney: max.text,
    expected_fingerprint: state.fingerprint,
  };
}
