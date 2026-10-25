import { it, expect } from "vitest";
import { readSettlementPreview, centsMoney } from "../src/settlement-terms";
it("explains the original settlement-mode refusal before generic funds conditions", () => {
  const denied = {
    can_apply: false,
    external_payout: false,
    settlement_access: { mode: "1", manual_allowed: false },
  };
  expect(() => readSettlementPreview(denied, "1.00")).toThrow(
    "当前结算开关不允许手动申请结算",
  );
  expect(() =>
    readSettlementPreview(
      { ...denied, settlement_access: { mode: "3", manual_allowed: "false" } },
      "1.00",
    ),
  ).toThrow("结算模式响应无效");
});
it("keeps large financial integers exact and rejects inconsistent quotes", () => {
  const value = {
    can_apply: true,
    external_payout: false,
    terms: {
      gross_amount: "123",
      fee_amount: "3",
      net_amount: "120",
      retained_cents: "10",
    },
  };
  expect(readSettlementPreview(value, "1.23")).toMatchObject({
    fee_money: "0.03",
    net_money: "1.20",
    retained_money: "0.10",
  });
  expect(centsMoney("9223372036854775807")).toBe("92233720368547758.07");
  for (const raw of [
    null,
    { ...value, can_apply: false },
    { ...value, external_payout: true },
    { ...value, terms: { ...value.terms, fee_amount: "4" } },
    { ...value, terms: { ...value.terms, net_amount: "0" } },
    { ...value, terms: { ...value.terms, gross_amount: 123 } },
  ])
    expect(() => readSettlementPreview(raw, "1.23")).toThrow();
  expect(() => readSettlementPreview(value, "1.24")).toThrow();
  for (const raw of ["1.0", "-1", "01", "9223372036854775808", 1])
    expect(() => centsMoney(raw)).toThrow();
});
