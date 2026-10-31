import { expect, it } from "vitest";
import { adminOrderActions, merchantOrderActions } from "../src/order-actions";
const full = {
  trade_no: "T212",
  status: "paid",
  amount: "1000",
  refunded_amount: "1000",
  income_status: "held",
  merchant_actions: { refund: false },
};
it("exposes only original-refund recovery for a verified local intent even when new refund eligibility is gone", () => {
  const actions = merchantOrderActions({
    ...full,
    local_refund_recovery212: true,
  });
  expect(actions.map((a) => a.id)).toContain("recover-refund");
  expect(actions.map((a) => a.id)).not.toContain("refund");
});
it.each([undefined, false, "true"])(
  "never enables refund recovery without an actual verified local intent flag: %j",
  (flag) => {
    expect(
      merchantOrderActions({ ...full, local_refund_recovery212: flag }).map(
        (a) => a.id,
      ),
    ).not.toContain("recover-refund");
  },
);

it.each([
  { status: "paid", income_status: "held", charge_verified104: false },
  { status: "pending", admin_status: "pending" },
  { status: "paid", admin_archived: true },
])(
  "keeps administrator recovery independent of changed new-refund eligibility: %j",
  (changed) => {
    const ordinary = adminOrderActions({ ...full, ...changed });
    const restored = adminOrderActions({
      ...full,
      ...changed,
      local_refund_recovery212: true,
    });
    expect(restored[0]).toMatchObject({
      id: "recover-refund",
      label: "核对原退款",
    });
    expect(restored.slice(1)).toEqual(ordinary);
  },
);

it.each([undefined, false, "true"])(
  "never offers admin recovery without the verified local flag: %j",
  (flag) => {
    expect(
      adminOrderActions({ ...full, local_refund_recovery212: flag }).map(
        (a) => a.id,
      ),
    ).not.toContain("recover-refund");
  },
);
