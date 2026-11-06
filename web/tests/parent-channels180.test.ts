import { expect, it } from "vitest";
import {
  readParentChannel,
  readParentChannelList,
  parentChannelPath,
  readParentStatistics,
} from "../src/parent-channels";
const minimal = {
  id: "9007199254740993",
  name: "原通道",
  pay_type: "wxpay",
  plugin: "wxpay",
  configured: false,
};
it("keeps full parent IDs and rejects arbitrary metadata/private material", () => {
  expect(readParentChannel(minimal)).toEqual(minimal);
  expect(parentChannelPath(minimal.id)).toBe(
    "/payment-channels/9007199254740993",
  );
  for (const raw of [
    { ...minimal, id: 9007199254740993 },
    { ...minimal, id: "01" },
    { ...minimal, pay_type: "unknown" },
    { ...minimal, config: {} },
    { ...minimal, proof_hash: "secret" },
  ])
    expect(() => readParentChannel(raw)).toThrow();
  expect(() => parentChannelPath("../keys")).toThrow();
});
it("keeps unknown parent cost unknown and validates the statistics target", () => {
  const day = {
    day: "2026-10-26",
    realmoney: "100.00",
    refundmoney: "0.00",
    profitmoney: null,
    orderCount: "1",
    successCount: "1",
    successRate: "100.00",
  };
  const raw = {
    channel_id: minimal.id,
    items: Array.from({ length: 7 }, (_, i) => ({
      ...day,
      day: "2026-10-" + (20 + i),
    })),
    cost_basis: "unknown",
    money_basis: "verified",
  };
  expect(readParentStatistics(raw, minimal.id).items).toHaveLength(7);
  expect(() => readParentStatistics(raw, "8")).toThrow();
  expect(() =>
    readParentStatistics(
      { ...raw, items: [{ ...day, profitmoney: "100.00" }] },
      minimal.id,
    ),
  ).toThrow();
});
it("requires exact page envelope and unique actual IDs", () => {
  expect(
    readParentChannelList({ items: [minimal], total: 1, page: 1, size: 20 })
      .items,
  ).toHaveLength(1);
  for (const raw of [
    { items: [minimal, minimal], total: 2, page: 1, size: 20 },
    { items: [minimal], total: 0, page: 1, size: 20 },
    { items: [minimal], total: 1, page: 0, size: 20 },
    { items: [minimal], total: 1, page: 1, size: 20, private_key: "secret" },
  ])
    expect(() => readParentChannelList(raw)).toThrow();
});
