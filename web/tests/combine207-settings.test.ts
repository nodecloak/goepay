import { expect, it } from "vitest";
import { settingDefinition, settingStorage } from "../src/settings";

it("shows the original WeChat combined-payment settings on the payment tab", () => {
  const open = settingDefinition("wxcombine_open");
  expect(open.tab).toBe("pay");
  expect(open.label).toBe("微信开启合单支付");
  expect(open.options?.map((o) => o.value)).toEqual(["1", "0"]);
  const min = settingDefinition("wxcombine_minmoney");
  const sub = settingDefinition("wxcombine_submoney");
  expect([min.label, sub.label]).toEqual([
    "合单支付最小金额（元）",
    "合单支付子单最大金额（元）",
  ]);
  expect(settingStorage(min, "100")).toBe("100.00");
  expect(settingStorage(sub, "0.29")).toBe("0.29");
  expect(() => settingStorage(sub, "1.001")).toThrow();
  expect(() => settingStorage(open, "2")).toThrow();
});
