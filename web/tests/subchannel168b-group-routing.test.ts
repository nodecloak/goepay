import { describe, it, expect } from "vitest";
import { readGroupPaymentRates } from "../src/group-payment-rates";
import { groupRouteValue, setGroupPaymentRoute } from "../src/group-routing";
describe("subchannel168b original -2 policy", () => {
  it("preserves exact -2 instead of a parent ID and retains the rate", () => {
    const raw = { alipay: { channel: "-2", type: "channel", rate: "99.50" } };
    expect(readGroupPaymentRates(raw).alipay?.channel).toBe("-2");
    expect(groupRouteValue(raw, "alipay")).toBe("subchannels");
    expect(
      JSON.parse(setGroupPaymentRoute(raw, "alipay", "subchannels")).alipay,
    ).toEqual(raw.alipay);
  });
  it("does not accept -2 as a roll or sandbox route", () => {
    expect(() =>
      readGroupPaymentRates({ alipay: { channel: "-2", type: "roll" } }),
    ).toThrow();
    expect(() =>
      readGroupPaymentRates({ sandbox: { channel: "-2", type: "channel" } }),
    ).toThrow();
  });
});
