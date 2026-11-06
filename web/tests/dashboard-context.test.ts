import { beforeAll, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);
it("a delayed earlier administrator dashboard cannot replace a newly authenticated merchant overview", async () => {
  let release: () => void = () => {};
  const delayed = new Promise<void>((resolve) => {
    release = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const admin = url.startsWith("/console/v1/admin/");
      let data: unknown = { items: [], total: 0 };
      if (url.endsWith("/session"))
        data = {
          user: {
            id: admin ? "1" : "2",
            name: admin ? "admin" : "merchant",
            role: admin ? "admin" : "merchant",
            admin_class: "super",
          },
          csrf_token: admin ? "admin-csrf" : "merchant-csrf",
        };
      else if (url.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (url.endsWith("/dashboard")) {
        if (admin) await delayed;
        data = {
          merchant: {
            id: admin ? "1" : "2",
            name: admin ? "wrong-admin-payload" : "正确商户",
            enabled: true,
            pay: "1",
          },
          summary: { today_orders: "1" },
          available_money: admin ? "999.00" : "1.00",
          trend: [],
          payment_mix: [],
        };
      }
      return new Response(JSON.stringify({ code: 0, data }));
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/admin/dashboard");
  await router.isReady();
  const wrapper = mount(App, { global: { plugins: [router] } });
  try {
    await flushPromises();
    await router.push("/merchant/dashboard");
    await flushPromises();
    expect(wrapper.text()).toContain("正确商户");
    release();
    await flushPromises();
    expect(wrapper.text()).toContain("正确商户");
    expect(wrapper.text()).not.toContain("wrong-admin-payload");
  } finally {
    release();
    wrapper.unmount();
    vi.unstubAllGlobals();
  }
});
