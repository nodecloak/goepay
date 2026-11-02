import { expect, test } from "vitest";
import { payoutCapabilityReason } from "../src/settlement-payout";

test("missing payout permission identifies the administrator setting without enabling execution", () => {
  const text = payoutCapabilityReason(
    "payment writes or merchant transfers disabled",
  );
  expect(text).toContain("执行开关");
  expect(text).toContain("允许代付 API");
  expect(text).not.toContain("payment writes");
});

test.each([
  "approved settlement required",
  "original dedicated gross reservation required",
  "original recipient snapshot required",
  "no matching payment application/product and permitted channel",
  "original payout already prepared; use its existing transfer",
])("payout precondition %s has readable Chinese guidance", (reason) => {
  const text = payoutCapabilityReason(reason);
  expect(text).toMatch(/[\u4e00-\u9fff]/);
  expect(text).not.toBe(reason);
});

test("empty reason stays empty and unfamiliar upstream text is not shown", () => {
  expect(payoutCapabilityReason("")).toBe("");
  expect(
    payoutCapabilityReason("unexpected provider detail: synthetic-secret"),
  ).toBe("结算出款条件未满足，请刷新原申请并核对资金、收款资料和执行权限。");
});
