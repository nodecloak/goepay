import bankCodes from "./data/wechat-bank-codes.json";
export interface SettlementRecipient {
  state: "configured" | "missing" | "recorded";
  config_id: string;
  type: "" | "alipay" | "wxpay" | "bank";
  app_id: string;
  account_masked: string;
  name_masked: string;
}
export interface RecipientDraft {
  type: string;
  account: string;
  name: string;
  bank_code: string;
  app_id: string;
}
export type RecipientField = keyof RecipientDraft;
export type RecipientFieldErrors = Partial<Record<RecipientField, string>>;
const bankNames: Record<string, string> = {
  ICBC: "工商银行",
  ABC: "农业银行",
  CCB: "建设银行",
  BOC: "中国银行",
  COMM: "交通银行",
  CMB: "招商银行",
  PSBC: "邮储银行",
};
export const recipientBankOptions = [
  { value: "", label: "通道未要求银行代码时留空" },
  ...Object.entries(bankCodes).map(([bank, code]) => ({
    value: code,
    label: `${bankNames[bank] || bank}（${bank} · ${code}）`,
  })),
];
export function recipientFieldErrors(
  draft: RecipientDraft,
): RecipientFieldErrors {
  const errors: RecipientFieldErrors = {};
  const account = draft.account.trim(),
    name = draft.name.trim();
  if (!["alipay", "wxpay", "bank"].includes(draft.type))
    errors.type = "请选择收款方式";
  if (!account) errors.account = "请填写收款账号";
  else if (account.length > 128 || /[\r\n\x00]/.test(account))
    errors.account = "收款账号最多128字，不能包含换行";
  else if (draft.type === "bank" && !/^\d{8,32}$/.test(account))
    errors.account = "银行卡号应为8至32位数字";
  if (!name) errors.name = "请填写真实姓名";
  else if (name.length > 128 || /[\r\n\x00]/.test(name))
    errors.name = "真实姓名最多128字，不能包含换行";
  if (draft.type === "wxpay" && !draft.app_id.trim())
    errors.app_id = "请填写收款OpenID所属应用的AppID，可向平台管理员获取";
  else if (
    draft.app_id.length > 128 ||
    /[\r\n\x00]/.test(draft.app_id) ||
    (draft.type === "bank" && draft.app_id)
  )
    errors.app_id = "请核对收款所属AppID";
  if (
    (draft.bank_code && !/^\d{1,32}$/.test(draft.bank_code)) ||
    (draft.type !== "bank" && draft.bank_code)
  )
    errors.bank_code =
      "银行代码应为平台管理员提供的1至32位数字，仅用于银行卡收款";
  return errors;
}
// 添加商户 结算信息 is an optional first recipient checked with the rules above.
// A blank account and name mean the merchant does not use platform settlement.
const createRecipientFields = {
  type: "settlement_type",
  account: "settlement_account",
  name: "settlement_name",
  app_id: "settlement_app_id",
} as const;
const createRecipientMissing = {
  type: "请选择结算方式",
  account: "请填写结算账号",
  name: "请填写结算账号姓名",
  app_id: "微信结算请填写收款 OpenID 所属应用的 AppID",
};
const unusedCreateRecipient = (v: Record<string, unknown>) =>
  !String(v.settlement_account ?? "").trim() &&
  !String(v.settlement_name ?? "").trim();
export function createRecipientErrors(
  values: Record<string, unknown>,
): Record<string, string> {
  if (unusedCreateRecipient(values)) return {};
  const draft: RecipientDraft = {
    type: String(values.settlement_type ?? ""),
    account: String(values.settlement_account ?? ""),
    name: String(values.settlement_name ?? ""),
    bank_code: "",
    app_id:
      values.settlement_type === "wxpay"
        ? String(values.settlement_app_id ?? "")
        : "",
  };
  const found = recipientFieldErrors(draft),
    errors: Record<string, string> = {};
  for (const key of ["type", "account", "name", "app_id"] as const)
    if (found[key])
      errors[createRecipientFields[key]] = draft[key].trim()
        ? found[key]
        : createRecipientMissing[key];
  return errors;
}
// The create request carries the recipient only when it is used, and an AppID
// only for 微信, matching what the server records.
export function omitUnusedCreateRecipient(payload: Record<string, unknown>) {
  if (unusedCreateRecipient(payload))
    for (const name of Object.values(createRecipientFields))
      delete payload[name];
  else if (payload.settlement_type !== "wxpay")
    delete payload.settlement_app_id;
}
export class RecipientValidationError extends Error {
  constructor(public readonly fieldErrors: RecipientFieldErrors) {
    super(Object.values(fieldErrors)[0] || "请核对收款资料");
    this.name = "RecipientValidationError";
  }
}
export function readSettlementRecipient(raw: unknown): SettlementRecipient {
  if (!raw || typeof raw !== "object")
    throw new Error("收款配置读取失败，请重新核对");
  const r = raw as Record<string, unknown>;
  if (
    "account" in r ||
    "name" in r ||
    !["configured", "recorded", "missing"].includes(String(r.state)) ||
    typeof r.config_id !== "string" ||
    typeof r.type !== "string" ||
    typeof r.app_id !== "string" ||
    typeof r.account_masked !== "string" ||
    typeof r.name_masked !== "string"
  )
    throw new Error("收款配置格式无效");
  if (r.state === "missing") {
    if (r.config_id !== "0" || r.type !== "")
      throw new Error("缺失收款配置格式无效");
  } else if (
    !/^[1-9]\d{0,18}$/.test(r.config_id) ||
    BigInt(r.config_id) > 9223372036854775807n ||
    !["alipay", "wxpay", "bank"].includes(r.type) ||
    !r.account_masked.startsWith("***") ||
    r.name_masked !== "***" ||
    (r.type === "wxpay" && !r.app_id)
  )
    throw new Error("收款配置版本或脱敏资料无效");
  return {
    state: r.state as SettlementRecipient["state"],
    config_id: r.config_id,
    type: r.type as SettlementRecipient["type"],
    app_id: r.app_id,
    account_masked: r.account_masked,
    name_masked: r.name_masked,
  };
}
export function recipientConfigurationBody(
  draft: RecipientDraft,
  current: SettlementRecipient,
  merchant?: string,
): Record<string, string> {
  const account = draft.account.trim(),
    name = draft.name.trim();
  const errors = recipientFieldErrors(draft);
  if (Object.keys(errors).length) throw new RecipientValidationError(errors);
  const body: Record<string, string> = {
    type: draft.type,
    account,
    name,
    bank_code: draft.bank_code,
    app_id: draft.app_id,
    expected_config_id: current.config_id,
  };
  if (merchant) {
    if (!/^[1-9]\d*$/.test(merchant)) throw new Error("请先填写有效商户 ID");
    body.merchant_id = merchant;
  }
  return body;
}
export function settlementSnapshotLabel(raw: unknown): string {
  try {
    const v = readSettlementRecipient(raw);
    if (v.state === "recorded")
      return `${{ alipay: "支付宝", wxpay: "微信", bank: "银行卡", "": "" }[v.type]} ${v.account_masked} · 版本 ${v.config_id}`;
  } catch {}
  return "未记录收款快照，不能出款";
}
