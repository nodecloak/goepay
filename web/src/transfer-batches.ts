import { bankChannelProduct, transferPolicyError } from "./transfers";
import bankCodes from "./data/wechat-bank-codes.json";
export interface BatchChannel {
  id: string;
  types: string[];
  bank_product?: string;
  bank_code_required?: boolean;
}
export interface BatchPolicy {
  channels: BatchChannel[];
  minimum_money?: string;
  maximum_money?: string;
  default_remark?: string;
  default_title?: string;
  daily_recipient_limit?: number;
}
export interface BatchDraftRow {
  channel_id: string;
  type: string;
  account: string;
  name: string;
  amount: string;
  remark: string;
  bank_code: string;
}
export const emptyBatchRow = (): BatchDraftRow => ({
  channel_id: "",
  type: "",
  account: "",
  name: "",
  amount: "",
  remark: "",
  bank_code: "",
});
export const batchChannelTypes = (channel?: BatchChannel) =>
  channel?.types.filter(
    (type) =>
      ["alipay", "wxpay"].includes(type) ||
      (type === "bank" && !!bankChannelProduct(channel)),
  ) || [];
export function buildBatchItems(
  rows: BatchDraftRow[],
  policy: BatchPolicy,
): Record<string, string>[] {
  if (rows.length < 1 || rows.length > 100)
    throw new Error("每批须为 1–100 行");
  return rows.map((row, index) => {
    const fail = (message: string) => {
      throw new Error(`第 ${index + 1} 行：${message}`);
    };
    const channel = policy.channels.find((c) => c.id === row.channel_id);
    if (!channel || !batchChannelTypes(channel).includes(row.type))
      fail("请选择已确认能力的通道和付款方式");
    const amountError = transferPolicyError(
      row.amount,
      policy.minimum_money,
      policy.maximum_money,
    );
    if (amountError) fail(amountError);
    if (!row.account.trim()) fail("请填写收款账号");
    const remark =
      row.remark.trim() ||
      (row.type === "alipay" ? policy.default_title : policy.default_remark) ||
      "";
    if (row.type === "wxpay" && !remark.trim())
      fail("微信付款必须填写付款说明");
    const item: Record<string, string> = {
      channel_id: row.channel_id,
      type: row.type,
      account: row.account.trim(),
      name: row.name.trim(),
      amount: row.amount,
      remark,
    };
    if (row.type === "bank") {
      const product = bankChannelProduct(channel);
      if (
        !product ||
        !row.name.trim() ||
        !/^\d{12,32}$/.test(row.account.trim())
      )
        fail("银行卡付款须确认产品能力、真实姓名和 12–32 位银行卡号");
      if (product === "wxpay_v2") {
        if (!Object.values(bankCodes).includes(row.bank_code))
          fail("请选择开户银行");
        item.bank_code = row.bank_code;
      }
    }
    return item;
  });
}
export interface TransferBatch {
  id: string;
  merchant_id: string;
  external_no: string;
  item_count: number;
  amount: string;
  cost_amount: string;
  fee_amount: string;
  created_at?: string;
  state?: string;
  all_succeeded: boolean;
  summary: Partial<
    Record<
      | "unsent"
      | "processing"
      | "unknown"
      | "succeeded"
      | "failed"
      | "cancelled"
      | "reversed"
      | "needs_review",
      number
    >
  >;
  items: {
    position: number;
    send_state: string;
    transfer: Record<string, unknown>;
  }[];
}
export const batchSummaryLabels = {
  unsent: "尚未发送",
  processing: "处理中",
  unknown: "结果未知",
  succeeded: "已成功",
  failed: "已失败",
  cancelled: "未发送已取消",
  reversed: "已退票",
  needs_review: "需人工核对",
};
export function batchCanExecute(
  batch: { summary: TransferBatch["summary"] },
  writesEnabled: boolean,
) {
  return writesEnabled === true && Number(batch.summary.unsent) > 0;
}
export function batchOutcome(
  batch: Pick<TransferBatch, "item_count" | "all_succeeded" | "summary">,
): string {
  const pending = [
    "unsent",
    "processing",
    "unknown",
    "failed",
    "cancelled",
    "reversed",
    "needs_review",
  ] as const;
  if (
    batch.all_succeeded === true &&
    batch.summary.succeeded === batch.item_count &&
    batch.item_count > 0 &&
    pending.every((key) => !batch.summary[key])
  )
    return "服务端已核实当前全部项目成功；银行卡后续仍可能退票，请继续核对生命周期。";
  return "此批次不能视为全部成功。请逐项核对状态；结果未知或已发送项只查询原记录，不重新付款。";
}
