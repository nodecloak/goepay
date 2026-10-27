import { it, expect } from "vitest";
import { groupRouteValue, setGroupPaymentRoute } from "../src/group-routing";
import { groupDirectoryRates } from "../src/group-directory";
import { readGroupPaymentRates } from "../src/group-payment-rates";

it("keeps explicit original closed zero separate from a legacy absent route", () => {
  const original = {
    alipay: { channel: "0", type: "", rate: "0" },
    wxpay: { rate: "99" },
  };
  expect(groupRouteValue(original, "alipay")).toBe("disabled");
  expect(groupRouteValue(original, "wxpay")).toBe("default");
  expect(
    JSON.parse(setGroupPaymentRoute(original, "alipay", "default")),
  ).toEqual({ ...original, alipay: { rate: "0" } });
  expect(
    JSON.parse(setGroupPaymentRoute(original, "wxpay", "disabled")),
  ).toEqual({ ...original, wxpay: { rate: "99", channel: "0", type: "" } });
  expect(
    groupDirectoryRates({ info: original }).lines.find(
      (l) => l.label === "支付宝",
    )?.value,
  ).toContain("关闭");
});
it("round trips the original random sequential and first policies without changing rates", () => {
  for (const [value, channel, label] of [
    ["random", "-1", "随机可用"],
    ["sequential", "-4", "顺序可用"],
    ["first", "-5", "首个可用"],
  ]) {
    const raw = JSON.parse(
      setGroupPaymentRoute({ alipay: { rate: "98.25" } }, "alipay", value!),
    );
    expect(raw.alipay).toEqual({ rate: "98.25", channel, type: "channel" });
    expect(groupRouteValue(raw, "alipay")).toBe(value);
    expect(
      groupDirectoryRates({ info: raw }).lines.find((l) => l.label === "支付宝")
        ?.value,
    ).toContain(label);
    expect(
      readGroupPaymentRates({ alipay: { channel, type: "" } }).alipay?.channel,
    ).toBe(channel);
  }
});
it("rejects unsupported simulator routing and numeric negative zero without dropping the original", () => {
  for (const channel of ["0", "-1", "-4", "-5"]) {
    expect(() =>
      readGroupPaymentRates({ sandbox: { channel, type: "channel" } }),
    ).toThrow();
  }
  expect(() =>
    readGroupPaymentRates({ alipay: { channel: -0, type: "channel" } }),
  ).toThrow();
  expect(() => setGroupPaymentRoute({}, "sandbox", "disabled")).toThrow();
  expect(
    JSON.parse(
      setGroupPaymentRoute({ sandbox: { rate: "99" } }, "sandbox", "default"),
    ),
  ).toEqual({ sandbox: { rate: "99" } });
});
