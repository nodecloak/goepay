// @vitest-environment jsdom
import { it, expect, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";

const mounted: Array<{ unmount(): void }> = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  vi.unstubAllGlobals();
});

function stubFetch(providers: Record<string, boolean>, onComplete?: () => any) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: any) => {
      const p = new URL(url, "http://localhost").pathname;
      if (p.endsWith("/session"))
        return new Response(
          JSON.stringify({ code: -1, message: "unauthorized" }),
          { status: 401 },
        );
      if (p.endsWith("/public/site"))
        return new Response(
          JSON.stringify({ code: 0, data: { title: "GOEPAY" } }),
        );
      if (p.endsWith("/public/merchant-access"))
        return new Response(
          JSON.stringify({ code: 0, data: { close_keylogin: 0 } }),
        );
      if (p.endsWith("/connect/options"))
        return new Response(JSON.stringify({ code: 0, data: { providers } }));
      if (p.endsWith("/connect/complete"))
        return new Response(
          JSON.stringify(onComplete ? onComplete() : { code: 0, data: {} }),
        );
      if (p.endsWith("/public/capabilities"))
        return new Response(
          JSON.stringify({
            code: 0,
            data: {
              registration_open: true,
              email_enabled: true,
              invite_required: false,
              expires_seconds: 600,
            },
          }),
        );
      return new Response(
        JSON.stringify({ code: 0, data: { items: [], total: 0 } }),
      );
    }),
  );
}

async function mountLogin(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push(path);
  await router.isReady();
  const w = mount(App, { global: { plugins: [router] } });
  mounted.push(w);
  await flushPromises();
  return w;
}

it("shows only the enabled quick-login providers on the merchant login page", async () => {
  stubFetch({ qq: true, wx: true, alipay: false, telegram: false });
  const w = await mountLogin("/merchant/login");
  const quick = w.findAll("[data-quick-login]");
  const labels = quick.map((b) => b.text());
  expect(labels).toContain("QQ");
  expect(labels).toContain("微信");
  expect(labels).not.toContain("支付宝");
  expect(labels).not.toContain("Telegram");
});

it("hides the quick-login block when no provider is enabled", async () => {
  stubFetch({ qq: false, wx: false, alipay: false, telegram: false });
  const w = await mountLogin("/merchant/login");
  expect(w.findAll("[data-quick-login]").length).toBe(0);
});

it("shows the connect-completion form with the original prompt when landed with ?connect", async () => {
  stubFetch({ qq: true });
  const w = await mountLogin("/merchant/login?connect=pending-token-abc");
  expect(w.text()).toContain("请输入商户ID和密钥完成绑定和登录");
  expect(w.find('input[name="connect_merchant_id"]').exists()).toBe(true);
  expect(w.find('input[name="connect_api_key"]').exists()).toBe(true);
  expect(w.findAll("button").some((b) => b.text() === "绑定并登录")).toBe(true);
});

it("does not show the connect form without a pending state", async () => {
  stubFetch({ qq: true });
  const w = await mountLogin("/merchant/login");
  expect(w.find('input[name="connect_merchant_id"]').exists()).toBe(false);
});
