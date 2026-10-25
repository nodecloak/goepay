import { describe, it, expect } from "vitest";
import { readFundsAvailability } from "../src/funds-availability";
describe("server funds snapshots fail closed", () => {
  it("preserves exact balances and distinguishes restricted gross funds from ledger balances", () => {
    expect(
      readFundsAvailability({
        settle_type: 1,
        available_money: "9.90",
        ledger_available_money: "10.90",
        today_blocked_money: "1.00",
      }),
    ).toEqual({
      settle_type: 1,
      available_money: "9.90",
      ledger_available_money: "10.90",
      today_blocked_money: "1.00",
    });
    expect(
      readFundsAvailability({
        settle_type: 0,
        available_money: "92233720368547758.07",
        ledger_available_money: "92233720368547758.07",
        today_blocked_money: "0.00",
      }).available_money,
    ).toBe("92233720368547758.07");
  });
  it("rejects absent, malformed or contradictory policy instead of inventing zero balances", () => {
    const base = {
      settle_type: 1,
      available_money: "0.00",
      ledger_available_money: "1.00",
      today_blocked_money: "1.00",
    };
    for (const data of [
      null,
      {},
      { ...base, settle_type: "1" },
      { ...base, settle_type: 2 },
      { ...base, available_money: 0 },
      { ...base, ledger_available_money: "0.00" },
      { ...base, today_blocked_money: "-1.00" },
      { ...base, settle_type: 0 },
      { ...base, available_money: "92233720368547758.08" },
    ])
      expect(() => readFundsAvailability(data)).toThrow("资金");
  });
});
