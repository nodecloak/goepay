// @vitest-environment jsdom
import { beforeAll, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);

it("merchant settings links to independent notice and printing pages and retains the own-login-log entry", async () => {
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
          ? {
              name: "本地商户",
              email: "local121@example.invalid",
              phone: "+65123456789012345678",
              pay: "1",
            }
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
  expect(w.get(".account-profile-facts").text()).toContain("联系邮箱");
  expect(w.get(".account-profile-facts").text()).toContain(
    "local121@example.invalid",
  );
  const phone = w.get('input[autocomplete="tel"]');
  expect(phone.attributes("maxlength")).toBe("21");
  expect((phone.element as HTMLInputElement).value).toBe(
    "+65123456789012345678",
  );
  expect(w.get('a[href="/merchant/account/notice"]').text()).toContain(
    "消息提醒设置",
  );
  expect(w.get('a[href="/merchant/account/printing"]').text()).toContain(
    "订单打印设置",
  );
  expect(w.findComponent({ name: "MerchantNoticeSettings" }).exists()).toBe(
    false,
  );
  expect(w.findComponent({ name: "PrintingSettingsPage" }).exists()).toBe(
    false,
  );
  expect(w.findAll("button").some((b) => b.text() === "登录日志")).toBe(true);
  w.unmount();
});
