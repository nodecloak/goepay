import { it, expect } from "vitest";
import {
  readGroupRoutingCatalog,
  readGroupRoutingRolls,
  groupRouteValue,
  setGroupPaymentRoute,
} from "../src/group-routing";
const catalog = {
  available: true,
  items: [
    { id: "9007199254740993", type: "alipay", methods: ["native"] },
    { id: "8", type: "wxpay", methods: ["native"] },
  ],
};
it("binds choices to actual runtime IDs and globally usable enabled rolls", () => {
  const channels = readGroupRoutingCatalog(catalog);
  expect(channels).toHaveLength(2);
  expect(channels[0]!.id).toBe("9007199254740993");
  const roll = {
    id: "9223372036854775807",
    name: "原轮询",
    merchant_id: "0",
    type: "alipay",
    enabled: true,
    strategy: "first",
    channel_ids: "9007199254740993:1",
  };
  expect(readGroupRoutingRolls([roll], channels)).toEqual([
    {
      value: "roll:9223372036854775807",
      label: "原轮询（轮询组 #9223372036854775807）",
      type: "alipay",
    },
  ]);
  expect(
    readGroupRoutingRolls(
      [
        { ...roll, enabled: false },
        { ...roll, id: "7", merchant_id: "1000" },
      ],
      channels,
    ),
  ).toEqual([]);
  expect(() =>
    readGroupRoutingRolls([{ ...roll, channel_ids: "123:1" }], channels),
  ).toThrow();
});
it("rejects malformed or rounded catalog values and repeated runtime identities", () => {
  for (const raw of [
    {},
    { ...catalog, available: false },
    { ...catalog, items: [{ ...catalog.items[0], id: 9007199254740993 }] },
    { ...catalog, items: [catalog.items[0], catalog.items[0]] },
    { ...catalog, items: [{ ...catalog.items[0], id: "01" }] },
  ])
    expect(() => readGroupRoutingCatalog(raw)).toThrow();
});
it("changes only the chosen original route while keeping both retained rates", () => {
  const original = {
    alipay: { rate: "99.50", channel: "7", type: "channel" },
    wxpay: { rate: "98", channel: "8", type: "roll" },
  };
  expect(groupRouteValue(original, "alipay")).toBe("channel:7");
  const edited = JSON.parse(
    setGroupPaymentRoute(original, "alipay", "roll:9007199254740993"),
  );
  expect(edited).toEqual({
    ...original,
    alipay: { rate: "99.50", channel: "9007199254740993", type: "roll" },
  });
  expect(
    groupRouteValue(
      JSON.parse(setGroupPaymentRoute(original, "alipay", "first")),
      "alipay",
    ),
  ).toBe("first");
  expect(
    groupRouteValue(
      JSON.parse(setGroupPaymentRoute(original, "alipay", "default")),
      "alipay",
    ),
  ).toBe("default");
  for (const value of [
    "channel:01",
    "roll:-5",
    "channel:9223372036854775808",
    "private:7",
  ])
    expect(() => setGroupPaymentRoute(original, "alipay", value)).toThrow();
  expect(original.alipay.channel).toBe("7");
});
