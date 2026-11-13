import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import {
  CryptoIntentStore,
  cryptoIntentRefused,
  verifyCryptoIntentRecord,
} from "../src/crypto-intent";
import { ApiClient, ApiError } from "../src/api";

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});
const scope = "a".repeat(64);
const body = {
  request_id: "original-request",
  network: "tron",
  address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
  amount: "1.000000",
  merchant_id: "1",
};
it("accepts only the original request record and a request-bound not-recorded refusal proof", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const intent = await new CryptoIntentStore(scope).begin(
    "/crypto/withdrawals",
    body,
  );
  const record = {
    id: "1".repeat(48),
    merchant_id: "1",
    request_id: "original-request",
    network: "tron",
    address: body.address,
    amount: "1",
    kind: "settlement",
    state: "requested",
    invoice_id: "",
  };
  expect(verifyCryptoIntentRecord(record, intent).request_id).toBe(
    "original-request",
  );
  for (const changed of [
    { amount: "2" },
    { merchant_id: "2" },
    { request_id: "other" },
    { network: "ethereum" },
    { kind: "refund" },
    { state: "invented" },
  ])
    expect(() =>
      verifyCryptoIntentRecord({ ...record, ...changed }, intent),
    ).toThrow();
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({
            code: -1,
            message: "not recorded",
            data: {
              request_not_recorded: true,
              kind: "crypto",
              merchant_id: "1",
              external_no: "original-request",
              reason: "conflict",
            },
          }),
          { status: 409, headers: { "Content-Type": "application/json" } },
        ),
      ),
  );
  const client = new ApiClient("merchant");
  client.csrfToken = "private-csrf";
  let refusal: unknown;
  try {
    await client.request("/crypto/withdrawals", { method: "POST", body });
  } catch (e) {
    refusal = e;
  }
  expect(cryptoIntentRefused(refusal, intent)).toBe(true);
  expect(cryptoIntentRefused(new ApiError("conflict", 409, -1), intent)).toBe(
    false,
  );
  expect(
    cryptoIntentRefused(
      new ApiError("conflict", 409, -1, undefined, true, {
        kind: "crypto",
        merchant_id: "2",
        external_no: "original-request",
        reason: "conflict",
      }),
      intent,
    ),
  ).toBe(false);
});
it("recovers the original immutable crypto request across a new store and rejects changed inputs", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const first = new CryptoIntentStore(scope);
  await first.begin("/crypto/withdrawals", body);
  const recovered = await new CryptoIntentStore(scope).read();
  expect(recovered?.body).toEqual({ ...body, amount: "1" });
  expect(recovered?.digest).toMatch(/^[a-f0-9]{64}$/);
  await expect(
    first.begin("/crypto/withdrawals", { ...body, request_id: "replacement" }),
  ).rejects.toThrow(/原请求/);
  await expect(
    first.begin("/crypto/withdrawals", { ...body, amount: "2" }),
  ).rejects.toThrow(/原请求/);
  expect(await new CryptoIntentStore("b".repeat(64)).read()).toBeNull();
});
it("rejects secrets, corrupt snapshots and unavailable storage before a financial dispatch", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const store = new CryptoIntentStore(scope);
  await expect(
    store.begin("/crypto/withdrawals", {
      ...body,
      current_password: "secret-password",
    }),
  ).rejects.toThrow();
  expect(localStorage.getItem(store.key)).toBeNull();
  await store.begin("/crypto/withdrawals", body);
  const raw = localStorage.getItem(store.key)!;
  expect(raw).not.toMatch(/password|csrf|token/);
  localStorage.setItem(store.key, raw.replace('"amount":"1"', '"amount":"2"'));
  await expect(store.read()).rejects.toThrow(/存储/);
  const denied = {
    getItem: () => null,
    setItem: () => {
      throw Error("denied");
    },
  } as unknown as Storage;
  await expect(
    new CryptoIntentStore(scope, denied).begin("/crypto/withdrawals", body),
  ).rejects.toThrow(/存储/);
});
