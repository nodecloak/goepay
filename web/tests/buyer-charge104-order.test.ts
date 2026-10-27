import { expect, it } from "vitest";
import { adminOrderActions, merchantOrderActions } from "../src/order-actions";
import { businessBatchEligible185 } from "../src/order-business-batches185";
import { canRefundListOrder, consoleValue } from "../src/pro-console-fields";

const row = {
  trade_no: "T104",
  status: "paid",
  amount: "100",
  realmoney: "106",
  charge_verified104: true,
  refunded_amount: "100",
  merchant_actions: { refund: true },
};
it("keeps actual buyer fee refundable after the original merchant amount has returned", () => {
  expect(merchantOrderActions(row).some((a) => a.id === "refund")).toBe(true);
  expect(adminOrderActions(row).some((a) => a.id === "manual-refund")).toBe(
    true,
  );
  expect(businessBatchEligible185(row, "refund")).toBe(true);
  expect(canRefundListOrder(row)).toBe(true);
  expect(consoleValue("realmoney", "106", true)).toBe("1.06");
});
it("never derives a payment or refund capability from an unconfirmed actual source", () => {
  const unknown = {
    ...row,
    charge_verified104: false,
    realmoney: "",
    refunded_amount: "0",
  };
  expect(merchantOrderActions(unknown).some((a) => a.id === "refund")).toBe(
    false,
  );
  expect(businessBatchEligible185(unknown, "refund")).toBe(false);
  expect(canRefundListOrder(unknown)).toBe(false);
  const full = { ...row, refunded_amount: "106" };
  expect(businessBatchEligible185(full, "refund")).toBe(false);
  expect(canRefundListOrder(full)).toBe(false);
});
