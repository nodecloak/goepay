import { it, expect } from "vitest";
import {
  readRuntimeChannelPolicy,
  readRuntimeChannelCatalog,
  readRuntimeChannelPolicyResult,
  channelRetainedRate,
  runtimeChannelPolicyPath,
} from "../src/runtime-channel-policy";
const state = {
  channel_id: "9007199254740993",
  pay_type: "alipay",
  methods: ["page", "scan"],
  configured: true,
  rate: "99.50",
  fee_rate_bps: 50,
  mode: 0,
  enabled: true,
  revision: "1",
  fingerprint: "a".repeat(64),
  can_update: true,
};
it("parses exact channel IDs, policy units and capability fields without accepting secrets", () => {
  expect(readRuntimeChannelPolicy(state)).toEqual(state);
  for (const raw of [
    null,
    [],
    { ...state, channel_id: 9007199254740993 },
    { ...state, channel_id: "01" },
    { ...state, channel_id: "9223372036854775808" },
    { ...state, pay_type: "unknown" },
    { ...state, methods: ["page", "page"] },
    { ...state, methods: ["http://private"] },
    { ...state, rate: "99.5" },
    { ...state, rate: "100.01" },
    { ...state, fee_rate_bps: 49 },
    { ...state, mode: 1 },
    { ...state, revision: "0" },
    { ...state, enabled: 1 },
    { ...state, can_update: "true" },
    { ...state, config: {} },
    { ...state, api_key: "secret" },
  ])
    expect(() => readRuntimeChannelPolicy(raw)).toThrow();
});
it("unconfigured policy is explicit legacy behavior rather than a fabricated 100 percent rate", () => {
  const legacy = {
    ...state,
    configured: false,
    rate: null,
    fee_rate_bps: null,
    revision: "0",
  };
  expect(readRuntimeChannelPolicy(legacy)).toEqual(legacy);
  for (const raw of [
    { ...legacy, rate: "100.00" },
    { ...legacy, revision: "1" },
    { ...legacy, enabled: false },
    { ...legacy, fee_rate_bps: 0 },
  ])
    expect(() => readRuntimeChannelPolicy(raw)).toThrow();
});
it("catalog rejects duplicate IDs and mismatched totals", () => {
  expect(readRuntimeChannelCatalog({ items: [state], total: 1 })).toEqual([
    state,
  ]);
  for (const v of [
    { items: [state, state], total: 2 },
    { items: [state], total: 2 },
    { items: [state], total: "1" },
    { items: [state], total: 1, config: {} },
  ])
    expect(() => readRuntimeChannelCatalog(v)).toThrow();
});
it("retained percentages map exactly to fee basis points including empty and zero", () => {
  expect(channelRetainedRate("99.5")).toEqual({
    rate: "99.50",
    retainedBPS: 9950,
    feeBPS: 50,
  });
  expect(channelRetainedRate("")).toEqual({
    rate: "100.00",
    retainedBPS: 10000,
    feeBPS: 0,
  });
  expect(channelRetainedRate("0")).toEqual({
    rate: "0.00",
    retainedBPS: 0,
    feeBPS: 10000,
  });
  for (const v of ["-1", "1e2", "099.5", "100.01", "99.999", "NaN", "Infinity"])
    expect(() => channelRetainedRate(v)).toThrow();
});
it("write results bind exact original channel, revision, mode, methods and intended values", () => {
  const before = readRuntimeChannelPolicy(state),
    after = {
      ...state,
      rate: "98.75",
      fee_rate_bps: 125,
      enabled: false,
      revision: "2",
      fingerprint: "b".repeat(64),
    },
    desired = { rate: "98.75", mode: 0 as const, enabled: false };
  expect(readRuntimeChannelPolicyResult(after, before, desired)).toEqual(after);
  for (const v of [
    { ...after, channel_id: "9007199254740995" },
    { ...after, pay_type: "wxpay" },
    { ...after, methods: ["page"] },
    { ...after, revision: "3" },
    { ...after, fingerprint: state.fingerprint },
    { ...after, enabled: true },
    { ...after, rate: "99.50", fee_rate_bps: 50 },
    { ...after, can_update: false },
  ])
    expect(() => readRuntimeChannelPolicyResult(v, before, desired)).toThrow();
});
it("policy URL cannot target a different namespace or smuggle query parameters", () => {
  expect(runtimeChannelPolicyPath(state.channel_id)).toBe(
    "/channel-policies/" + state.channel_id,
  );
  for (const id of [
    "",
    "0",
    "01",
    "-1",
    "9223372036854775808",
    "7?channel_id=9",
  ])
    expect(() => runtimeChannelPolicyPath(id)).toThrow();
});
