import { ApiError } from "./api";
import { fundsDefinitiveRefusal212 } from "./funds-intent212";
export interface CryptoIntent {
  version: 1;
  principal: string;
  target: string;
  body: Readonly<Record<string, string>>;
  digest: string;
}
export function cryptoIntentRefused(error: unknown, intent: CryptoIntent) {
  if (fundsDefinitiveRefusal212(error)) return true;
  const proof =
    error instanceof ApiError && error.responseDecoded && error.status === 409
      ? error.requestNotRecorded
      : undefined;
  return (
    !!proof &&
    proof.kind === "crypto" &&
    proof.merchant_id === intent.body.merchant_id &&
    proof.external_no === intent.body.request_id &&
    proof.reason === "conflict"
  );
}
const problem =
  "无法读取或保存原 USDT 请求存储，请恢复浏览器存储后重试；未发送新请求";
const targets = [
  "/crypto/withdrawals",
  "/crypto/refunds",
  "/crypto/held-refunds",
];
export function canonicalCryptoAmount(value: string): string {
  if (!/^(0|[1-9]\d{0,12})(?:\.\d{1,6})?$/.test(value)) throw Error(problem);
  const [whole, fraction = ""] = value.split(".");
  const rest = fraction.replace(/0+$/, "");
  const amount = whole + (rest ? "." + rest : "");
  if (amount === "0") throw Error(problem);
  return amount;
}
function normalized(target: string, input: Readonly<Record<string, string>>) {
  if (
    !targets.includes(target) ||
    !input ||
    typeof input !== "object" ||
    Array.isArray(input)
  )
    throw Error(problem);
  const allowed = [
    "merchant_id",
    "request_id",
    "invoice_id",
    "network",
    "address",
    "amount",
  ];
  if (
    Object.entries(input).some(
      ([k, v]) =>
        !allowed.includes(k) ||
        typeof v !== "string" ||
        v.length > 128 ||
        /[\x00-\x1f\x7f]/.test(v),
    )
  )
    throw Error(problem);
  if (
    !/^[1-9]\d{0,18}$/.test(input.merchant_id || "") ||
    !/^[!-~]{1,128}$/.test(input.request_id || "") ||
    !input.address ||
    !input.amount
  )
    throw Error(problem);
  if (
    target === "/crypto/withdrawals"
      ? !["tron", "ethereum"].includes(input.network || "") ||
        input.invoice_id !== undefined
      : !/^[a-f0-9]{48}$/.test(input.invoice_id || "") ||
        input.network !== undefined
  )
    throw Error(problem);
  const body: Record<string, string> = {};
  for (const key of allowed)
    if (input[key] !== undefined) body[key] = input[key]!;
  body.amount = canonicalCryptoAmount(body.amount!);
  if (/^0x[0-9a-fA-F]{40}$/.test(body.address!))
    body.address = body.address!.toLowerCase();
  return body;
}
async function digest(
  principal: string,
  target: string,
  body: Readonly<Record<string, string>>,
) {
  const raw = new TextEncoder().encode(
    JSON.stringify([principal, target, body]),
  );
  return Array.from(
    new Uint8Array(await crypto.subtle.digest("SHA-256", raw)),
    (b) => b.toString(16).padStart(2, "0"),
  ).join("");
}
export function verifyCryptoIntentRecord(raw: unknown, intent: CryptoIntent) {
  if (!raw || typeof raw !== "object") throw Error("原 USDT 请求响应无法核对");
  const r = raw as Record<string, unknown>,
    b = intent.body;
  const kind =
    intent.target === "/crypto/withdrawals"
      ? "settlement"
      : intent.target === "/crypto/held-refunds"
        ? "exception_refund"
        : "refund";
  const address =
    typeof r.address === "string" && r.address.startsWith("0x")
      ? r.address.toLowerCase()
      : r.address;
  if (
    !/^[a-f0-9]{48}$/.test(String(r.id)) ||
    String(r.merchant_id) !== b.merchant_id ||
    r.request_id !== b.request_id ||
    r.kind !== kind ||
    address !== b.address ||
    canonicalCryptoAmount(String(r.amount)) !== b.amount ||
    (b.network && r.network !== b.network) ||
    (b.invoice_id && r.invoice_id !== b.invoice_id) ||
    ![
      "requested",
      "approved",
      "preparing",
      "prepared",
      "signing",
      "signed",
      "broadcasting",
      "unknown",
      "confirmed",
      "failed",
      "cancelled",
    ].includes(String(r.state))
  )
    throw Error("原 USDT 请求号、归属、金额或地址无法核对，请保留原请求");
  return {
    id: String(r.id),
    state: String(r.state),
    request_id: String(r.request_id),
  };
}
export class CryptoIntentStore {
  readonly key: string;
  constructor(
    readonly principal: string,
    readonly storage: Storage = localStorage,
  ) {
    if (!/^[a-f0-9]{64}$/.test(principal)) throw Error(problem);
    this.key = `goepay.crypto-intent.${principal}`;
  }
  async read(): Promise<CryptoIntent | null> {
    try {
      const raw = this.storage.getItem(this.key);
      if (raw === null) return null;
      if (raw.length > 4096) throw Error();
      const value = JSON.parse(raw) as CryptoIntent;
      if (
        value.version !== 1 ||
        value.principal !== this.principal ||
        Object.keys(value).some(
          (k) =>
            !["version", "principal", "target", "body", "digest"].includes(k),
        )
      )
        throw Error();
      const body = normalized(value.target, value.body);
      if (
        JSON.stringify(body) !== JSON.stringify(value.body) ||
        value.digest !== (await digest(this.principal, value.target, body))
      )
        throw Error();
      return Object.freeze({ ...value, body: Object.freeze(body) });
    } catch {
      throw Error(problem);
    }
  }
  async begin(
    target: string,
    input: Record<string, string>,
  ): Promise<CryptoIntent> {
    const locks =
      typeof navigator === "undefined" ? undefined : navigator.locks;
    return locks
      ? locks.request(this.key, () => this.beginLocked(target, input))
      : this.beginLocked(target, input);
  }
  private async beginLocked(
    target: string,
    input: Record<string, string>,
  ): Promise<CryptoIntent> {
    const body = normalized(target, input),
      previous = await this.read();
    if (previous) {
      if (
        previous.target !== target ||
        JSON.stringify(previous.body) !== JSON.stringify(body)
      )
        throw Error("原请求尚待核对，不能更换金额、地址、网络或请求号");
      return previous;
    }
    const value: CryptoIntent = {
      version: 1,
      principal: this.principal,
      target,
      body,
      digest: await digest(this.principal, target, body),
    };
    try {
      this.storage.setItem(this.key, JSON.stringify(value));
      if ((await this.read())?.digest !== value.digest) throw Error();
    } catch {
      throw Error(problem);
    }
    return Object.freeze({ ...value, body: Object.freeze(body) });
  }
  async clear(value: CryptoIntent) {
    try {
      const current = await this.read();
      if (current && current.digest !== value.digest) throw Error();
      this.storage.removeItem(this.key);
      if (this.storage.getItem(this.key) !== null) throw Error();
    } catch {
      throw Error(problem);
    }
  }
}
