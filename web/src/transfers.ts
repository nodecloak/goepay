import { displayValue, validAmount } from "./api";
export const transferTypeLabel: Record<string, string> = {
  alipay: "支付宝钱包",
  wxpay: "微信零钱",
  bank: "银行卡",
};
export function transferReadActions(value: Record<string, unknown> | null) {
  const unsent =
    value &&
    [value.status, value.send_state, value.settlement_send_state].some(
      (state) => state === "unsent" || state === "cancelled",
    );
  return {
    query: !!value && !unsent,
    proof: !!value && !unsent && value.type === "alipay",
  };
}
export type BankProduct = "alipay_bankcard" | "wxpay_v2";
export const bankProductLabel: Record<BankProduct, string> = {
  alipay_bankcard: "支付宝银行卡（对私）",
  wxpay_v2: "微信付款到银行卡",
};
export function bankChannelProduct(channel: unknown): BankProduct | undefined {
  if (!channel || typeof channel !== "object") return;
  const value = channel as Record<string, unknown>;
  if (!Array.isArray(value.types) || !value.types.includes("bank")) return;
  if (
    value.bank_product === "alipay_bankcard" &&
    value.bank_code_required === false
  )
    return "alipay_bankcard";
  if (value.bank_product === "wxpay_v2" && value.bank_code_required === true)
    return "wxpay_v2";
}
export function cents(value: string): bigint | undefined {
  if (!/^(0|[1-9]\d{0,13})(\.\d{1,2})?$/.test(value)) return;
  const [whole, fraction = ""] = value.split(".");
  return BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0"));
}
export function transferPolicyError(
  amount: string,
  minimum?: string,
  maximum?: string,
): string {
  if (!validAmount(amount)) return "请输入大于 0 且最多两位小数的付款金额";
  const value = cents(amount)!,
    min = minimum === undefined ? undefined : cents(minimum),
    max = maximum === undefined ? undefined : cents(maximum);
  if (min !== undefined && value < min) return `付款金额不得低于 ${minimum} 元`;
  if (max !== undefined && max > 0n && value > max)
    return `付款金额不得高于 ${maximum} 元`;
  return "";
}
export function transferNotice(
  data: Record<string, unknown> | null,
  status: number,
): string {
  if (data?.status === "unsent" || data?.send_state === "unsent")
    return "原付款绑定尚未发送；结算来源请在原结算申请中核对、发送或取消，不应重新申请付款。";
  if (data?.status === "cancelled" || data?.send_state === "cancelled")
    return "原付款已证明未发送并取消。请核对对应资金释放记录，不表示支付机构付款成功。";
  if (data?.status === "reversed" || data?.bank_lifecycle === "reversed")
    return "银行退票已核实。请按已返还本金、手续费和退票记账流水核对资金，不应视为收款成功。";
  if (
    data?.status === "reversal_pending" ||
    data?.bank_lifecycle === "reversal_pending"
  )
    return "银行退票或手续费结果尚未核实，正在等待对账。历史账本成功不代表当前银行最终到账，不能据此再次付款或假定资金已返还。";
  if (status === 202 || data?.status === "unknown")
    return data?.type === "bank"
      ? "银行付款结果尚未核实，只能查询原付款记录；未经核实的退票不会自动入账，请勿重新付款。"
      : "付款结果尚未核实，预占资金将保留。请使用原业务请求号查询，勿重新发起新的付款。";
  if (data?.status === "succeeded")
    return data.type === "bank"
      ? "通道已核实当前银行付款状态。银行仍可能后续退票，请核对银行生命周期、服务费暂存及后续查询。"
      : "通道已确认付款成功，请核对付款记录和资金明细。";
  if (data?.status === "failed")
    return "通道已确认付款失败，请核对资金预占释放记录。";
  return `付款状态：${displayValue(data?.status)}。请继续查询核实最终结果。`;
}
