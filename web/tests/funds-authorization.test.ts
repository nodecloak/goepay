import { it, expect, vi, afterEach } from "vitest";
import { ApiClient } from "../src/api";
import {
  readPaymentPasswordStatus,
  validateAuthorizationTarget,
  readAuthorizationDeadline,
  isAdminFundsAction,
  needsFundsAuthorization,
} from "../src/funds-authorization";
afterEach(() => vi.unstubAllGlobals());
it("historical frozen funding adoption always requests an exact action grant", () => {
  expect(isAdminFundsAction("/orders/T/adopt-income-hold", "POST")).toBe(true);
  expect(
    needsFundsAuthorization(
      "/console/v1/admin/orders/T/adopt-income-hold",
      false,
    ),
  ).toBe(true);
  expect(
    needsFundsAuthorization("/console/v1/admin/orders/T/refund", false),
  ).toBe(false);
  expect(
    isAdminFundsAction("/orders/T/adopt-income-hold?anything=1", "POST"),
  ).toBe(false);
});
it("wrong funds credentials with403 preserve the original administrator login session", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "still-valid";
  client.onUnauthorized = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: -1,
          message: "funds authentication failed",
          data: { error: "FUNDS_AUTHENTICATION_FAILED" },
        }),
        { status: 403 },
      ),
    ),
  );
  await expect(
    client.request("/security/payment-password", { method: "PUT", body: {} }),
  ).rejects.toMatchObject({ status: 403 });
  expect(client.csrfToken).toBe("still-valid");
  expect(client.onUnauthorized).not.toHaveBeenCalled();
});
it("matches only the exact administrator financial POST allowlist, leaving queries and unrelated actions alone", () => {
  for (const path of [
    "/transfers",
    "/referral-policies/global/0",
    "/referral-policies/group/1",
    "/transfer-batches",
    "/transfer-batches/1/execute",
    "/transfer-batches/1/cancel-unsent",
    "/orders/T/refund",
    "/orders/T/close",
    "/orders/T/freeze-income",
    "/orders/T/unfreeze-income",
    "/workflows/settlements",
    "/workflows/deposits/2/reject",
    "/workflows/profitsharing-rules/3/execute",
  ])
    expect(isAdminFundsAction(path, "POST")).toBe(true);
  for (const path of [
    "/transfers/1/query",
    "/referral-policies/global/1",
    "/referral-policies/group/0",
    "/referral-policies/group/01",
    "/orders/T/query",
    "/orders/T/resend-notification",
    "/orders/T/sandbox-paid",
    "/transfer-batches/1/query",
    "/transfer-batches/0/execute",
    "/workflows/recharges",
    "/transfers?query=anything",
    "/transfers/extra",
  ])
    expect(isAdminFundsAction(path, "POST")).toBe(false);
  expect(isAdminFundsAction("/transfers", "GET")).toBe(false);
});
it("requires complete status booleans and a safe administrator target", () => {
  expect(() => readPaymentPasswordStatus({ configured: false })).toThrow();
  expect(
    readPaymentPasswordStatus({
      configured: false,
      enforced: true,
      credential_epoch: 0,
      totp_required: false,
    }).enforced,
  ).toBe(true);
  expect(
    validateAuthorizationTarget("/console/v1/admin/orders/TRADE/refund"),
  ).toBe("/orders/TRADE/refund");
  for (const target of [
    "/console/v1/merchant/transfers",
    "https://invalid/console/v1/admin/transfers",
    "/console/v1/admin/transfers?grant=x",
    "/console/v1/admin/../merchant/transfers",
    "/console/v1/admin/transfers#secret",
  ])
    expect(() => validateAuthorizationTarget(target)).toThrow();
  expect(() =>
    readAuthorizationDeadline({ expires_at: "2001-01-01T00:00:00Z" }),
  ).toThrow();
});
it("transmits a grant only for its explicit administrator POST and never retains it on subsequent calls", async () => {
  const fetcher = vi.fn().mockImplementation(() =>
    Promise.resolve(
      new Response(JSON.stringify({ code: 0, data: { status: "unknown" } }), {
        status: 202,
      }),
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  const admin = new ApiClient("admin");
  admin.csrfToken = "csrf";
  await admin.request("/transfers", {
    method: "POST",
    body: { external_no: "original" },
    paymentAuthorization: "one-use-grant",
  });
  expect(fetcher).toHaveBeenLastCalledWith(
    "/console/v1/admin/transfers",
    expect.objectContaining({
      headers: expect.objectContaining({
        "X-Payment-Authorization": "one-use-grant",
        "X-CSRF-Token": "csrf",
      }),
    }),
  );
  await admin.request("/transfers");
  expect(fetcher.mock.calls.at(-1)?.[1].headers).not.toHaveProperty(
    "X-Payment-Authorization",
  );
  const merchant = new ApiClient("merchant");
  merchant.csrfToken = "merchant";
  await expect(
    merchant.request("/transfers", {
      method: "POST",
      body: {},
      paymentAuthorization: "grant",
    }),
  ).rejects.toThrow();
  await expect(
    admin.request("/transfers", { paymentAuthorization: "grant" }),
  ).rejects.toThrow();
  await admin.request("/security/payment-password", {
    method: "PUT",
    body: { current_password: "login", new_payment_password: "funds" },
  });
  expect(fetcher.mock.calls.at(-1)?.[1].method).toBe("PUT");
});
