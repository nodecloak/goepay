import { it, expect } from "vitest";
import {
  normalizeChannelLimits,
  readRuntimeChannelLimits,
  readRuntimeChannelLimitsResult,
  runtimeChannelLimitsPath,
} from "../src/runtime-channel-limits";
const fields = {
  daytop: "100.00",
  daymaxorder: "2",
  paymin: "0.01",
  paymax: "100.00",
  timestart: "23",
  timestop: "2",
  rate_limit_seconds: "30",
  rate_limit_count: "3",
};
const state = {
  ...fields,
  channel_id: "9007199254740993",
  pay_type: "alipay",
  configured: true,
  revision: "1",
  fingerprint: "a".repeat(64),
  can_update: true,
  timezone: "UTC+08:00",
  semantics: "successful-threshold-v1",
  usage: {
    day: "2026-10-26",
    successful_amount: "150.00",
    successful_count: "1",
    recent_starts: "2",
    day_paused: true,
    rate_limited: false,
    as_of: "2026-10-26T06:00:00Z",
  },
};
it("requires exact targets and authenticated units without accepting unknown projections", () => {
  expect(runtimeChannelLimitsPath(state.channel_id)).toBe(
    "/channel-policies/9007199254740993/limits",
  );
  expect(readRuntimeChannelLimits(state)).toEqual(state);
  for (const v of [
    { ...state, channel_id: 9007199254740993 },
    { ...state, channel_id: "01" },
    { ...state, daytop: "1e2" },
    { ...state, paymin: "0.001" },
    { ...state, timestart: "24" },
    { ...state, timestop: "" },
    { ...state, rate_limit_seconds: "2147483648" },
    { ...state, revision: "0" },
    { ...state, timezone: "UTC" },
    { ...state, usage: { ...state.usage, day_paused: false } },
    { ...state, usage: { ...state.usage, rate_limited: true } },
    { ...state, usage: { ...state.usage, successful_amount: 150 } },
    { ...state, usage: { ...state.usage, day: "2026-02-30" } },
    { ...state, api_key: "secret" },
  ])
    expect(() => readRuntimeChannelLimits(v)).toThrow();
});
it("normalizes original zero and equal-hour meanings with bounded integer arithmetic", () => {
  expect(
    normalizeChannelLimits({
      ...fields,
      daytop: "",
      paymin: "0",
      timestart: "13",
      timestop: "13",
    }),
  ).toEqual({
    ...fields,
    daytop: "0.00",
    paymin: "0.00",
    timestart: "13",
    timestop: "13",
  });
  for (const v of [
    { ...fields, paymin: "2", paymax: "1" },
    { ...fields, daymaxorder: "9223372036854775808" },
    { ...fields, rate_limit_count: "-1" },
    { ...fields, timestart: "" },
    { ...fields, daytop: "92233720368547758.08" },
  ])
    expect(() => normalizeChannelLimits(v)).toThrow();
});
it("a save response must bind exact desired fields, target and a single new revision", () => {
  const next = { ...state, revision: "2", fingerprint: "b".repeat(64) };
  expect(
    readRuntimeChannelLimitsResult(
      next,
      readRuntimeChannelLimits(state),
      fields,
    ),
  ).toEqual(next);
  for (const v of [
    { ...next, channel_id: "7" },
    { ...next, daytop: "99.00" },
    { ...next, revision: "3" },
    { ...next, fingerprint: state.fingerprint },
  ])
    expect(() =>
      readRuntimeChannelLimitsResult(
        v,
        readRuntimeChannelLimits(state),
        fields,
      ),
    ).toThrow();
});
