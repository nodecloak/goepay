import { it, expect } from "vitest";
import {
  readPayoutDetail,
  readPayoutCapabilities,
  payoutActions,
  payoutStateLabel,
} from "../src/settlement-payout";
import { isAdminFundsAction } from "../src/funds-authorization";
export const detail = (payout: unknown = null) => ({
  request: {
    id: "12",
    merchant_id: "1000",
    status: "approved",
    amount_cents: "123",
    settlement: { gross_amount: "123", fee_amount: "3", net_amount: "120" },
    funding: {
      state: "reserved",
      account_id: "90",
      gross_amount: "123",
      reserved_amount: "123",
    },
    recipient: {
      state: "recorded",
      config_id: "42",
      type: "alipay",
      app_id: "",
      account_masked: "***1234",
      name_masked: "***",
    },
  },
  payout,
});
export const intent = (state = "unsent", send_state = "unsent") => ({
  request_id: "12",
  merchant_id: "1000",
  channel_id: "1",
  transfer_id: "90",
  state,
  send_state,
  transfer: {
    id: "90",
    transfer_no: "TF90",
    amount: "120",
    status: state === "unsent" ? "pending" : state,
  },
  funding: detail().request.funding,
  recipient: detail().request.recipient,
  created_at: "2026-10-23T00:00:00Z",
});
export const caps = () => ({
  request_id: "12",
  can_prepare: true,
  writes_enabled: true,
  channels: [{ id: "1", type: "alipay", app_id: "" }],
  reason: "",
});
it("fails closed on malformed scope, unsupported channel product and numeric money", () => {
  expect(readPayoutDetail(detail(), "12").payout).toBeNull();
  expect(() => readPayoutDetail(detail(), "13")).toThrow();
  expect(() =>
    readPayoutDetail(detail({ ...intent(), merchant_id: "999" }), "12"),
  ).toThrow();
  expect(() =>
    readPayoutDetail(
      { ...detail(), request: { ...detail().request, amount_cents: 123 } },
      "12",
    ),
  ).toThrow();
  expect(() =>
    readPayoutCapabilities(
      { ...caps(), channels: [{ id: "1", type: "bank", app_id: "" }] },
      "12",
    ),
  ).toThrow();
});
it("unknown and failed original intents can never become new prepare or execute actions; merchant can only query claimed unknown", () => {
  const c = readPayoutCapabilities(caps(), "12");
  expect(
    payoutActions(readPayoutDetail(detail(), "12"), c, true),
  ).toMatchObject({ prepare: true, execute: false, cancel: false });
  for (const state of [
    "pending",
    "unknown",
    "failed",
    "succeeded",
    "cancelled",
  ]) {
    const d = readPayoutDetail(
      detail(intent(state, state === "cancelled" ? "cancelled" : "claimed")),
      "12",
    );
    expect(payoutActions(d, c, true)).toMatchObject({
      prepare: false,
      execute: false,
      cancel: false,
    });
  }
  expect(
    payoutActions(
      readPayoutDetail(detail(intent("unknown", "claimed")), "12"),
      null,
      false,
    ),
  ).toEqual({ prepare: false, execute: false, cancel: false, query: true });
  expect(payoutStateLabel("unknown")).toContain("待核实");
  expect(payoutStateLabel("failed")).not.toContain("成功");
});
it("requires admin grants only for exact payout execution or proof-unsent cancellation", () => {
  for (const path of [
    "/settlement-payouts/12/execute",
    "/settlement-payouts/12/cancel-unsent",
  ])
    expect(isAdminFundsAction(path, "POST")).toBe(true);
  for (const path of [
    "/settlement-payouts/12/query",
    "/settlement-payouts/12/prepare",
    "/settlement-payouts/0/execute",
    "/settlement-payouts/12/execute?x=1",
  ])
    expect(isAdminFundsAction(path, "POST")).toBe(false);
});
it("rejects inconsistent immutable net/gross amounts and keeps historical missing snapshots readable without sends", () => {
  const d = detail(intent());
  expect(() =>
    readPayoutDetail(
      {
        ...d,
        payout: {
          ...intent(),
          transfer: { ...intent().transfer, amount: "119" },
        },
      },
      "12",
    ),
  ).toThrow();
  expect(() =>
    readPayoutDetail(
      {
        ...d,
        request: {
          ...d.request,
          settlement: {
            gross_amount: "123",
            fee_amount: "4",
            net_amount: "120",
          },
        },
      },
      "12",
    ),
  ).toThrow();
  const historical = readPayoutDetail(
    {
      request: {
        ...d.request,
        funding: {
          state: "legacy_unscoped",
          account_id: "0",
          gross_amount: "0",
          reserved_amount: "0",
        },
        recipient: {
          state: "missing",
          config_id: "0",
          type: "",
          app_id: "",
          account_masked: "",
          name_masked: "",
        },
        settlement: null,
      },
      payout: null,
    },
    "12",
  );
  expect(
    payoutActions(historical, readPayoutCapabilities(caps(), "12"), true),
  ).toEqual({ prepare: false, execute: false, cancel: false, query: false });
});
