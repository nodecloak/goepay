export interface SettlementBatchMember {
  recipient?: {
    state: string;
    config_id: string;
    type: string;
    app_id: string;
    account_masked: string;
    name_masked: string;
  };
  request_id: string;
  uid: string;
  gross_amount_cents: string;
  fee_amount_cents: string;
  net_amount_cents: string;
  position: number;
  type: string;
  status: string;
  funding_state: string;
  payout_state: string;
  transfer_id: string;
}
export interface SettlementBatch {
  id: string;
  batch: string;
  external_no: string;
  allmoney: string;
  count: number;
  time: string;
  status: "active" | "archived" | "completed";
  all_paid: boolean;
  items: SettlementBatchMember[];
}
export interface SettlementBatchList {
  items: SettlementBatch[];
  total: number;
  next_before_id: string;
}
export function settlementBatchBody(external: string, rawIDs: string) {
  const ids = rawIDs.trim().split(/[\s,，]+/);
  if (
    !external ||
    external.length > 128 ||
    external.trim() !== external ||
    /[\x00-\x1f\x7f]/.test(external) ||
    ids.length < 1 ||
    ids.length > 100 ||
    ids.some(
      (id) => !/^[1-9]\d{0,18}$/.test(id) || BigInt(id) > 9223372036854775807n,
    ) ||
    new Set(ids).size !== ids.length
  )
    throw new Error("请输入不重复的原结算申请编号，最多 100 条");
  return { external_no: external, request_ids: ids };
}
export const settlementBatchStatus = (status: string) =>
  ({ active: "待完成", archived: "已归档", completed: "已完成" })[status] ||
  "状态待核实";
