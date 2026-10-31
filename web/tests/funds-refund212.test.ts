import { expect, it } from "vitest";
import { verifiedRefund212 } from "../src/funds-intent212";

const intent = { body: { amount: "2.50", external_no: "original212" } };
const owner = { merchant_id: "1000", order_id: "91" };
const receipt = {
  id: "77",
  merchant_id: "1000",
  order_id: "91",
  refund_no: "original212",
  amount: "250",
  merchant_amount: "248",
  fee_amount: "2",
  status: "succeeded",
};
it("accepts a complete original refund receipt with exact amounts and known ownership", () => {
  expect(verifiedRefund212(receipt, intent, owner, 200)).toEqual({
    id: "77",
    status: "succeeded",
  });
});
it.each(["succeeded", "failed"])(
  "keeps HTTP202 unverified despite a complete %s snapshot",
  (status) => {
    expect(
      verifiedRefund212({ ...receipt, status }, intent, owner, 202),
    ).toEqual({ id: "77", status: "unknown" });
  },
);
it.each([
  { id: undefined },
  { id: 77 },
  { id: "9223372036854775808" },
  { merchant_id: undefined },
  { merchant_id: "1002" },
  { order_id: undefined },
  { order_id: "92" },
  { refund_no: undefined },
  { refund_no: "replacement" },
  { amount: "251" },
  { amount: 250 },
  { merchant_amount: undefined },
  { fee_amount: "3" },
  { status: "not-a-refund-status" },
])(
  "rejects an incomplete, mismatched or unsafe original refund projection: %j",
  (change) => {
    expect(() =>
      verifiedRefund212({ ...receipt, ...change }, intent, owner, 200),
    ).toThrow(/原退款/);
  },
);
it("binds only ownership facts the original order actually exposed", () => {
  expect(verifiedRefund212(receipt, intent, {}, 200)).toEqual({
    id: "77",
    status: "succeeded",
  });
  expect(() =>
    verifiedRefund212(receipt, intent, { merchant_id: "broken" }, 200),
  ).toThrow(/原退款/);
});
it("compares large cents exactly without JavaScript number rounding", () => {
  const large = {
    ...receipt,
    amount: "9007199254740993",
    merchant_amount: "9007199254740992",
    fee_amount: "1",
  };
  const original = {
    body: { amount: "90071992547409.93", external_no: "original212" },
  };
  expect(verifiedRefund212(large, original, owner, 200)).toEqual({
    id: "77",
    status: "succeeded",
  });
  expect(() =>
    verifiedRefund212(
      { ...large, amount: "9007199254740992" },
      original,
      owner,
      200,
    ),
  ).toThrow(/原退款/);
});
