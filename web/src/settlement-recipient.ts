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
  if (
    !["alipay", "wxpay", "bank"].includes(draft.type) ||
    !account ||
    !name ||
    account.length > 128 ||
    name.length > 128 ||
    draft.app_id.length > 128 ||
    /[\r\n\x00]/.test(account + name + draft.app_id + draft.bank_code) ||
    (draft.type === "wxpay" && !draft.app_id) ||
    (draft.type === "bank" &&
      (!/^\d{8,32}$/.test(account) ||
        draft.app_id ||
        (draft.bank_code && !/^\d{1,32}$/.test(draft.bank_code)))) ||
    (draft.type !== "bank" && draft.bank_code)
  )
    throw new Error("请填写完整且有效的收款资料");
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
