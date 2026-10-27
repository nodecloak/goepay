import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import MerchantAPISwitches from "../src/components/MerchantAPISwitches.vue";
import { ApiClient } from "../src/api";
import actual from "../e2e/globalaccess179-http-fixture.json";
import {
  parsePlatformMerchantAccess,
  parsePublicMerchantAccess,
  platformMerchantAccessBody,
  fetchPublicMerchantAccess,
} from "../src/platform-merchant-access";
afterEach(() => vi.unstubAllGlobals());
it("shows actual platform closure on personal controls and preserves their stored values", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({ code: 0, message: "", data: actual.public_closed }),
        ),
    ),
  );
  const client = new ApiClient("merchant");
  client.csrfToken = "personal-session";
  const personal = {
    merchant_id: "9007199254740993",
    keylogin: 1,
    refund: 1,
    transfer: 0,
    configured: false,
    revision: "0",
    fingerprint: "a".repeat(64),
    can_update: true,
    transfer_granted: false,
  };
  const request = vi.fn().mockResolvedValue(personal);
  client.request = request;
  const w = mount(MerchantAPISwitches, {
    props: { client, merchantId: personal.merchant_id, ownSession: true },
  });
  await flushPromises();
  expect(w.find('[name="keylogin"]').exists()).toBe(false);
  expect(w.find('[name="refund"]').exists()).toBe(false);
  expect(w.text()).toContain("平台已关闭商户自助退款");
  expect(w.find('[name="transfer"]').exists()).toBe(true);
  w.unmount();
});
it("accepts only the disposed actual HTTP DTOs without widening the personal DTO", () => {
  expect(
    actual.synthetic_only &&
      actual.schema_disposed &&
      actual.session_invalid_after_cleanup,
  ).toBe(true);
  expect(actual.cookie_or_bearer_exported).toBe(false);
  expect(parsePlatformMerchantAccess(actual.admin_default)).toEqual(
    actual.admin_default,
  );
  expect(parsePublicMerchantAccess(actual.public_closed)).toEqual(
    actual.public_closed,
  );
  for (const value of [
    { ...actual.admin_default, user_refund: 1 },
    { ...actual.admin_default, revision: "1" },
    { ...actual.admin_default, can_update: false },
  ])
    expect(() => parsePlatformMerchantAccess(value)).toThrow();
  for (const value of [
    { ...actual.public_default, user_refund: "1" },
    { ...actual.public_default, known: true },
    { close_keylogin: false, user_refund: 1 },
  ])
    expect(() => parsePublicMerchantAccess(value)).toThrow();
  expect(
    platformMerchantAccessBody(
      actual.admin_closed as never,
      "close_keylogin",
      true,
    ),
  ).toEqual({
    close_keylogin: "0",
    user_refund: "0",
    expected_fingerprint: actual.admin_closed.fingerprint,
  });
});
it("public capability failures remain unknown and cannot mutate client CSRF", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response("{}", { status: 503 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(fetchPublicMerchantAccess()).rejects.toThrow();
  expect(fetcher).toHaveBeenCalledWith(
    "/console/v1/public/merchant-access",
    expect.objectContaining({ credentials: "omit", cache: "no-store" }),
  );
});
it("hides new key login when the real platform capability is closed while retaining password login", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/session"))
        return new Response(JSON.stringify({ code: -1, message: "login" }), {
          status: 401,
        });
      return new Response(
        JSON.stringify({
          code: 0,
          message: "",
          data: url.endsWith("/merchant-access")
            ? actual.public_closed
            : { zhuce: "", footer: "", format: "html" },
        }),
      );
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/merchant/dashboard");
  const w = mount(App, { global: { plugins: [router] } });
  await flushPromises();
  expect(w.find('[data-login-mode="key"]').exists()).toBe(false);
  expect(w.find('input[name="password"]').exists()).toBe(true);
  expect(w.text()).toContain("平台已关闭密钥登录");
  w.unmount();
});
