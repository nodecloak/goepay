import { expect, it } from "vitest";
import { settingDefinition, settingStorage } from "../src/settings";
it("exposes the fixed test merchant in original website settings and preserves full IDs", () => {
  const d = settingDefinition("test_pay_uid");
  expect(d.tab).toBe("site");
  expect(d.label).toContain("测试支付收款商户");
  expect(d.hint).toContain("0");
  expect(settingStorage(d, "9007199254740993")).toBe("9007199254740993");
  for (const v of ["01", "-1", "1e2", "9223372036854775808"])
    expect(() => settingStorage(d, v)).toThrow();
});
