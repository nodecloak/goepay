import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { ApiError } from "../src/api";
import {
  FundsIntentStore212,
  fundsScope212,
  fundsDefinitiveRefusal212,
  verifiedWorkflow212,
} from "../src/funds-intent212";
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});
const scope = "a".repeat(64);
it("recovers immutable money and request number after a new store instance", () => {
  const a = new FundsIntentStore212(scope, "refund", "T1");
  const body = { amount: "2.50", external_no: "original" };
  a.begin("/orders/T1/refund", body);
  body.amount = "8.00";
  expect(new FundsIntentStore212(scope, "refund", "T1").read()?.body).toEqual({
    amount: "2.50",
    external_no: "original",
  });
  expect(() =>
    a.begin("/orders/T1/refund", {
      amount: "8.00",
      external_no: "replacement",
    }),
  ).toThrow(/原资金请求/);
  expect(
    new FundsIntentStore212("b".repeat(64), "refund", "T1").read(),
  ).toBeNull();
});
it("fails closed on malformed data or storage failure instead of forgetting the original", () => {
  const a = new FundsIntentStore212(scope, "refund", "T1");
  localStorage.setItem(a.key, "broken");
  expect(() => a.read()).toThrow(/保存/);
  const denied = {
    getItem: () => null,
    setItem: () => {
      throw Error("quota");
    },
  } as unknown as Storage;
  expect(() =>
    new FundsIntentStore212(scope, "refund", "T1", denied).begin(
      "/orders/T1/refund",
      { amount: "2.50", external_no: "r" },
    ),
  ).toThrow(/保存/);
  expect(localStorage.getItem(a.key)).toBe("broken");
});
it("recovers after reauthentication but isolates another principal or issuer without storing credentials", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const a = await fundsScope212({
    area: "merchant",
    id: "1000",
    issuer: "",
    csrf: "private-session-one",
  });
  expect(a).not.toBe(
    await fundsScope212({
      area: "merchant",
      id: "1001",
      issuer: "",
      csrf: "private-session-one",
    }),
  );
  new FundsIntentStore212(a, "refund", "T1").begin("/orders/T1/refund", {
    amount: "2.50",
    external_no: "r",
  });
  const refreshed = await fundsScope212({
    area: "merchant",
    id: "1000",
    issuer: "",
    csrf: "private-session-two",
  });
  expect(
    new FundsIntentStore212(refreshed, "refund", "T1").read()?.body,
  ).toEqual({ amount: "2.50", external_no: "r" });
  const delegated = await fundsScope212({
    area: "merchant",
    id: "1000",
    issuer: "7",
    csrf: "private-session-two",
  });
  expect(new FundsIntentStore212(delegated, "refund", "T1").read()).toBeNull();
  expect(JSON.stringify(localStorage)).not.toContain("private-session-one");
});
it("distinguishes decoded refusals from a lost, malformed or conflicting result", () => {
  expect(fundsDefinitiveRefusal212(new ApiError("invalid", 400, -1))).toBe(
    true,
  );
  expect(fundsDefinitiveRefusal212(new ApiError("conflict", 409, -1))).toBe(
    false,
  );
  expect(
    fundsDefinitiveRefusal212(
      new ApiError("bad body", 400, -1, undefined, false),
    ),
  ).toBe(false);
  expect(fundsDefinitiveRefusal212(new TypeError("network"))).toBe(false);
});
it("requires the original payer recipient amount and external number when resolving an application", () => {
  const store = new FundsIntentStore212(scope, "internal-transfers", "1000");
  const intent = store.begin("/workflows/internal-transfers", {
    amount: "1.00",
    external_no: "r1",
    recipient_id: "1001",
  });
  const record = {
    id: "81",
    kind: "internal-transfers",
    merchant_id: "1000",
    recipient_id: "1001",
    amount_cents: "100",
    external_no: "r1",
    status: "pending",
  };
  expect(verifiedWorkflow212(record, intent, "1000")).toEqual({
    id: "81",
    status: "pending",
  });
  for (const changed of [
    { merchant_id: "1002" },
    { recipient_id: "1002" },
    { amount_cents: "200" },
    { external_no: "r2" },
  ])
    expect(() =>
      verifiedWorkflow212({ ...record, ...changed }, intent, "1000"),
    ).toThrow();
});

it.each([
  undefined,
  null,
  { id: "0", status: "pending" },
  { id: "9223372036854775808", status: "pending" },
  { id: "81", status: "succeeded" },
])(
  "rejects a corrupted accepted workflow receipt instead of allowing discard: %j",
  (record) => {
    const store = new FundsIntentStore212(scope, "internal-transfers", "1000");
    const original = store.begin("/workflows/internal-transfers", {
      amount: "1.00",
      external_no: "r",
      recipient_id: "1001",
    });
    localStorage.setItem(
      store.key,
      JSON.stringify({ ...original, phase: "accepted", record }),
    );
    expect(() => store.read()).toThrow(/保存/);
  },
);

it.each([
  {
    amount: "1.00",
    external_no: "r",
    expected_fee_cents: "0",
    expected_net_cents: "100",
  },
  {
    amount: "1.00",
    external_no: "r",
    expected_fee_cents: "0",
    expected_net_cents: "200",
    recipient_config_id: "42",
  },
])(
  "refuses malformed saved settlement terms rather than retrying with a new recipient version",
  (body) => {
    const store = new FundsIntentStore212(scope, "settlements", "1000");
    localStorage.setItem(
      store.key,
      JSON.stringify({
        version: 1,
        principal: scope,
        kind: "settlements",
        object: "1000",
        target: "/workflows/settlements",
        phase: "unknown",
        body,
      }),
    );
    expect(() => store.read()).toThrow(/保存/);
  },
);
