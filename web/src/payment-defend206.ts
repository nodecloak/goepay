export const paymentDefend206Keys = [
  "pay_verify",
  "pay_verify_type",
  "pay_verify_check_second",
  "pay_verify_check_count",
  "pay_verify_check_rate",
  "pay_verify_check_ip",
  "pay_verify_check_uid",
] as const;
export type PaymentDefend206Settings = Record<
  (typeof paymentDefend206Keys)[number],
  string
>;
export interface PaymentDefend206Policy {
  settings: PaymentDefend206Settings;
  configured: boolean;
  revision: string;
  fingerprint: string;
  can_update: true;
}
export interface PaymentDefend206Update {
  settings: PaymentDefend206Settings;
  expected_fingerprint: string;
}
export const paymentDefend206Modes = [
  { value: "0", label: "关闭" },
  { value: "1", label: "按规则开启" },
  { value: "2", label: "指定商户开启" },
  { value: "3", label: "全部商户开启" },
] as const;
export const paymentDefend206Types = [
  { value: "0", label: "JS跳转验证（速度快）" },
  { value: "1", label: "极验无感验证（速度慢）" },
  { value: "2", label: "极验滑动验证（速度慢且体验差）" },
  { value: "3", label: "禁止支付" },
] as const;
const invalid = () => Error("支付验证设置格式无法核对");
const maximumID = 9223372036854775807n;
function object(
  value: unknown,
  keys: readonly string[],
): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw invalid();
  const v = value as Record<string, unknown>;
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((key) => !Object.hasOwn(v, key))
  )
    throw invalid();
  return v;
}
function integer(value: string): boolean {
  // Match the server's text grammar/resource bound without rewriting zeros.
  return value === "" || (value.length <= 128 && /^[0-9]+$/.test(value));
}
export function readPaymentDefend206Settings(
  value: unknown,
): PaymentDefend206Settings {
  const v = object(value, paymentDefend206Keys);
  if (
    paymentDefend206Keys.some(
      (key) =>
        typeof v[key] !== "string" ||
        (v[key] as string).length > 8192 ||
        /[\u0000-\u001f\u007f-\u009f]/.test(v[key] as string),
    )
  )
    throw invalid();
  const settings = Object.fromEntries(
    paymentDefend206Keys.map((key) => [key, v[key]]),
  ) as PaymentDefend206Settings;
  if (
    !/^[0-3]$/.test(settings.pay_verify) ||
    !/^[0-3]$/.test(settings.pay_verify_type) ||
    !integer(settings.pay_verify_check_second) ||
    !integer(settings.pay_verify_check_count) ||
    !integer(settings.pay_verify_check_ip)
  )
    throw invalid();
  const rate = settings.pay_verify_check_rate;
  if (rate !== "" && (rate.length > 128 || !/^[0-9]+(?:\.[0-9]+)?$/.test(rate)))
    throw invalid();
  if (settings.pay_verify_check_uid !== "") {
    const ids = settings.pay_verify_check_uid.split("|");
    if (
      ids.some(
        (id) =>
          !/^[0-9]+$/.test(id) || BigInt(id) <= 0n || BigInt(id) > maximumID,
      )
    )
      throw invalid();
  }
  return settings;
}
export function validPaymentDefend206Settings(value: unknown): boolean {
  try {
    readPaymentDefend206Settings(value);
    return true;
  } catch {
    return false;
  }
}
export function readPaymentDefend206Policy(
  value: unknown,
): PaymentDefend206Policy {
  const v = object(value, [
    "settings",
    "configured",
    "revision",
    "fingerprint",
    "can_update",
  ]);
  if (
    typeof v.configured !== "boolean" ||
    typeof v.revision !== "string" ||
    !/^(0|[1-9][0-9]{0,18})$/.test(v.revision) ||
    BigInt(v.revision) > maximumID ||
    v.configured !== (v.revision !== "0") ||
    typeof v.fingerprint !== "string" ||
    !/^[0-9a-f]{64}$/.test(v.fingerprint) ||
    v.can_update !== true
  )
    throw invalid();
  const settings = readPaymentDefend206Settings(v.settings);
  if (!v.configured && settings.pay_verify !== "0") throw invalid();
  return {
    settings,
    configured: v.configured,
    revision: v.revision,
    fingerprint: v.fingerprint,
    can_update: true,
  };
}
export function paymentDefend206Body(
  policy: PaymentDefend206Policy,
  draft: PaymentDefend206Settings,
): PaymentDefend206Update {
  const original = readPaymentDefend206Policy(policy);
  return {
    settings: readPaymentDefend206Settings(draft),
    expected_fingerprint: original.fingerprint,
  };
}
export function samePaymentDefend206Settings(
  a: PaymentDefend206Settings,
  b: PaymentDefend206Settings,
): boolean {
  return paymentDefend206Keys.every((key) => a[key] === b[key]);
}
