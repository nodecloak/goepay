import { describe, it, expect } from "vitest";
import {
  parseOrderBusinessBatch185,
  businessBatchEligible185,
  batchBusinessLabels185,
} from "../src/order-business-batches185";
import {
  isAdminFundsAction,
  needsFundsAuthorization,
} from "../src/funds-authorization";
import actual from "../e2e/orderbatch185-http-fixture.json";
const original = {
  id: "9007199254740993",
  external_no: "fixed-request185",
  action: "query",
  reason: "local test",
  state: "prepared",
  created_at: "2026-10-26T12:00:00Z",
  items: [
    {
      position: 1,
      order_id: "9007199254740995",
      merchant_id: "9007199254740997",
      trade_no: "T185",
      state: "unsent",
    },
  ],
  replayed: false,
  fingerprint: "a".repeat(64),
  counts: {
    total: 1,
    processed: 0,
    succeeded: 0,
    failed: 0,
    unknown: 0,
    cancelled: 0,
  },
};
describe("original order business batch185", () => {
  it("counts only explicitly available notifications whose latest result is not successful", () => {
    for (const row of [
      {
        status: "paid",
        notification_state: "succeeded",
        notify_url: "https://synthetic.invalid/notify",
        can_batch_notify: true,
      },
      { status: "paid", notification_state: "none" },
      { status: "paid", notification_state: "none", can_batch_notify: false },
      { status: "paid", can_batch_notify: true },
    ])
      expect(businessBatchEligible185(row, "notify")).toBe(false);
    for (const state of ["none", "pending", "running", "dead"])
      expect(
        businessBatchEligible185(
          { status: "paid", notification_state: state, can_batch_notify: true },
          "notify",
        ),
      ).toBe(true);
  });
  it("accepts all disposed actual Go/PG prepared, completed, partial and cancelled projections", () => {
    expect(actual.schema_disposed).toBe(true);
    expect(actual.session_invalid_after_cleanup).toBe(true);
    for (const v of [
      actual.prepared,
      actual.completed,
      actual.partial,
      actual.partly_cancelled,
    ])
      expect(parseOrderBusinessBatch185(v).id).toBe(v.id);
  });
  it("reads strict full string IDs and rejects rounded IDs, unknown fields and inconsistent progress", () => {
    expect(parseOrderBusinessBatch185(original).id).toBe("9007199254740993");
    for (const v of [
      { ...original, id: 9007199254740993 },
      { ...original, extra: true },
      { ...original, counts: { ...original.counts, total: 2 } },
      { ...original, state: "completed" },
      { ...original, items: [{ ...original.items[0], state: "succeeded" }] },
    ])
      expect(() => parseOrderBusinessBatch185(v)).toThrow();
  });
  it("uses actual payment and held-income states for eligible counts", () => {
    expect(batchBusinessLabels185.refund).toBe("API退款");
    expect(
      businessBatchEligible185(
        { status: "pending", admin_status: "paid" },
        "refund",
      ),
    ).toBe(false);
    expect(
      businessBatchEligible185(
        {
          status: "paid",
          income_status: "held",
          amount: "100",
          refunded_amount: "0",
        },
        "refund",
      ),
    ).toBe(true);
    expect(
      businessBatchEligible185(
        { status: "paid", income_status: "held" },
        "freeze-income",
      ),
    ).toBe(false);
    expect(
      businessBatchEligible185(
        { status: "paid", income_status: "held" },
        "unfreeze-income",
      ),
    ).toBe(true);
    expect(
      businessBatchEligible185(
        { status: "paid", plugin: "alipayd", settle: 1 },
        "confirm-settlement",
      ),
    ).toBe(false);
  });
  it("marks the exact finite batch execution target as a mandatory financial authorization target", () => {
    expect(
      isAdminFundsAction(
        "/orders/business-batches/9007199254740993/execute",
        "POST",
      ),
    ).toBe(true);
    expect(
      needsFundsAuthorization(
        "/console/v1/admin/orders/business-batches/9007199254740993/execute",
        false,
      ),
    ).toBe(true);
    expect(
      isAdminFundsAction("/orders/business-batches/01/execute", "POST"),
    ).toBe(false);
  });
});
