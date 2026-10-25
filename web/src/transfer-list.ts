import type { Area } from "./menu";
import { centsMoney } from "./settlement-terms";
import { beijingTimestamp } from "./console-datetime";

export function originalTransferColumns(area: Area) {
  return [
    { key: "number", label: "交易号" },
    ...(area === "admin" ? [{ key: "merchantID", label: "商户号" }] : []),
    { key: "method", label: "付款方式" },
    { key: "account", label: "付款账号" },
    { key: "amount", label: "付款金额" },
    { key: "createdAt", label: "提交时间" },
    { key: "status", label: "状态" },
  ];
}
export function hasOriginalTransferProjection(
  items: Record<string, unknown>[],
) {
  return items.every((row) =>
    [
      "created_at",
      "type",
      "account_masked",
      "name_masked",
      "provider_transfer_no",
      "pay_date",
    ].every((key) => Object.hasOwn(row, key)),
  );
}
export function readTransferListRow(value: unknown) {
  try {
    if (!value || typeof value !== "object" || Array.isArray(value))
      throw Error();
    const raw = value as Record<string, unknown>;
    const id = (key: string) => {
      const v = raw[key];
      if (
        typeof v !== "string" ||
        !/^([1-9]\d{0,18})$/.test(v) ||
        BigInt(v) > 9223372036854775807n
      )
        throw Error();
      return v;
    };
    const text = (key: string) => {
      const v = raw[key];
      if (
        typeof v !== "string" ||
        v.length > 512 ||
        /[\u0000-\u001f\u007f]/.test(v)
      )
        throw Error();
      return v;
    };
    const identifier = id("id"),
      merchantID = id("merchant_id"),
      amount = centsMoney(raw.amount),
      fee = centsMoney(raw.fee_amount),
      cost = centsMoney(raw.cost_amount);
    if (
      BigInt(raw.amount as string) <= 0n ||
      BigInt(raw.amount as string) + BigInt(raw.fee_amount as string) !==
        BigInt(raw.cost_amount as string)
    )
      throw Error();
    const type = text("type"),
      account = text("account_masked"),
      name = text("name_masked");
    if (type === "") {
      if (account !== "" || name !== "") throw Error();
    } else if (
      !["alipay", "wxpay", "bank"].includes(type) ||
      !(
        account === "***" ||
        (account.startsWith("***") && Array.from(account).length === 7)
      ) ||
      name !== "***"
    )
      throw Error();
    const labels: Record<string, string> = {
      pending: "正在处理",
      unknown: "结果待核对",
      succeeded: "转账成功",
      failed: "转账失败",
      unsent: "待处理",
      cancelled: "已取消",
      reversed: "已退回",
      reversal_pending: "退回处理中",
    };
    let state = text("status");
    if (!Object.hasOwn(labels, state)) throw Error();
    if (["reversed", "reversal_pending"].includes(String(raw.bank_lifecycle)))
      state = String(raw.bank_lifecycle);
    const recorded = beijingTimestamp(raw.created_at),
      createdAt =
        typeof recorded === "string" &&
        recorded !== raw.created_at &&
        /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(recorded)
          ? recorded
          : "未记录";
    return {
      raw,
      id: identifier,
      merchantID,
      number: text("biz_no") || identifier,
      externalNo: text("transfer_no"),
      providerNo: text("provider_transfer_no"),
      method:
        (
          { alipay: "支付宝", wxpay: "微信", bank: "银行卡" } as Record<
            string,
            string
          >
        )[type] || "历史未记录",
      account: account || "历史未记录",
      name: name || "历史未记录",
      amount,
      fee,
      cost,
      createdAt,
      state,
      status: labels[state]!,
      tone:
        state === "succeeded"
          ? "success"
          : state === "failed"
            ? "danger"
            : ["reversed", "cancelled"].includes(state)
              ? "neutral"
              : "processing",
    };
  } catch {
    throw Error("付款记录数据无法核对，请刷新后重试");
  }
}
