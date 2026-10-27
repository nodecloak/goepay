import { it, expect } from "vitest";
import { groupDirectoryRates } from "../src/group-directory";
it("shows original group route identity together with the matching retained rate", () => {
  const result = groupDirectoryRates({
    rate_bps: "38",
    info: {
      alipay: { channel: "9007199254740993", type: "channel", rate: "99.50" },
      wxpay: { channel: "7", type: "roll" },
    },
  });
  expect(result.error).toBe("");
  expect(result.lines).toContainEqual({
    label: "支付宝",
    value: "通道 #9007199254740993 · 99.50%",
  });
  expect(result.lines).toContainEqual({
    label: "微信支付",
    value: "轮询组 #7 · 继承统一分成99.62%",
  });
});
