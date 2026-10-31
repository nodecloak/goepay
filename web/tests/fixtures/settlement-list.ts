export function settlementListFixture() {
  const amount = "9007199254740993";
  return {
    id: "9007199254740993",
    merchant_id: "9223372036854775807",
    kind: "settlements",
    external_no: "original-settlement-149",
    status: "approved",
    amount_cents: amount,
    amount: "90071992547409.93",
    created_at: "2026-10-25T15:30:00Z",
    settlement: {
      gross_amount: amount,
      fee_amount: "3",
      net_amount: "9007199254740990",
    },
    settlement_funding: {
      state: "reserved",
      account_id: "7",
      gross_amount: amount,
      reserved_amount: amount,
    },
    settlement_recipient: {
      state: "recorded",
      config_id: "9",
      type: "alipay",
      app_id: "",
      account_masked: "***inal",
      name_masked: "***",
    },
    payout: null as null | Record<string, unknown>,
    auto: true,
  };
}
export function paidSettlementListFixture() {
  const r = settlementListFixture();
  r.settlement_funding = {
    ...r.settlement_funding,
    state: "paid",
    reserved_amount: "0",
  };
  r.payout = {
    request_id: r.id,
    merchant_id: r.merchant_id,
    channel_id: "5",
    transfer_id: "11",
    state: "succeeded",
    send_state: "claimed",
    transfer: {
      id: "11",
      transfer_no: "original-TF149",
      amount: r.settlement.net_amount,
      status: "succeeded",
      type: "alipay",
      bank_product: "",
      pay_date: "2026-10-25 23:35:00",
    },
    funding: { ...r.settlement_funding },
    recipient: { ...r.settlement_recipient },
    created_at: "2026-10-25T15:33:00Z",
  };
  return r;
}

export function waitingReceiptSettlementListFixture() {
  const row = paidSettlementListFixture();
  row.settlement_recipient = {
    ...row.settlement_recipient,
    type: "wxpay",
    app_id: "configured-wechat-app",
  };
  row.settlement_funding = {
    ...row.settlement_funding,
    state: "reserved",
    reserved_amount: row.amount_cents,
  };
  row.payout = {
    ...row.payout,
    state: "pending",
    funding: { ...row.settlement_funding },
    recipient: { ...row.settlement_recipient },
    transfer: {
      ...(row.payout!.transfer as object),
      type: "wxpay",
      status: "pending",
      pay_date: "",
    },
  };
  return { ...row, can_confirm_receipt: true };
}
