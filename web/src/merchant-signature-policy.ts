export interface SignaturePolicy {
  merchant_id: string;
  keytype: 0 | 1 | null;
  algorithm: "MD5" | "RSA";
  rsa_configured: boolean;
  fingerprint: string;
  can_update: boolean;
}
const invalid = () => new Error("签名方式响应无法核实，请刷新后重新确认");
export function signaturePolicyPath(
  area: "merchant" | "admin",
  merchantID?: string,
): string {
  if (area === "merchant" && merchantID === undefined)
    return "/account/signature-policy";
  if (
    area !== "admin" ||
    typeof merchantID !== "string" ||
    !/^[1-9]\d{0,18}$/.test(merchantID) ||
    BigInt(merchantID) > 9223372036854775807n
  )
    throw invalid();
  return "/merchants/" + merchantID + "/signature-policy";
}
export function readSignaturePolicy(raw: unknown): SignaturePolicy {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
  const v = raw as Record<string, unknown>,
    keys = [
      "merchant_id",
      "keytype",
      "algorithm",
      "rsa_configured",
      "fingerprint",
      "can_update",
    ];
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(v, k)) ||
    typeof v.merchant_id !== "string" ||
    !/^[1-9]\d{0,18}$/.test(v.merchant_id) ||
    BigInt(v.merchant_id) > 9223372036854775807n ||
    ![null, 0, 1].includes(v.keytype as null) ||
    !(v.algorithm === "MD5" || v.algorithm === "RSA") ||
    typeof v.rsa_configured !== "boolean" ||
    typeof v.can_update !== "boolean" ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint) ||
    (v.keytype === 1 && !v.rsa_configured)
  )
    throw invalid();
  return v as unknown as SignaturePolicy;
}
export function readSignaturePolicyResult(
  raw: unknown,
  before: SignaturePolicy,
  desired: 0 | 1,
): SignaturePolicy {
  const after = readSignaturePolicy(raw);
  if (
    after.merchant_id !== before.merchant_id ||
    after.keytype !== desired ||
    after.algorithm !== before.algorithm ||
    after.rsa_configured !== before.rsa_configured ||
    after.fingerprint === before.fingerprint ||
    !after.can_update
  )
    throw invalid();
  return after;
}
export function signaturePolicyLabel(v: SignaturePolicy): string {
  return v.keytype === 0
    ? "MD5+RSA兼容"
    : v.keytype === 1
      ? "仅RSA"
      : `仅${v.algorithm}（历史配置）`;
}
