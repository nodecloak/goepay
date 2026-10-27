import { it, expect } from "vitest";
import {
  readGroupPaymentRates,
  setGroupPaymentRate,
  groupPaymentRatesError,
} from "../src/group-payment-rates";

it("reads original route choices with exact runtime IDs and keeps them when only the rate changes", () => {
  const original = {
    alipay: { channel: "9007199254740993", type: "channel", rate: "99.50" },
    wxpay: { channel: "7", type: "roll", rate: "98" },
  };
  expect(readGroupPaymentRates(original)).toEqual(original);
  expect(JSON.parse(setGroupPaymentRate(original, "alipay", "97.75"))).toEqual({
    ...original,
    alipay: { ...original.alipay, rate: "97.75" },
  });
  expect(original.alipay.rate).toBe("99.50");
  expect(groupPaymentRatesError(original)).toBe("");
});

it("accepts only explicit supported original route sentinels and safe legacy numeric IDs", () => {
  for (const [type, channel] of [
    ["", 0],
    ["channel", 0],
    ["roll", 0],
    ["channel", -5],
    ["channel", 7],
    ["roll", "9223372036854775807"],
  ] as const) {
    expect(readGroupPaymentRates({ alipay: { type, channel } })).toEqual({
      alipay: { type, channel: String(channel) },
    });
  }
});

it("rejects rounded IDs, missing route types and unsupported routing kinds without dropping configuration", () => {
  for (const setting of [
    { channel: 9007199254740993, type: "channel" },
    { channel: "9223372036854775808", type: "channel" },
    { channel: "01", type: "channel" },
    { channel: "1e2", type: "channel" },
    { channel: "7" },
    { type: "channel" },
    { channel: "-2", type: "roll" },
    { channel: "-5", type: "roll" },
    { channel: "7", type: "" },
    { channel: "7", type: "channel", private_key: "x" },
  ]) {
    expect(() => readGroupPaymentRates({ alipay: setting })).toThrow();
    expect(() =>
      setGroupPaymentRate({ alipay: setting }, "wxpay", "99"),
    ).toThrow();
  }
  expect(() =>
    readGroupPaymentRates({ sandbox: { channel: "1", type: "channel" } }),
  ).toThrow();
});

