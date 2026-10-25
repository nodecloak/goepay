import { it, expect } from "vitest";
import { readHeldRefundFunding } from "../src/refund-funding";

it("requires conserved exact integer funding and the same original hold before confirmation", () => {
  const view = {
    hold_id: "9",
    funding_mode: "income_hold",
    held_amount: "400",
    available_amount: "10",
    fee_amount: "1",
    held_remaining: "500",
  };
  expect(readHeldRefundFunding(view, "4.11")).toEqual(view);
  for (const bad of [
    { ...view, held_amount: "401" },
    { ...view, held_remaining: "399" },
    { ...view, hold_id: "0" },
    { ...view, held_amount: 400 },
    { ...view, held_amount: "0400" },
    { ...view, fee_amount: "-1" },
    { ...view, funding_mode: "available" },
  ])
    expect(() => readHeldRefundFunding(bad, "4.11")).toThrow();
});
