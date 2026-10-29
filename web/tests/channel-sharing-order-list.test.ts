import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import ChannelSharingOrderList from "../src/components/ChannelSharingOrderList.vue";
import { ApiClient } from "../src/api";

// Pro 分账记录 (profitsharing/order) over the SERVER endpoints: KitFilterBar
// fields (搜索字段/搜索内容/分账状态/分账规则ID/日期), 统计/批量/导出 toolbar,
// Pro columns with joined 订单金额/支付方式/分账规则, per-row 查询结果/回退/
// 修改金额(disabled), server offset paging with total, 统计 from the aggregate,
// 导出 via downloadCSV.

const orderRow = {
  id: "101",
  external_no: "sharing_aaa",
  out_no: "up_aaa",
  state: "succeeded",
  share_amount: "500",
  order_amount: "10000",
  provider: "wxpay",
  method: "native",
  rule_id: "5",
  channel_id: "7",
  created_at: "2026-10-01T02:00:00Z",
};
const listBody = { items: [orderRow], total: "1", limit: 20 };
const statsBody = {
  count: "1",
  share_amount: "500",
  released_amount: "500",
  success_count: "1",
  fail_count: "0",
};

function makeRouter(initial = "/profitsharing/order") {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div/>" } },
      { path: "/profitsharing/order", component: { template: "<div/>" } },
      { path: "/:pathMatch(.*)*", component: { template: "<div/>" } },
    ],
  });
  router.push(initial);
  return router;
}

const render = (
  request: (...args: any[]) => any,
  area: "admin" | "merchant" = "admin",
  extra?: Record<string, any>,
  router = makeRouter(),
) =>
  mount(ChannelSharingOrderList, {
    props: {
      client: Object.assign(new ApiClient(area), { request, ...extra }),
      area,
    },
    global: { stubs: { Teleport: true }, plugins: [router] },
  });

function listRequest(extra?: (...args: any[]) => any) {
  return vi.fn(async (path: string, options?: any) => {
    if (extra) {
      const r = await extra(path, options);
      if (r !== undefined) return r;
    }
    if (path.includes("/orders/statistics")) return statsBody;
    if (path.includes("/orders")) return listBody;
    return {};
  });
}

async function open(w: ReturnType<typeof render>) {
  await w.get('[aria-label="商户ID"]').setValue("1000");
  await w.get("form.pa-filter").trigger("submit");
  await flushPromises();
}

it("renders Pro filters, toolbar, columns and joined fields from the server list", async () => {
  const w = render(listRequest());
  await open(w);
  // Pro filter fields.
  expect(w.find('[aria-label="搜索字段"]').exists()).toBe(true);
  expect(w.find('[aria-label="搜索内容"]').exists()).toBe(true);
  expect(w.find('[aria-label="分账状态"]').exists()).toBe(true);
  // Toolbar.
  expect(w.get('[data-action="stats"]').text()).toBe("统计");
  expect(w.get('[data-action="batch"]').text()).toBe("批量操作");
  expect(w.get('[data-action="export"]').text()).toBe("导出");
  // Columns.
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers).toEqual([
    "",
    "系统订单号",
    "分账规则",
    "支付方式",
    "订单金额",
    "分账金额",
    "时间",
    "分账状态",
    "操作",
  ]);
  // Joined columns: 订单金额 ¥100.00, 分账金额 ¥5.00, 分账规则 5, state word.
  const body = w.get("tbody").text();
  expect(body).toContain("sharing_aaa");
  expect(body).toContain("¥100.00");
  expect(body).toContain("¥5.00");
  expect(body).toContain("分账成功");
  // 修改金额 disabled (immutable).
  expect(
    w.get('[data-action="editmoney"]').attributes("disabled"),
  ).toBeDefined();
  w.unmount();
});

it("sends server filter params including 分账规则ID and offset paging", async () => {
  const seen: string[] = [];
  const w = render(
    listRequest((path: string) => {
      seen.push(path);
      return undefined;
    }),
  );
  await w.get('[aria-label="商户ID"]').setValue("1000");
  (w.vm as any).status = "unsent";
  (w.vm as any).ruleId = "5";
  await w.get("form.pa-filter").trigger("submit");
  await flushPromises();
  const listCall = seen.find(
    (p) => p.includes("/orders") && !p.includes("statistics"),
  )!;
  expect(listCall).toContain("merchant_id=1000");
  expect(listCall).toContain("status=unsent");
  expect(listCall).toContain("rule_id=5");
  expect(listCall).toContain("offset=0");
  expect(listCall).toContain("limit=20");
  w.unmount();
});

it("统计 loads the server aggregate", async () => {
  const w = render(listRequest());
  await open(w);
  await w.get('[data-action="stats"]').trigger("click");
  await flushPromises();
  const stats = w.get(".sharing-stats").text();
  expect(stats).toContain("记录数");
  expect(stats).toContain("¥5.00");
  expect(stats).toContain("100.0%");
  w.unmount();
});

it("导出 calls downloadCSV with the filter + format=csv", async () => {
  const downloadCSV = vi.fn(async () => {});
  const w = render(listRequest(), "admin", { downloadCSV });
  await open(w);
  await w.get('[data-action="export"]').trigger("click");
  await flushPromises();
  expect(downloadCSV).toHaveBeenCalledTimes(1);
  const path = String(
    (downloadCSV.mock.calls as unknown as string[][])[0]?.[0] ?? "",
  );
  expect(path).toContain("/channel-sharing/orders?");
  expect(path).toContain("format=csv");
  w.unmount();
});

it("merchant scope loads on mount without a merchant-id field", async () => {
  const w = render(listRequest(), "merchant");
  await flushPromises();
  expect(w.find('[aria-label="商户ID"]').exists()).toBe(false);
  expect(w.get("tbody").text()).toContain("sharing_aaa");
  w.unmount();
});

it("分账回退 row action opens a per-receiver modal and posts to /returns", async () => {
  const detail = {
    external_no: "sharing_aaa",
    out_no: "up_aaa",
    state: "succeeded",
    completed: true,
    funds_released: true,
    released_amount: "500",
    items: [{ receiver_id: "9", amount: "500", state: "succeeded" }],
  };
  let returnBody: any;
  const w = render(
    listRequest((path: string, options?: any) => {
      if (options?.method === "POST" && path === "/channel-sharing/returns") {
        returnBody = options.body;
        return { id: "1", state: "pending" };
      }
      // detail fetch for the opened record
      if (/\/orders\/sharing_aaa(\?|$)/.test(path)) return detail;
      return undefined;
    }),
  );
  await open(w);
  await w.get('[data-action="return"]').trigger("click");
  await flushPromises();
  // Modal fields present and receiver prefilled.
  expect(w.find('[aria-label="分账接收方"]').exists()).toBe(true);
  expect(w.find('[aria-label="回退金额（元）"]').exists()).toBe(true);
  await w.get('[aria-label="回退金额（元）"]').setValue("3.00");
  // First submit arms the confirm, second submit sends.
  await w.get("form.pa-modal-body").trigger("submit");
  await flushPromises();
  await w.get("form.pa-modal-body").trigger("submit");
  await flushPromises();
  expect(returnBody).toMatchObject({
    sharing_external_no: "sharing_aaa",
    receiver_id: "9",
    amount: "300",
  });
  expect(String(returnBody.external_no)).toMatch(/^return_[0-9a-f]{24}$/);
  expect(returnBody.merchant_id).toBe("1000");
  w.unmount();
});

it("操作台 row action emits open-flow", async () => {
  const w = render(listRequest());
  await open(w);
  await w.get('[data-action="open-flow"]').trigger("click");
  await flushPromises();
  expect(w.emitted("open-flow")).toBeTruthy();
  w.unmount();
});

it("prefills 分账规则ID from the rule_id route query (记录 deep-link)", async () => {
  const router = makeRouter("/profitsharing/order?rule_id=5");
  await router.isReady();
  const seen: string[] = [];
  const w = render(
    listRequest((path: string) => {
      if (path.includes("/orders") && !path.includes("statistics"))
        seen.push(path);
      return undefined;
    }),
    "merchant",
    undefined,
    router,
  );
  await flushPromises();
  // Prefilled from the route query and carried into the server list call.
  expect((w.vm as any).ruleId).toBe("5");
  expect(seen.some((p) => p.includes("rule_id=5"))).toBe(true);
  w.unmount();
});
