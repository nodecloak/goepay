// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";

it("the original merchant settings includes existing notice, printing and own-login-log entry", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const p = new URL(url, "http://localhost").pathname;
      const data = p.endsWith("/session")
        ? {
            user: { id: "1", name: "本地商户", role: "merchant" },
            csrf_token: "settings121-csrf",
          }
        : p.endsWith("/account/profile")
          ? { name: "本地商户", email: "local121@example.invalid", pay: "1" }
          : p.endsWith("/mfa/status")
            ? { enabled: false }
            : { items: [], total: 0 };
      return new Response(JSON.stringify({ code: 0, data }));
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/merchant/account/settings");
  await router.isReady();
  const w = mount(App, {
    global: {
      plugins: [router],
      stubs: {
        MerchantNoticeSettings: { template: "<section>消息推送</section>" },
        PrintingSettingsPage: { template: "<section>小票打印设置</section>" },
      },
    },
  });
  await flushPromises();
  expect(w.text()).toContain("消息推送");
  expect(w.text()).toContain("小票打印设置");
  expect(w.findAll("button").some((b) => b.text() === "登录日志")).toBe(true);
  w.unmount();
});
