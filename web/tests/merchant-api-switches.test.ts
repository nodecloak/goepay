import { expect, it, vi, afterEach } from "vitest";
import {
  parseMerchantAPISwitches,
  merchantAPISwitchBody,
  parseMerchantKeySession,
} from "../src/merchant-api-switches";
import { ApiClient } from "../src/api";
import actualKeyLogin from "../e2e/merchant174-key-login-http-fixture.json";
const mid = "9007199254740993";
const state = {
  merchant_id: mid,
  keylogin: 1,
  refund: 1,
  transfer: 0,
  configured: false,
  revision: "0",
  fingerprint: "a".repeat(64),
  can_update: true,
  transfer_granted: false,
} as const;
afterEach(() => vi.unstubAllGlobals());
it("accepts the actual disposable large-owner HTTP DTO and rejects administrator or internal metadata", () => {
  expect(actualKeyLogin.synthetic_only).toBe(true);
  expect(actualKeyLogin.schema_disposed).toBe(true);
  expect(actualKeyLogin.session_invalid_after_cleanup).toBe(true);
  expect(actualKeyLogin.cookie_or_bearer_exported).toBe(false);
  const data = actualKeyLogin.response.data;
  expect(parseMerchantKeySession(data, mid)).toEqual(data);
  for (const extra of [
    { admin_class: "operator" },
    { username: "internal" },
    { auth_version: 1 },
  ]) {
    expect(() =>
      parseMerchantKeySession(
        { ...data, user: { ...data.user, ...extra } },
        mid,
      ),
    ).toThrow();
  }
});
it("preserves complete string IDs and exact flags/defaults with no metadata or credentials", () => {
  expect(parseMerchantAPISwitches(state, mid)).toEqual(state);
  expect(
    merchantAPISwitchBody(state, {
      keylogin: false,
      refund: true,
      transfer: true,
    }),
  ).toEqual({
    keylogin: 0,
    refund: 1,
    transfer: 1,
    expected_fingerprint: state.fingerprint,
  });
  for (const patch of [
    { merchant_id: Number(mid) },
    { merchant_id: "9007199254740995" },
    { keylogin: true },
    { refund: null },
    { transfer: 2 },
    { revision: 0 },
    { revision: "00" },
    { configured: true },
    { transfer: 1 },
    { api_key: "private" },
    { can_update: false },
  ])
    expect(() =>
      parseMerchantAPISwitches({ ...state, ...patch }, mid),
    ).toThrow();
  expect(() =>
    parseMerchantAPISwitches(
      { ...state, configured: true, revision: "1", transfer: 1 },
      mid,
    ),
  ).not.toThrow();
});
it("accepts only an original merchant session for exactly the requested full owner", () => {
  const s = {
    user: { id: mid, name: "owner", role: "merchant" },
    csrf_token: "c".repeat(43),
  };
  expect(parseMerchantKeySession(s, mid)).toEqual(s);
  for (const patch of [
    { user: { ...s.user, id: Number(mid) } },
    { user: { ...s.user, id: "9007199254740995" } },
    { user: { ...s.user, role: "admin" } },
    { delegated_by: { id: "1", name: "super" } },
    { csrf_token: "bad" },
    { api_key: "secret" },
  ])
    expect(() => parseMerchantKeySession({ ...s, ...patch }, mid)).toThrow();
});
it("key login sends credentials only in POST JSON and pins same-client CSRF across a late response", async () => {
  const client = new ApiClient("merchant");
  let resolve!: (v: Response) => void;
  const fetcher = vi.fn(
    (_url: string, _options?: RequestInit) =>
      new Promise<Response>((r) => (resolve = r)),
  );
  vi.stubGlobal("fetch", fetcher);
  const pending = client.merchantKeyLogin(mid, "not-in-url", "123456");
  expect(fetcher.mock.calls[0]?.[0]).toBe("/console/v1/merchant/key-login");
  const request = (fetcher.mock.calls[0] as unknown[])[1] as RequestInit;
  expect(request.method).toBe("POST");
  expect(JSON.parse(String(request.body))).toEqual({
    merchant_id: mid,
    api_key: "not-in-url",
    code: "123456",
  });
  client.csrfToken = "n".repeat(43);
  resolve(
    new Response(
      JSON.stringify({
        code: 0,
        data: {
          user: { id: mid, name: "owner", role: "merchant" },
          csrf_token: "c".repeat(43),
        },
      }),
      { status: 200 },
    ),
  );
  await expect(pending).rejects.toThrow();
  expect(client.csrfToken).toBe("n".repeat(43));
});
it("invalid or cross-owner success never installs a CSRF token and admin has no key-login shortcut", async () => {
  const c = new ApiClient("merchant");
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            code: 0,
            data: {
              user: { id: "2", name: "other", role: "merchant" },
              csrf_token: "c".repeat(43),
            },
          }),
          { status: 200 },
        ),
    ),
  );
  await expect(c.merchantKeyLogin(mid, "key", "")).rejects.toThrow();
  expect(c.csrfToken).toBe("");
  await expect(
    new ApiClient("admin").merchantKeyLogin(mid, "key", ""),
  ).rejects.toThrow();
});
it("a late rejected key login cannot clear the replacement CSRF or invoke its unauthorized handler", async () => {
  const c = new ApiClient("merchant");
  let resolve!: (v: Response) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(() => new Promise<Response>((r) => (resolve = r))),
  );
  const unauthorized = vi.fn();
  c.onUnauthorized = unauthorized;
  const pending = c.merchantKeyLogin(mid, "key", "");
  c.csrfToken = "n".repeat(43);
  resolve(
    new Response(JSON.stringify({ code: -1, message: "invalid credentials" }), {
      status: 401,
    }),
  );
  await expect(pending).rejects.toThrow();
  expect(c.csrfToken).toBe("n".repeat(43));
  expect(unauthorized).not.toHaveBeenCalled();
});
