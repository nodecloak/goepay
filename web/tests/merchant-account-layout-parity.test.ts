// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import MerchantAccountSettings from "../src/components/MerchantAccountSettings.vue";
import { ApiClient } from "../src/api";

async function fixture(ownSession = true) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/merchant/account/settings");
  await router.isReady();
  const requests = vi.spyOn(ApiClient.prototype, "request");
  const w = mount(MerchantAccountSettings, {
    props: { client: new ApiClient("merchant"), ownSession, merchantId: "1" },
    global: {
      plugins: [router],
      stubs: {
        AccountPage: {
          template:
            '<section class="panel"><h2>账户资料</h2><button @click="$emit(\'login-log\')">登录日志</button></section>',
        },
        MerchantPersonalSettings: {
          props: ["ownSession"],
          template:
            '<section class="panel personal-settings"><h2>个人功能设置</h2><span>{{ ownSession }}</span></section>',
        },
        MerchantAPISwitches: {
          props: ["ownSession"],
          template:
            '<section class="panel api-switches"><h2>接口与登录功能设置</h2><span>{{ ownSession }}</span></section>',
        },
        BuyerFeeSettings104: {
          props: ["allowed"],
          template:
            '<section class="panel buyer-fee104"><h2>手续费承担设置</h2><span>{{ allowed }}</span></section>',
        },
        TotpSettings: {
          template: '<section class="panel"><h2>动态验证码保护</h2></section>',
        },
        AccountLoginLogDialog: {
          template:
            '<div role="dialog" aria-label="登录日志"><button @click="$emit(\'close\')">关闭登录日志</button></div>',
        },
        MerchantNoticeSettings: {
          template: "<section data-embedded-notice>消息推送整表</section>",
        },
        PrintingSettingsPage: {
          template: "<section data-embedded-printing>小票打印整表</section>",
        },
      },
    },
  });
  await flushPromises();
  return { w, router, requests };
}

it("groups account functions after a full-width profile and links to independent notice/printing nodes without mounting their forms", async () => {
  const { w, router, requests } = await fixture();
  expect(w.get(".merchant-account-profile h2").text()).toBe("账户资料");
  const functions = w.get('[aria-label="联系方式与功能设置"]');
  expect(functions.findAll("h2, h3").map((h) => h.text())).toContain(
    "联系方式与功能设置",
  );
  expect(functions.text()).toContain("个人功能设置");
  expect(functions.text()).toContain("手续费承担设置");
  expect(functions.text()).toContain("接口与登录功能设置");
  expect(w.find("[data-embedded-notice]").exists()).toBe(false);
  expect(w.find("[data-embedded-printing]").exists()).toBe(false);
  const links = w.get('[aria-label="消息与打印设置"]');
  expect(links.get('a[href="/merchant/account/notice"]').text()).toContain(
    "消息提醒设置",
  );
  expect(links.get('a[href="/merchant/account/printing"]').text()).toContain(
    "订单打印设置",
  );
  await links.get('a[href="/merchant/account/notice"]').trigger("click");
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/merchant/account/notice");
  expect(requests).not.toHaveBeenCalled();
  w.unmount();
});

it("keeps own login logs and security available while delegated settings retain their existing restrictions", async () => {
  const { w } = await fixture();
  await w.get("button").trigger("click");
  expect(w.find('[role="dialog"]').exists()).toBe(true);
  expect(w.text()).toContain("动态验证码保护");
  await w.setProps({ ownSession: false });
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).not.toContain("动态验证码保护");
  expect(w.text()).toContain("安全设置和本人登录日志需要商户本人登录后查看。");
  for (const selector of [
    ".personal-settings",
    ".api-switches",
    ".buyer-fee104",
  ])
    expect(w.get(selector).text()).toContain("false");
  w.unmount();
});
