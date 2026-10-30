import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import ChannelSharingRuleList from "../src/components/ChannelSharingRuleList.vue";
import { ApiClient } from "../src/api";

// Pro 分账规则 (profitsharing/receiver) parity: list columns, 新增 toolbar,
// row 编辑/开启/关闭 actions, the add/edit dialog fields, and the ordered
// create-receiver -> bind -> publish-rule save over GOEPAY's channel-sharing API.

const caps = {
  merchant_id: "1000",
  available: true,
  reason: "",
  channels: [
    {
      channel_id: "7",
      provider: "wxpay",
      product: "wxpay_v3_direct",
      methods: ["native"],
      receiver_types: ["MERCHANT_ID", "PERSONAL_OPENID"],
      return_supported: true,
      finish_supported: true,
    },
  ],
};
const boundReceiver = {
  id: "9",
  merchant_id: "1000",
  channel_id: "7",
  provider: "wxpay",
  product: "wxpay_v3_direct",
  type: "MERCHANT_ID",
  account_masked: "***1234",
  name_masked: "测***",
  state: "bound",
};
const rule = {
  id: "5",
  merchant_id: "1000",
  channel_id: "7",
  revision: "3",
  provider: "wxpay",
  app_id: "wx",
  product: "wxpay_v3_direct",
  enabled: true,
  minimum_amount: "0",
  methods: ["native"],
  items: [
    { receiver_id: "9", mode: "rate", rate_bps: "2000", fixed_amount: "0" },
  ],
};

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div/>" } },
      { path: "/profitsharing/order", component: { template: "<div/>" } },
      { path: "/:pathMatch(.*)*", component: { template: "<div/>" } },
    ],
  });
}

const render = (
  request: (...args: any[]) => any,
  area: "admin" | "merchant" = "admin",
  router = makeRouter(),
) =>
  mount(ChannelSharingRuleList, {
    props: { client: Object.assign(new ApiClient(area), { request }), area },
    global: { stubs: { Teleport: true }, plugins: [router] },
  });

async function open(w: ReturnType<typeof render>) {
  await w.get('[aria-label="商户ID"]').setValue("1000");
  await w.get("form.pa-filter").trigger("submit");
  await flushPromises();
}

function listRequest(extra?: (...args: any[]) => any) {
  return vi.fn(async (path: string, options?: any) => {
    if (extra) {
      const r = await extra(path, options);
      if (r !== undefined) return r;
    }
    if (path.includes("channels")) return caps;
    if (
      path.includes("/receivers?") ||
      /\/receivers$/.test(path.split("?")[0]!)
    )
      return { items: [boundReceiver], next_cursor: "0" };
    if (path.includes("/rules/7")) return rule;
    return { items: [], next_cursor: "0" };
  });
}

it("renders the Pro 分账规则 list with channel, receiver+ratio, status and 新增 toolbar", async () => {
  const w = render(listRequest());
  await open(w);
  // Columns / headers.
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers).toEqual([
    "ID",
    "支付通道",
    "分账接收方+分账比例",
    "状态",
    "操作",
  ]);
  // 新增 toolbar.
  expect(w.get('[data-action="create"]').text()).toBe("新增");
  // Rule id, masked receiver + ratio, status word.
  const body = w.get("tbody").text();
  expect(body).toContain("***1234");
  expect(body).toContain("20.00%");
  expect(body).toContain("已开启");
  // Row actions.
  expect(w.get('[data-action="edit"]').text()).toBe("编辑");
  expect(w.get('[data-action="toggle"]').text()).toBe("关闭");
  w.unmount();
});

it("opens the add dialog with Pro fields and saves via create -> bind -> publish in order", async () => {
  const calls: string[] = [];
  const request = listRequest(async (path: string, options?: any) => {
    if (options?.method === "POST") {
      calls.push(path);
      if (/\/receivers$/.test(path))
        return {
          ...boundReceiver,
          id: "11",
          account_masked: "***9999",
          state: "draft",
        };
      if (/\/receivers\/11\/bind$/.test(path))
        return { ...boundReceiver, id: "11", state: "bound" };
      if (/\/rules\/7$/.test(path)) return { ...rule, revision: "4" };
    }
    return undefined;
  });
  const w = render(request);
  await open(w);
  await w.get('[data-action="create"]').trigger("click");
  await flushPromises();
  // Pro dialog fields present.
  expect(w.find('[aria-label="支付通道"]').exists()).toBe(true);
  expect(w.find('[aria-label="订单最小金额"]').exists()).toBe(true);
  expect(w.find('[aria-label="接收方账号"]').exists()).toBe(true);
  expect(w.find('[aria-label="接收方姓名"]').exists()).toBe(true);
  expect(w.find('[aria-label="分账比例"]').exists()).toBe(true);
  // Fill one receiver and save.
  await w.get('[aria-label="接收方账号"]').setValue("1900000109");
  await w.get('[aria-label="接收方姓名"]').setValue("测试公司");
  await w.get('[aria-label="分账比例"]').setValue("20");
  await w.get("form.pa-modal-body").trigger("submit");
  await flushPromises();
  // Ordered side effects: create receiver, then bind, then publish rule.
  expect(calls.filter((p) => /\/receivers$/.test(p)).length).toBe(1);
  expect(calls).toContain("/channel-sharing/receivers/11/bind");
  expect(calls.some((p) => /\/rules\/7$/.test(p))).toBe(true);
  expect(calls.indexOf("/channel-sharing/receivers")).toBeLessThan(
    calls.indexOf("/channel-sharing/receivers/11/bind"),
  );
  expect(calls.indexOf("/channel-sharing/receivers/11/bind")).toBeLessThan(
    calls.findIndex((p) => /\/rules\/7$/.test(p)),
  );
  w.unmount();
});

it("rejects a ratio total over 100% before any write", async () => {
  const request = listRequest(async (_path: string, options?: any) => {
    if (options?.method === "POST") throw new Error("must not write");
    return undefined;
  });
  const w = render(request);
  await open(w);
  await w.get('[data-action="create"]').trigger("click");
  await flushPromises();
  await w.get('[aria-label="接收方账号"]').setValue("acct");
  await w.get('[aria-label="分账比例"]').setValue("150");
  await w.get("form.pa-modal-body").trigger("submit");
  await flushPromises();
  expect(w.text()).toContain("分账比例总和不能超过100%");
  w.unmount();
});

it("merchant scope loads on mount without a merchant-id field", async () => {
  const w = render(listRequest(), "merchant");
  await flushPromises();
  expect(w.find('[aria-label="商户ID"]').exists()).toBe(false);
  expect(w.get("tbody").text()).toContain("***1234");
  w.unmount();
});

it("toggles a rule off through the disable endpoint", async () => {
  let disabled = false;
  const request = listRequest(async (path: string, options?: any) => {
    if (options?.method === "POST" && /\/rules\/7\/disable$/.test(path)) {
      disabled = true;
      return { ...rule, enabled: false };
    }
    if (path.includes("/rules/7")) return { ...rule, enabled: !disabled };
    return undefined;
  });
  const w = render(request);
  await open(w);
  await w.get('[data-action="toggle"]').trigger("click");
  await flushPromises();
  expect(disabled).toBe(true);
  w.unmount();
});

it("记录 row link navigates to 分账记录 filtered by the rule id", async () => {
  const router = makeRouter();
  const w = render(listRequest(), "admin", router);
  await open(w);
  const record = w.get('[data-action="record"]');
  expect(record.text()).toBe("记录");
  await record.trigger("click");
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/profitsharing/order");
  expect(router.currentRoute.value.query.rule_id).toBe("5");
  w.unmount();
});

it("the grey 新增 says why: query first, then the server's no-channel reason", async () => {
  const none = {
    ...caps,
    available: false,
    reason: "当前未配置支持渠道分账的通道；模拟通道不执行真实分账",
    channels: [],
  };
  const request = listRequest(async (path: string) =>
    path.includes("channels") ? none : undefined,
  );
  const w = render(request);
  const button = () => w.get('[data-action="create"]');
  const hint = () => w.find(".create-hint");
  expect(button().attributes("disabled")).toBeDefined();
  expect(hint().text()).toBe("请先输入商户ID并查询");
  await open(w);
  expect(button().attributes("disabled")).toBeDefined();
  expect(hint().text()).toBe(none.reason);
  // The reason is said once, beside the button.
  expect(w.text().split(none.reason).length - 1).toBe(1);
  w.unmount();
});

it("no hint once a supported channel is loaded", async () => {
  const w = render(listRequest());
  await open(w);
  expect(
    w.get('[data-action="create"]').attributes("disabled"),
  ).toBeUndefined();
  expect(w.find(".create-hint").exists()).toBe(false);
  w.unmount();
});
