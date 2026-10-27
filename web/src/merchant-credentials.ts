export type MerchantCredentialOperation = "reset-md5" | "generate-rsa";
export interface MerchantCredentialState {
  merchant_id: string;
  revision: string;
  algorithm: "MD5" | "RSA";
  public_key: string;
  md5_configured: boolean;
  fingerprint: string;
}
const invalid = () => new Error("密钥响应无法核实，请刷新当前状态后重新确认");
function object(raw: unknown, keys: string[]): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
  const value = raw as Record<string, unknown>;
  if (
    Object.keys(value).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(value, k))
  )
    throw invalid();
  return value;
}
function id(raw: unknown): raw is string {
  return (
    typeof raw === "string" &&
    /^[1-9]\d{0,18}$/.test(raw) &&
    BigInt(raw) <= 9223372036854775807n
  );
}
function fingerprint(raw: unknown): raw is string {
  return typeof raw === "string" && /^[a-f0-9]{64}$/.test(raw);
}
export function merchantCredentialPath(merchantID: string): string {
  if (!id(merchantID)) throw invalid();
  return "/merchants/" + merchantID + "/credentials";
}
export function readMerchantCredentialState(
  raw: unknown,
  merchantID: string,
): MerchantCredentialState {
  merchantCredentialPath(merchantID);
  const v = object(raw, [
    "merchant_id",
    "revision",
    "algorithm",
    "public_key",
    "md5_configured",
    "fingerprint",
  ]);
  if (
    v.merchant_id !== merchantID ||
    !id(v.revision) ||
    (v.algorithm !== "MD5" && v.algorithm !== "RSA") ||
    typeof v.public_key !== "string" ||
    v.public_key.length > 8192 ||
    typeof v.md5_configured !== "boolean" ||
    !fingerprint(v.fingerprint)
  )
    throw invalid();
  return {
    merchant_id: merchantID,
    revision: v.revision,
    algorithm: v.algorithm,
    public_key: v.public_key,
    md5_configured: v.md5_configured,
    fingerprint: v.fingerprint,
  };
}
export function readMerchantCredentialResult(
  raw: unknown,
  before: MerchantCredentialState,
  operation: MerchantCredentialOperation,
): { state: MerchantCredentialState; apiKey?: string; privateKey?: string } {
  const keys = [
      "merchant_id",
      "revision",
      "algorithm",
      "fingerprint",
      ...(operation === "reset-md5"
        ? ["api_key"]
        : ["public_key", "private_key"]),
    ],
    v = object(raw, keys);
  if (
    v.merchant_id !== before.merchant_id ||
    !id(v.revision) ||
    BigInt(v.revision) !== BigInt(before.revision) + 1n ||
    v.algorithm !== before.algorithm ||
    !fingerprint(v.fingerprint) ||
    v.fingerprint === before.fingerprint
  )
    throw invalid();
  const state = { ...before, revision: v.revision, fingerprint: v.fingerprint };
  if (operation === "reset-md5") {
    if (typeof v.api_key !== "string" || !/^[a-f0-9]{32}$/.test(v.api_key))
      throw invalid();
    return { state: { ...state, md5_configured: true }, apiKey: v.api_key };
  }
  if (
    typeof v.public_key !== "string" ||
    v.public_key.length > 8192 ||
    !/^-----BEGIN PUBLIC KEY-----\r?\n[A-Za-z0-9+/=\r\n]+-----END PUBLIC KEY-----\r?\n?$/.test(
      v.public_key,
    ) ||
    typeof v.private_key !== "string" ||
    v.private_key.length > 8192 ||
    !/^-----BEGIN PRIVATE KEY-----\r?\n[A-Za-z0-9+/=\r\n]+-----END PRIVATE KEY-----\r?\n?$/.test(
      v.private_key,
    )
  )
    throw invalid();
  return {
    state: { ...state, public_key: v.public_key },
    privateKey: v.private_key,
  };
}
