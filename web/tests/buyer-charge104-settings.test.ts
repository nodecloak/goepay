import { expect, it } from "vitest";
import {
  parseBuyerFee104,
  buyerFeeBody104,
  parseMinimum104,
  minimumBody104,
} from "../src/buyer-charge104-settings";
const merchant = {
  merchant_id: "9007199254740993",
  mode: 0,
  mode_feerate: "",
  configured: false,
  revision: "0",
  fingerprint: "a".repeat(64),
  can_update: true,
  partial_available: true,
};
const global = {
  kind: "global",
  channel_id: "0",
  payfee_lessthan: "0.10",
  payfee_mincost: "0.20",
  effective_payfee_lessthan: "0.10",
  effective_payfee_mincost: "0.20",
  configured: true,
  revision: "1",
  fingerprint: "b".repeat(64),
  can_update: true,
};
it("keeps original personal fields, exact owner, blank full buyer and zero merchant endpoints", () => {
  const state = parseBuyerFee104(merchant, merchant.merchant_id);
  expect(buyerFeeBody104(state, { mode: 1, mode_feerate: "" })).toEqual({
    mode: 1,
    mode_feerate: "",
    expected_fingerprint: merchant.fingerprint,
  });
  expect(
    buyerFeeBody104(state, { mode: 1, mode_feerate: "0" }).mode_feerate,
  ).toBe("0");
  expect(
    buyerFeeBody104(state, { mode: 1, mode_feerate: "3" }).mode_feerate,
  ).toBe("3");
  expect(
    buyerFeeBody104(state, { mode: 1, mode_feerate: "0.000000000001" })
      .mode_feerate,
  ).toBe("0.000000000001");
  expect(() =>
    buyerFeeBody104(state, { mode: 1, mode_feerate: "0.0000000000001" }),
  ).toThrow();
  for (const bad of [
    { ...merchant, merchant_id: "2" },
    { ...merchant, mode: 2 },
    { ...merchant, mode_feerate: "3" },
    { ...merchant, revision: 0 },
    { ...merchant, proof_hash: "private" },
  ])
    expect(() => parseBuyerFee104(bad, merchant.merchant_id)).toThrow();
});
it("keeps global and parent minimum currency, blank and zero preserve original global-pair fallback", () => {
  const state = parseMinimum104(global, "global", "0");
  expect(
    minimumBody104(state, { payfee_lessthan: "0", payfee_mincost: "0.01" }),
  ).toEqual({
    payfee_lessthan: "0.00",
    payfee_mincost: "0.01",
    expected_fingerprint: global.fingerprint,
  });
  const parent = parseMinimum104(
    {
      ...global,
      kind: "parent",
      channel_id: "9007199254740993",
      payfee_lessthan: "",
      payfee_mincost: "",
    },
    "parent",
    "9007199254740993",
  );
  expect(
    minimumBody104(parent, { payfee_lessthan: "0", payfee_mincost: "" }),
  ).toEqual({
    payfee_lessthan: "0.00",
    payfee_mincost: "",
    expected_fingerprint: global.fingerprint,
  });
  const zeroParent = parseMinimum104(
    { ...parent, payfee_lessthan: "0.00", payfee_mincost: "0.01" },
    "parent",
    parent.channel_id,
  );
  expect(zeroParent.effective_payfee_lessthan).toBe("0.10");
  for (const bad of [
    { ...global, channel_id: "1" },
    { ...global, payfee_mincost: "" },
    { ...global, effective_payfee_mincost: "0.21" },
    { ...global, payfee_mincost: 0.2 },
  ])
    expect(() => parseMinimum104(bad, "global", "0")).toThrow();
  for (const amount of ["1e2", "-1", "0.001", "92233720368547758.08"])
    expect(() =>
      minimumBody104(state, { payfee_lessthan: amount, payfee_mincost: "0" }),
    ).toThrow();
});
