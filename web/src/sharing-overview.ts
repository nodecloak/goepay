export const sharingID = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[1-9]\d{0,18}$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
export const sharingUnsigned = (v: unknown): v is string =>
  typeof v === "string" &&
  /^(0|[1-9]\d{0,18})$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
export function sharingPageTotal(raw: unknown): number {
  if (typeof raw === "number" && Number.isSafeInteger(raw) && raw >= 0)
    return raw;
  if (sharingUnsigned(raw) && BigInt(raw) <= BigInt(Number.MAX_SAFE_INTEGER))
    return Number(raw);
  throw Error("分账列表总数无效");
}
export function sharingMinorInput(raw: string, label = "金额"): string {
  if (!/^(0|[1-9]\d{0,9})(?:\.\d{1,2})?$/.test(raw))
    throw Error(label + "填写不正确，最多两位小数");
  const [whole, fraction = ""] = raw.split(".");
  const value = BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0"));
  if (value <= 0n) throw Error(label + "必须大于0");
  return value.toString();
}
const text = (v: unknown): v is string =>
  typeof v === "string" && v.length <= 256 && !/[\x00-\x1f\x7f]/.test(v);
export interface AdminSharingRule {
  rule: {
    id: string;
    merchant_id: string;
    channel_id: string;
    revision: string;
    enabled: boolean;
    provider: string;
    minimum_amount: string;
    methods: string[];
    items: {
      receiver_id: string;
      mode: string;
      rate_bps: string;
      fixed_amount: string;
    }[];
  };
  receivers: {
    id: string;
    account_masked: string;
    name_masked: string;
    state: string;
  }[];
}
export function adminSharingRule(raw: unknown): AdminSharingRule {
  const v = raw as AdminSharingRule,
    r = v?.rule;
  if (
    !r ||
    !sharingID(r.id) ||
    !sharingID(r.merchant_id) ||
    !sharingID(r.channel_id) ||
    !sharingID(r.revision) ||
    typeof r.enabled !== "boolean" ||
    !["wxpay", "alipay"].includes(r.provider) ||
    !sharingUnsigned(r.minimum_amount) ||
    !Array.isArray(r.methods) ||
    !r.methods.every(text) ||
    !Array.isArray(r.items) ||
    !r.items.length ||
    r.items.length > 50 ||
    !Array.isArray(v.receivers) ||
    v.receivers.length > 50
  )
    throw Error("分账规则总览响应格式错误");
  const receiverIDs = new Set<string>();
  for (const receiver of v.receivers) {
    if (
      !sharingID(receiver.id) ||
      receiverIDs.has(receiver.id) ||
      !text(receiver.account_masked) ||
      !text(receiver.name_masked) ||
      !text(receiver.state)
    )
      throw Error("分账接收方总览响应格式错误");
    receiverIDs.add(receiver.id);
  }
  for (const item of r.items) {
    if (
      !receiverIDs.has(item.receiver_id) ||
      !["rate", "fixed"].includes(item.mode) ||
      !sharingUnsigned(item.rate_bps) ||
      !sharingUnsigned(item.fixed_amount) ||
      BigInt(item.rate_bps) > 10000n
    )
      throw Error("分账比例总览响应格式错误");
  }
  return v;
}
