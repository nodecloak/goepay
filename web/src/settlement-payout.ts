import { centsMoney } from "./settlement-terms";
import {
  readSettlementRecipient,
  type SettlementRecipient,
} from "./settlement-recipient";
type Row = Record<string, unknown>;
export interface PayoutFunding {
  state: string;
  account_id: string;
  gross_amount: string;
  reserved_amount: string;
}
export interface SettlementPayout {
  request_id: string;
  merchant_id: string;
  channel_id: string;
  transfer_id: string;
  state:
    | "unsent"
    | "pending"
    | "unknown"
    | "succeeded"
    | "failed"
    | "cancelled"
    | "reversal_pending"
    | "reversed";
  send_state: "unsent" | "claimed" | "cancelled";
  transfer: {
    id: string;
    transfer_no: string;
    amount: string;
    status: string;
    type: string;
    bank_product: string;
  };
  funding: PayoutFunding;
  recipient: SettlementRecipient;
  created_at: string;
}
export interface PayoutDetail {
  request: {
    id: string;
    merchant_id: string;
    status: string;
    amount_cents: string;
    settlement: {
      gross_amount: string;
      fee_amount: string;
      net_amount: string;
    } | null;
    funding: PayoutFunding | null;
    recipient: SettlementRecipient | null;
  };
  payout: SettlementPayout | null;
}
export interface PayoutCapabilities {
  request_id: string;
  can_prepare: boolean;
  writes_enabled: boolean;
  channels: {
    id: string;
    type: "alipay" | "wxpay" | "bank";
    app_id: string;
    bank_product?: string;
  }[];
  reason: string;
}
function row(raw: unknown): Row {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw new Error("结算出款资料格式无效，请重新读取原申请");
  return raw as Row;
}
function id(raw: unknown): string {
  if (
    typeof raw !== "string" ||
    !/^[1-9]\d{0,18}$/.test(raw) ||
    BigInt(raw) > 9223372036854775807n
  )
    throw new Error("结算出款编号无效");
  return raw;
}
function text(raw: unknown): string {
  if (typeof raw !== "string") throw new Error("结算出款资料缺失");
  return raw;
}
function money(raw: unknown): string {
  centsMoney(raw);
  return raw as string;
}
function funding(raw: unknown): PayoutFunding {
  const v = row(raw),
    state = text(v.state);
  if (
    ![
      "reserved",
      "released",
      "paid",
      "paid_fee_held",
      "returned",
      "legacy_unscoped",
    ].includes(state)
  )
    throw new Error("结算资金状态无效");
  return {
    state,
    account_id: state === "legacy_unscoped" ? "0" : id(v.account_id),
    gross_amount: money(v.gross_amount),
    reserved_amount: money(v.reserved_amount),
  };
}
export function readSettlementPayout(
  raw: unknown,
  requestId: string,
  merchantId: string,
): SettlementPayout {
  const p = row(raw),
    t = row(p.transfer),
    state = text(p.state),
    send = text(p.send_state);
  if (
    id(p.request_id) !== requestId ||
    id(p.merchant_id) !== merchantId ||
    ![
      "unsent",
      "pending",
      "unknown",
      "succeeded",
      "failed",
      "cancelled",
      "reversal_pending",
      "reversed",
    ].includes(state) ||
    !["unsent", "claimed", "cancelled"].includes(send) ||
    (send === "unsent" && state !== "unsent") ||
    (send === "cancelled" && state !== "cancelled") ||
    (state === "unsent" && send !== "unsent")
  )
    throw new Error("结算出款归属或状态不一致，请重新查询原申请");
  const transferId = id(p.transfer_id);
  if (id(t.id) !== transferId) throw new Error("结算出款绑定编号不一致");
  const recipient = readSettlementRecipient(p.recipient),
    type = t.type === undefined ? "" : text(t.type),
    bank = t.bank_product === undefined ? "" : text(t.bank_product);
  if (
    (recipient.type === "bank" &&
      (type !== "bank" || !["wxpay_v2", "alipay_bankcard"].includes(bank))) ||
    (recipient.type !== "bank" &&
      (type === "bank" ||
        bank !== "" ||
        ["reversed", "reversal_pending"].includes(state)))
  )
    throw new Error("结算银行卡产品与原收款快照不一致");
  const created = text(p.created_at);
  if (!Number.isFinite(Date.parse(created)))
    throw new Error("结算出款时间无效");
  return {
    request_id: requestId,
    merchant_id: merchantId,
    channel_id: id(p.channel_id),
    transfer_id: transferId,
    state: state as SettlementPayout["state"],
    send_state: send as SettlementPayout["send_state"],
    transfer: {
      id: transferId,
      transfer_no: text(t.transfer_no),
      amount: money(t.amount),
      status: text(t.status),
      type,
      bank_product: bank,
    },
    funding: funding(p.funding),
    recipient,
    created_at: created,
  };
}
export function readPayoutDetail(
  raw: unknown,
  requestId: string,
): PayoutDetail {
  const d = row(raw),
    r = row(d.request);
  if (
    id(r.id) !== id(requestId) ||
    !["pending", "approved", "rejected"].includes(String(r.status))
  )
    throw new Error("结算申请归属或状态无效");
  const merchant = id(r.merchant_id),
    terms = r.settlement == null ? null : row(r.settlement);
  const result: PayoutDetail = {
    request: {
      id: requestId,
      merchant_id: merchant,
      status: String(r.status),
      amount_cents: money(r.amount_cents),
      settlement: terms
        ? {
            gross_amount: money(terms.gross_amount),
            fee_amount: money(terms.fee_amount),
            net_amount: money(terms.net_amount),
          }
        : null,
      funding: r.funding == null ? null : funding(r.funding),
      recipient:
        r.recipient == null ? null : readSettlementRecipient(r.recipient),
    },
    payout:
      d.payout === null
        ? null
        : readSettlementPayout(d.payout, requestId, merchant),
  };
  const t = result.request.settlement;
  if (
    t &&
    (t.gross_amount !== result.request.amount_cents ||
      BigInt(t.net_amount) <= 0n ||
      BigInt(t.fee_amount) + BigInt(t.net_amount) !== BigInt(t.gross_amount))
  )
    throw new Error("结算申请总额、手续费与净额不一致，请重新核对");
  if (
    result.payout &&
    (!t ||
      result.payout.transfer.amount !== t.net_amount ||
      result.payout.funding.gross_amount !== result.request.amount_cents)
  )
    throw new Error("原 TF 与结算申请金额不一致，请重新核对");
  if (result.payout && t) {
    const p = result.payout;
    if (
      (p.funding.state === "paid_fee_held" &&
        (p.recipient.type !== "bank" ||
          !["succeeded", "reversal_pending"].includes(p.state) ||
          p.funding.reserved_amount !== t.fee_amount ||
          BigInt(t.fee_amount) <= 0n)) ||
      (p.funding.state === "returned" &&
        (p.state !== "reversed" || p.funding.reserved_amount !== "0"))
    )
      throw new Error("银行卡结算费用保留或退回金额不一致");
  }
  return result;
}
export function readPayoutCapabilities(
  raw: unknown,
  requestId: string,
): PayoutCapabilities {
  const c = row(raw);
  if (
    id(c.request_id) !== requestId ||
    typeof c.can_prepare !== "boolean" ||
    typeof c.writes_enabled !== "boolean" ||
    !Array.isArray(c.channels)
  )
    throw new Error("结算出款能力读取失败");
  const channels = c.channels.map((raw) => {
    const x = row(raw),
      type = text(x.type);
    if (
      !["alipay", "wxpay", "bank"].includes(type) ||
      (type === "bank" &&
        !["wxpay_v2", "alipay_bankcard"].includes(String(x.bank_product)))
    )
      throw new Error("本流程仅支持明确可用的通道与银行产品");
    return {
      id: id(x.id),
      type: type as "alipay" | "wxpay" | "bank",
      app_id: text(x.app_id),
      ...(type === "bank" ? { bank_product: text(x.bank_product) } : {}),
    };
  });
  if (new Set(channels.map((x) => x.id)).size !== channels.length)
    throw new Error("结算通道编号重复");
  return {
    request_id: requestId,
    can_prepare: c.can_prepare,
    writes_enabled: c.writes_enabled,
    channels,
    reason: text(c.reason),
  };
}
export function payoutActions(
  d: PayoutDetail,
  c: PayoutCapabilities | null,
  admin: boolean,
) {
  const p = d.payout;
  return {
    prepare:
      admin &&
      !p &&
      d.request.status === "approved" &&
      d.request.funding?.state === "reserved" &&
      d.request.recipient?.state === "recorded" &&
      !!d.request.settlement &&
      c?.can_prepare === true &&
      c.writes_enabled === true &&
      c.channels.length > 0,
    execute:
      admin &&
      p?.send_state === "unsent" &&
      p.state === "unsent" &&
      c?.writes_enabled === true,
    cancel: admin && p?.send_state === "unsent" && p.state === "unsent",
    query:
      p?.send_state === "claimed" &&
      (["pending", "unknown", "reversal_pending"].includes(p.state) ||
        (p.transfer.type === "bank" && p.state === "succeeded")),
  };
}
export function payoutStateLabel(state: string): string {
  return (
    (
      {
        unsent: "已准备，尚未发送",
        pending: "处理中，待核实",
        unknown: "结果待核实，只查询原出款",
        succeeded: "支付机构已核实出款成功",
        failed: "出款失败，以资金归属核对释放",
        cancelled: "已取消未发送出款",
        reversal_pending: "银行退票证据待核对，资金不自动退回",
        reversed: "银行卡原出款已核实退回",
      } as Record<string, string>
    )[state] || "状态待核实"
  );
}
export function payoutFundingLabel(raw: unknown): string {
  const state = (raw as { state?: string } | null)?.state;
  return (
    (
      {
        reserved: "本申请独立冻结",
        paid: "本申请已结转出款",
        released: "本申请已释放",
        legacy_unscoped: "历史共享冻结（未接管）",
        paid_fee_held: "银行卡出款已核实，结算手续费保留待退票核对",
        returned: "本申请本金及保留费用已核实退回",
      } as Record<string, string>
    )[String(state)] || "未记录独立资金归属"
  );
}
