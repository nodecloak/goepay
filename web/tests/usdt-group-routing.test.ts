import { expect, it } from "vitest";
import {
  CATALOG_PAY_TYPES,
  FILTER_PAY_TYPES,
  payTypeLabel,
} from "../src/pay-types";
import { readGroupPaymentRates } from "../src/group-payment-rates";
import {
  readGroupRoutingCatalog,
  setGroupPaymentRoute,
} from "../src/group-routing";
it("includes USDT in payment methods and accepts explicit group channel and retained rate", () => {
  expect(CATALOG_PAY_TYPES).toContain("usdt");
  expect(payTypeLabel("usdt")).toBe("USDT");
  expect(FILTER_PAY_TYPES).not.toContain("usdt");
  expect(
    readGroupRoutingCatalog({
      available: true,
      items: [{ id: "99", type: "usdt", methods: ["native"] }],
    }),
  ).toHaveLength(1);
  expect(
    readGroupPaymentRates({
      usdt: { rate: "99.25", channel: "99", type: "channel" },
    }),
  ).toMatchObject({ usdt: { rate: "99.25" } });
});
it("rejects USDT group routing modes the chain admission does not support", () => {
  for (const channel of ["-1", "-2", "-3", "-4"])
    expect(() =>
      readGroupPaymentRates({
        usdt: { channel, type: channel === "-3" ? "roll" : "channel" },
      }),
    ).toThrow();
  expect(() =>
    readGroupPaymentRates({ usdt: { channel: "12", type: "roll" } }),
  ).toThrow();
  expect(
    readGroupPaymentRates({ usdt: { channel: "-5", type: "channel" } }).usdt
      ?.channel,
  ).toBe("-5");
});
