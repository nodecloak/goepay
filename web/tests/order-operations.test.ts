import { describe, it, expect } from "vitest";
import { refundNotice, operationNotice } from "../src/order-operations";
describe("verified payment outcome wording", () => {
  it("only calls a succeeded refund successful and keeps unknown or pending explicit", () => {
    expect(refundNotice({ status: "succeeded", refund_no: "R1" })).toContain(
      "退款成功",
    );
    expect(refundNotice({ status: "pending" })).toContain("处理中");
    expect(refundNotice({ status: "unknown" })).toContain("尚未核实");
    expect(refundNotice(undefined)).toContain("尚未核实");
  });
  it("does not call an unknown close response closed", () => {
    expect(operationNotice("close", null)).toContain("尚未核实");
    expect(operationNotice("query", { status: "paid" })).toContain("paid");
  });
});
import { canRetryJob } from "../src/operations";
describe("bounded operational job retries", () => {
  it("offers only the known notification and result-query retry kinds", () => {
    for (const kind of [
      "payment.notify",
      "payment.reconcile",
      "refund.reconcile",
      "transfer.reconcile",
      "transfer.bank_reconcile",
      "authorization.release.reconcile",
    ])
      expect(canRetryJob({ kind, status: "dead" })).toBe(true);
    expect(canRetryJob({ kind: "transfer.submit", status: "dead" })).toBe(
      false,
    );
    expect(canRetryJob({ kind: "payment.reconcile", status: "pending" })).toBe(
      false,
    );
    expect(
      canRetryJob({ kind: "payment.notify", status: "dead", can_retry: false }),
    ).toBe(false);
  });
});
