import { it, expect } from "vitest";
import {
  groupPaymentRatesError,
  readGroupPaymentRates,
  setGroupPaymentRate,
} from "../src/group-payment-rates";

it("keeps original retained percentages as strings and clears only the explicitly edited payment", () => {
  const source = '{"alipay":{"rate":"99.50"},"wxpay":{"rate":"100.00"}}';
  const updated = setGroupPaymentRate(source, "alipay", "");
  expect(JSON.parse(updated)).toEqual({
    alipay: { rate: "" },
    wxpay: { rate: "100.00" },
  });
  expect(readGroupPaymentRates(source).alipay.rate).toBe("99.50");
  for (const rate of ["", "0", "99.50", "100.00"])
    expect(groupPaymentRatesError(JSON.stringify({ alipay: { rate } }))).toBe(
      "",
    );
});
it("refuses malformed and unsupported rate structures without silently dropping old configuration", () => {
  for (const value of ["-1", "1e2", "99.999", "100.01"])
    expect(groupPaymentRatesError({ wxpay: { rate: value } })).not.toBe("");
  for (const invalid of [
    [],
    null,
    { alipay: { rate: 99.5 } },
    { alipay: { rate: null } },
    { alipay: { rate: "99", channel: 7 } },
    { bank: { rate: "99" } },
  ]) {
    expect(groupPaymentRatesError(invalid)).not.toBe("");
    expect(() => setGroupPaymentRate(invalid, "wxpay", "99")).toThrow();
  }
});
