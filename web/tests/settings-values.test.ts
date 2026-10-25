import { it, expect } from "vitest";
import {
  settingDefinition,
  settingDisplay,
  settingStorage,
} from "../src/settings";
it("places the original ordinary transfer permission with payment controls and explains inheritance", () => {
  const d = settingDefinition("user_transfer");
  expect(d.tab).toBe("transfer");
  expect(d.label).toContain("代付功能");
  expect(d.hint).toContain("商户自身权限");
  expect(settingStorage(d, "1")).toBe("1");
});
it("keeps original payment amount settings in yuan with exact limits", () => {
  for (const key of ["pay_minmoney", "pay_maxmoney"]) {
    const d = settingDefinition(key);
    expect(d.tab).toBe("pay");
    expect(d.kind).toBe("money");
    expect(settingStorage(d, "90071992547409.93")).toBe("90071992547409.93");
    expect(settingStorage(d, "0")).toBe("0.00");
    for (const invalid of ["-1", "1e2", "0.001", "92233720368547758.08"])
      expect(() => settingStorage(d, invalid)).toThrow();
  }
});
it("offers the original four global settlement modes in the settlement tab", () => {
  const d = settingDefinition("settle_open");
  expect(d.tab).toBe("settle");
  expect(d.kind).toBe("select");
  expect(d.options).toEqual([
    { value: "0", label: "关闭结算" },
    { value: "1", label: "只开启每日自动结算" },
    { value: "2", label: "只开启手动申请结算" },
    { value: "3", label: "开启自动+手动结算" },
  ]);
  for (const value of ["0", "1", "2", "3"])
    expect(settingStorage(d, value)).toBe(value);
  expect(() => settingStorage(d, "4")).toThrow();
});
it("keeps original admin risk and pending-business notification mode namespaces distinct", () => {
  const risk = settingDefinition("msgconfig_risk"),
    business = settingDefinition("msgconfig_domain");
  expect(risk.options?.find((x) => x.value === "2")?.label).toBe("Telegram");
  expect(business.options?.find((x) => x.value === "2")?.label).toBe(
    "群机器人",
  );
  expect(settingStorage(business, "4")).toBe("4");
  expect(() => settingStorage(risk, "4")).toThrow("有效选项");
});
it("uses exact integer conversion for money and basis points and rejects fractional cents", () => {
  const minimum = settingDefinition("settlement_minimum_cents"),
    rate = settingDefinition("settlement_fee_rate_bps");
  expect(settingDisplay(minimum, "9007199254740993")).toBe("90071992547409.93");
  expect(settingStorage(minimum, "1.23")).toBe("123");
  expect(settingDisplay(rate, "101")).toBe("1.01");
  expect(settingStorage(rate, "1.01")).toBe("101");
  for (const invalid of ["0.005", "-1", "1e2", "01.00", "Infinity"])
    expect(() => settingStorage(minimum, invalid)).toThrow();
  expect(() => settingStorage(rate, "100.01")).toThrow("上限");
  expect(() => settingStorage(minimum, "")).toThrow("明确");
});
