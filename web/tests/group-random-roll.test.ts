import { it, expect } from "vitest";
import { groupRouteValue, setGroupPaymentRoute } from "../src/group-routing";
import { readGroupPaymentRates } from "../src/group-payment-rates";
import { groupDirectoryRates } from "../src/group-directory";

it("keeps original random-roll type and retained share through rate and route editing", () => {
  const original = {
    alipay: { channel: "-3", type: "roll", rate: "99.50" },
    wxpay: { channel: "7", type: "channel", rate: "98" },
  };
  expect(groupRouteValue(original, "alipay")).toBe("random_roll");
  expect(groupDirectoryRates({ rate_bps: 0, info: original })).toMatchObject({
    error: "",
    lines: [
      { label: "统一分成", value: "100.00%" },
      { label: "支付宝", value: "随机可用轮询组 · 99.50%" },
      { label: "微信支付", value: "通道 #7 · 98%" },
    ],
  });
  expect(
    JSON.parse(
      setGroupPaymentRoute(
        { ...original, alipay: { rate: "99.50" } },
        "alipay",
        "random_roll",
      ),
    ),
  ).toEqual(original);
  expect(
    JSON.parse(setGroupPaymentRoute(original, "alipay", "channel:8")).alipay,
  ).toEqual({ channel: "8", type: "channel", rate: "99.50" });
  for (const type of ["channel", ""])
    expect(() =>
      readGroupPaymentRates({ alipay: { channel: "-3", type } }),
    ).toThrow();
});
