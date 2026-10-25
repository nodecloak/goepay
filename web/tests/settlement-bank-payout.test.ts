import { it, expect } from "vitest";
import {
  readPayoutDetail,
  readPayoutCapabilities,
  payoutActions,
  payoutFundingLabel,
} from "../src/settlement-payout";

const recipient = {
  state: "recorded",
  config_id: "42",
  type: "bank",
  app_id: "",
  account_masked: "***0000",
  name_masked: "***",
};
function bankDetail(state = "succeeded", fundingState = "paid_fee_held") {
  const funding = {
    state: fundingState,
    account_id: "90",
    gross_amount: "1000",
    reserved_amount: state === "reversed" ? "0" : "30",
  };
  return {
    request: {
      id: "12",
      merchant_id: "1000",
      status: "approved",
      amount_cents: "1000",
      settlement: { gross_amount: "1000", fee_amount: "30", net_amount: "970" },
      funding,
      recipient,
    },
    payout: {
      request_id: "12",
      merchant_id: "1000",
      channel_id: "7",
      transfer_id: "90",
      state,
      send_state: "claimed",
      transfer: {
        id: "90",
        transfer_no: "ST12",
        amount: "970",
        status: state,
        type: "bank",
        bank_product: "alipay_bankcard",
      },
      funding,
      recipient,
      created_at: "2026-10-23T00:00:00Z",
    },
  };
}
it("keeps succeeded bank intents queryable for late returns and fee held, never resendable", () => {
  for (const state of ["succeeded", "reversal_pending"]) {
    const d = readPayoutDetail(bankDetail(state), "12");
    expect(payoutActions(d, null, false)).toEqual({
      prepare: false,
      execute: false,
      cancel: false,
      query: true,
    });
    expect(payoutFundingLabel(d.payout?.funding)).toContain("手续费保留");
  }
  const d = readPayoutDetail(bankDetail("reversed", "returned"), "12");
  expect(payoutActions(d, null, true)).toEqual({
    prepare: false,
    execute: false,
    cancel: false,
    query: false,
  });
  expect(payoutFundingLabel(d.payout?.funding)).toContain("退回");
});
it("accepts only explicit known bank products and rejects missing product or a wrong held fee", () => {
  const c = {
    request_id: "12",
    can_prepare: true,
    writes_enabled: true,
    reason: "",
    channels: [
      {
        id: "7",
        type: "bank",
        app_id: "local-app",
        bank_product: "alipay_bankcard",
      },
    ],
  };
  expect(readPayoutCapabilities(c, "12").channels).toHaveLength(1);
  expect(() =>
    readPayoutCapabilities(
      { ...c, channels: [{ ...c.channels[0], bank_product: "" }] },
      "12",
    ),
  ).toThrow();
  const wrong = bankDetail();
  wrong.payout.funding.reserved_amount = "29";
  expect(() => readPayoutDetail(wrong, "12")).toThrow();
  const missing = bankDetail();
  missing.payout.transfer.bank_product = "";
  expect(() => readPayoutDetail(missing, "12")).toThrow();
});
