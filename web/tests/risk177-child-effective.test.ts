import { it, expect } from "vitest";
import { readSubchannelRow, subchannelReason } from "../src/subchannels";
import fixture from "../e2e/subchannel168b-fixture.json";
it("child configuration and an authenticated effective risk are independent and keep exact IDs", () => {
  const raw = {
    ...structuredClone(fixture.admin.items[0]),
    core_risk: {
      blocked: true,
      reason: "通道连续订单未支付",
      until: null,
      action_id: "9007199254741774",
      revision: "1",
    },
    parent_core_risk: {
      blocked: false,
      reason: "",
      until: null,
      action_id: "0",
      revision: "0",
    },
    unavailable_reason: "risk_limited",
    browser_test_available: false,
    enabled: true,
  };
  const row = readSubchannelRow(raw, "admin", "1");
  expect(row.enabled).toBe(true);
  expect(row.coreRisk?.blocked).toBe(true);
  expect(row.coreRisk?.action_id).toBe("9007199254741774");
  expect(row.browserTestAvailable).toBe(false);
  expect(subchannelReason(row.unavailableReason)).toContain("限制新付款");
  expect(() =>
    readSubchannelRow(
      { ...raw, browser_test_available: true, unavailable_reason: "" },
      "admin",
      "1",
    ),
  ).toThrow();
  expect(() =>
    readSubchannelRow({ ...raw, core_risk: null }, "admin", "1"),
  ).toThrow();
});
