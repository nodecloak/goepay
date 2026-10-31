import type { InjectionKey, ComputedRef } from "vue";
import { ApiError } from "./api";

export interface FundsPrincipal212 {
  area: string;
  id: string;
  issuer: string;
  csrf: string;
}
export const fundsPrincipal212: InjectionKey<
  ComputedRef<FundsPrincipal212 | null>
> = Symbol("funds-principal-212");
export type FundsKind212 = "refund" | "settlements" | "internal-transfers";
export interface FundsIntent212 {
  version: 1;
  principal: string;
  kind: FundsKind212;
  object: string;
  target: string;
  body: Readonly<Record<string, string>>;
  phase: "unknown" | "accepted";
  record?: { id: string; status: string };
}
const problem =
  "无法安全读取或保存原资金请求，请保留页面并恢复浏览器存储后重试；尚未发送新的请求";
const kinds = ["refund", "settlements", "internal-transfers"];
const int64 = (value: unknown, zero = false): value is string =>
  typeof value === "string" &&
  /^(0|[1-9]\d{0,18})$/.test(value) &&
  BigInt(value) <= 9223372036854775807n &&
  (zero || BigInt(value) > 0n);
export async function fundsScope212(
  context: FundsPrincipal212,
): Promise<string> {
  if (
    !["admin", "merchant"].includes(context.area) ||
    !/^[1-9]\d{0,18}$/.test(context.id) ||
    !context.csrf
  )
    throw new Error("本人会话尚未核实，不能保存资金请求");
  const data = new TextEncoder().encode(
    JSON.stringify([context.area, context.id, context.issuer]),
  );
  const digest = await crypto.subtle.digest("SHA-256", data);
  // Only this one-way namespace is persisted; never a password, CSRF proof or
  // payment-authorization token. The same principal can recover after login;
  // callers separately invalidate in-flight work when the live session changes.
  return Array.from(new Uint8Array(digest), (n) =>
    n.toString(16).padStart(2, "0"),
  ).join("");
}
export function fundsDefinitiveRefusal212(
  error: unknown,
  intent?: FundsIntent212,
): boolean {
  if (
    error instanceof ApiError &&
    error.responseDecoded &&
    error.status === 409
  ) {
    const proof = error.requestNotRecorded;
    return (
      !!intent &&
      !!proof &&
      proof.kind === intent.kind &&
      proof.merchant_id === intent.object &&
      proof.external_no === intent.body.external_no
    );
  }
  return (
    error instanceof ApiError &&
    error.responseDecoded &&
    [400, 401, 403, 404, 405, 410, 422, 428, 429].includes(error.status)
  );
}
export class FundsIntentStore212 {
  readonly key: string;
  constructor(
    readonly principal: string,
    readonly kind: FundsKind212,
    readonly object: string,
    readonly storage: Storage = localStorage,
  ) {
    if (
      !/^[a-f0-9]{64}$/.test(principal) ||
      !kinds.includes(kind) ||
      !/^[A-Za-z0-9_-]{1,64}$/.test(object)
    )
      throw new Error(problem);
    this.key = `goepay.funds-intent212.${principal}.${kind}.${object}`;
  }
  read(): FundsIntent212 | null {
    try {
      const raw = this.storage.getItem(this.key);
      if (raw === null) return null;
      if (raw.length > 8192) throw new Error();
      const v = JSON.parse(raw) as FundsIntent212;
      const fields =
        this.kind === "refund"
          ? ["amount", "external_no"]
          : this.kind === "internal-transfers"
            ? ["amount", "external_no", "merchant_id", "recipient_id"]
            : [
                "amount",
                "external_no",
                "merchant_id",
                "expected_fee_cents",
                "expected_net_cents",
                "recipient_config_id",
              ];
      const target =
        this.kind === "refund"
          ? `/orders/${this.object}/refund`
          : `/workflows/${this.kind}`;
      if (
        v.version !== 1 ||
        v.principal !== this.principal ||
        v.kind !== this.kind ||
        v.object !== this.object ||
        v.target !== target ||
        !["unknown", "accepted"].includes(v.phase) ||
        !v.body ||
        typeof v.body !== "object" ||
        Array.isArray(v.body)
      )
        throw new Error();
      if (
        Object.entries(v.body).some(
          ([k, value]) =>
            !fields.includes(k) ||
            typeof value !== "string" ||
            value.length > 128 ||
            /[\x00-\x1f\x7f]/.test(value),
        )
      )
        throw new Error();
      if (
        !/^(0|[1-9]\d{0,16})(?:\.\d{1,2})?$/.test(v.body.amount) ||
        !v.body.external_no ||
        Object.keys(v).some(
          (k) =>
            ![
              "version",
              "principal",
              "kind",
              "object",
              "target",
              "body",
              "phase",
              "record",
            ].includes(k),
        )
      )
        throw new Error();
      const amount = fundsAmountCents212(v.body.amount);
      if (
        !int64(amount) ||
        (this.kind !== "refund" &&
          (!int64(this.object) ||
            (v.body.merchant_id !== undefined &&
              v.body.merchant_id !== this.object)))
      )
        throw new Error();
      if (this.kind === "internal-transfers" && !int64(v.body.recipient_id))
        throw new Error();
      if (
        this.kind === "settlements" &&
        (!int64(v.body.recipient_config_id) ||
          !int64(v.body.expected_fee_cents, true) ||
          !int64(v.body.expected_net_cents) ||
          BigInt(v.body.expected_fee_cents) +
            BigInt(v.body.expected_net_cents) !==
            BigInt(amount))
      )
        throw new Error();
      if (v.phase === "accepted" && !v.record) throw new Error();
      if (
        v.record &&
        (!int64(v.record.id) ||
          !(
            this.kind === "refund"
              ? [
                  "pending",
                  "unknown",
                  "processing",
                  "reserved",
                  "submitted",
                  "succeeded",
                  "failed",
                ]
              : ["pending", "approved", "rejected"]
          ).includes(v.record.status) ||
          Object.keys(v.record).some((k) => !["id", "status"].includes(k)))
      )
        throw new Error();
      return Object.freeze({ ...v, body: Object.freeze({ ...v.body }) });
    } catch {
      throw new Error(problem);
    }
  }
  begin(target: string, body: Record<string, string>): FundsIntent212 {
    const old = this.read();
    if (old) {
      if (
        old.target !== target ||
        JSON.stringify(old.body) !== JSON.stringify(body)
      )
        throw new Error(
          "尚有原资金请求待核对，不能换金额、收款版本或请求号重新提交",
        );
      return old;
    }
    return this.save({
      version: 1,
      principal: this.principal,
      kind: this.kind,
      object: this.object,
      target,
      body: { ...body },
      phase: "unknown",
    });
  }
  accept(record: { id: string; status: string }): FundsIntent212 {
    const value = this.read();
    if (!value) throw new Error(problem);
    return this.save({ ...value, phase: "accepted", record });
  }
  private save(value: FundsIntent212): FundsIntent212 {
    try {
      const raw = JSON.stringify(value);
      this.storage.setItem(this.key, raw);
      if (this.storage.getItem(this.key) !== raw) throw new Error();
      const saved = this.read();
      if (!saved) throw new Error();
      return saved;
    } catch {
      throw new Error(problem);
    }
  }
  clear(): void {
    try {
      this.storage.removeItem(this.key);
      if (this.storage.getItem(this.key) !== null) throw new Error();
    } catch {
      throw new Error(problem);
    }
  }
}

export function fundsAmountCents212(money: string): string {
  if (!/^\d+(?:\.\d{1,2})?$/.test(money)) throw new Error("原请求金额无效");
  const [whole, fraction = ""] = money.split(".");
  return (BigInt(whole) * 100n + BigInt(fraction.padEnd(2, "0"))).toString();
}
export function verifiedRefund212(
  raw: unknown,
  intent: Pick<FundsIntent212, "body">,
  owner: { merchant_id?: string; order_id?: string },
  httpStatus: number,
): { id: string; status: string } {
  const problem =
    "原退款响应与已保存的请求号、金额或订单归属无法核对，请保留原请求";
  if (!raw || typeof raw !== "object" || ![200, 202].includes(httpStatus))
    throw new Error(problem);
  const r = raw as Record<string, unknown>;
  if (
    !int64(r.id) ||
    !int64(r.merchant_id) ||
    !int64(r.order_id) ||
    r.refund_no !== intent.body.external_no ||
    r.amount !== fundsAmountCents212(intent.body.amount) ||
    !int64(r.amount) ||
    !int64(r.merchant_amount, true) ||
    !int64(r.fee_amount, true) ||
    BigInt(r.merchant_amount) + BigInt(r.fee_amount) !== BigInt(r.amount) ||
    (owner.merchant_id !== undefined && r.merchant_id !== owner.merchant_id) ||
    (owner.order_id !== undefined && r.order_id !== owner.order_id) ||
    ![
      "succeeded",
      "failed",
      "pending",
      "unknown",
      "processing",
      "reserved",
      "submitted",
    ].includes(String(r.status))
  )
    throw new Error(problem);
  // HTTP202 means the operation still needs a verified original lookup. A
  // terminal-looking snapshot cannot override that transport-level outcome.
  return {
    id: r.id,
    status: httpStatus === 202 ? "unknown" : String(r.status),
  };
}
export function verifiedWorkflow212(
  raw: unknown,
  intent: FundsIntent212,
  merchant: string,
): { id: string; status: string } {
  if (!raw || typeof raw !== "object") throw new Error("原申请响应无法核对");
  const r = raw as Record<string, unknown>;
  if (
    !/^[1-9]\d{0,18}$/.test(String(r.id)) ||
    r.kind !== intent.kind ||
    r.external_no !== intent.body.external_no ||
    r.merchant_id !== merchant ||
    r.amount_cents !== fundsAmountCents212(intent.body.amount) ||
    !["pending", "approved", "rejected"].includes(String(r.status))
  )
    throw new Error("原申请与已保存的金额或付款主体不一致，请保留原请求核对");
  if (
    intent.kind === "internal-transfers" &&
    r.recipient_id !== intent.body.recipient_id
  )
    throw new Error("原申请收款商户不一致");
  if (intent.kind === "settlements") {
    const recipient = r.settlement_recipient as
      Record<string, unknown> | undefined;
    const terms = r.settlement as Record<string, unknown> | undefined;
    if (
      recipient?.config_id !== intent.body.recipient_config_id ||
      terms?.fee_amount !== intent.body.expected_fee_cents ||
      terms?.net_amount !== intent.body.expected_net_cents
    )
      throw new Error("原申请费用或收款版本不一致");
  }
  return { id: String(r.id), status: String(r.status) };
}
