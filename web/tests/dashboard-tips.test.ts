// @vitest-environment jsdom
import { it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import DashboardOverview from "../src/components/DashboardOverview.vue";

const RouterLinkStub = {
  props: ["to"],
  template: '<a :href="to" class="router-link"><slot /></a>',
};

const stubs = {
  RouterLink: RouterLinkStub,
  DailyWelcome: true,
  RichContent: true,
  ShellIcon: true,
};

const merchantData = {
  merchant: { id: "1", name: "测试商户", pay: "1", enabled: true },
  available_money: "0.00",
  summary: {},
  trend: [],
  payment_mix: [],
};

function mountDashboard(tips: unknown) {
  const client = new ApiClient("merchant");
  const request = vi.spyOn(client, "request").mockResolvedValue({ tips });
  const w = mount(DashboardOverview, {
    props: { data: merchantData, area: "merchant", merchantId: "1", client },
    global: { stubs },
  });
  return { client, request, w };
}

beforeEach(() => {
  sessionStorage.clear();
});

it("fetches tips from the read-only endpoint and renders a banner per tip with an action link", async () => {
  const { request, w } = mountDashboard([
    {
      type: "warning",
      title: "您还未设置结算账户，请尽快设置以便正常结算",
      link: "/account/settings",
      link_text: "前往设置",
    },
    {
      type: "error",
      title: "您的支付功能已被限制，请联系平台客服处理",
      link: "/account/settings",
      link_text: "查看账户",
    },
  ]);
  await flushPromises();
  expect(request).toHaveBeenCalledWith("/dashboard/tips");
  const banners = w.findAll(".dashboard-tip");
  expect(banners.length).toBe(2);
  expect(banners[0].text()).toContain("您还未设置结算账户");
  // Action link routes into the merchant console page that fixes it.
  const action = banners[0].find("a.router-link");
  expect(action.attributes("href")).toBe("/merchant/account/settings");
  expect(action.text()).toBe("前往设置");
  // Error tone carries the error class.
  expect(banners[1].classes()).toContain("tip-error");
});

it("closing a banner hides it and persists the dismissal for the session", async () => {
  const { w } = mountDashboard([
    {
      type: "warning",
      title: "您还未绑定手机号码，请尽快绑定以保障账户安全",
      link: "/account/settings",
      link_text: "前往绑定",
    },
  ]);
  await flushPromises();
  expect(w.findAll(".dashboard-tip").length).toBe(1);
  await w.find(".dashboard-tip .tip-close").trigger("click");
  expect(w.findAll(".dashboard-tip").length).toBe(0);
  expect(sessionStorage.getItem("merchant_dashboard_tips_closed")).toContain(
    "您还未绑定手机号码",
  );
});

it("renders no banners and does not break when the tips call fails", async () => {
  const client = new ApiClient("merchant");
  const request = vi
    .spyOn(client, "request")
    .mockRejectedValue(new Error("boom"));
  const w = mount(DashboardOverview, {
    props: { data: merchantData, area: "merchant", merchantId: "1", client },
    global: { stubs },
  });
  await flushPromises();
  expect(request).toHaveBeenCalled();
  expect(w.findAll(".dashboard-tip").length).toBe(0);
  // The dashboard overview itself still rendered (welcome panel present).
  expect(w.find(".welcome").exists()).toBe(true);
});

it("does not fetch tips for the admin dashboard", async () => {
  const client = new ApiClient("admin");
  const request = vi.spyOn(client, "request").mockResolvedValue({ tips: [] });
  mount(DashboardOverview, {
    props: {
      data: { summary: {}, trend: [], payment_mix: [] },
      area: "admin",
      adminClass: "super",
      client,
    },
    global: { stubs },
  });
  await flushPromises();
  expect(request).not.toHaveBeenCalled();
});
