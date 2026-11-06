import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { computed } from "vue";
import { webcrypto } from "node:crypto";
import { fundsPrincipal212 } from "../src/funds-intent212";
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
  merchant_id: "1000",
  subchannel_id: "0",
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
beforeEach(() => {
  vi.stubGlobal("crypto", webcrypto);
  Object.defineProperty(navigator, "locks", {
    configurable: true,
    value: {
      request: async (
        _key: string,
        _opts: unknown,
        fn: (lock: object) => unknown,
      ) => fn({}),
    },
  });
});
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

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
) => {
  const client = Object.assign(new ApiClient(area), { request, ...extra });
  client.csrfToken = "synthetic";
  return mount(ChannelSharingOrderList, {
    props: {
      client,
      area,
    },
    global: {
      stubs: { Teleport: true },
      plugins: [router],
      provide: {
        [fundsPrincipal212 as symbol]: computed(() => ({
          area,
          id: area === "admin" ? "1" : "1000",
          issuer: "synthetic",
          csrf: client.csrfToken,
        })),
      },
    },
  });
};

function listRequest(extra?: (...args: any[]) => any) {
  return vi.fn(async (path: string, options?: any) => {
    if (extra) {
      const r = await extra(path, options);
      if (r !== undefined) return r;
    }
    if (
      path.includes("/orders/statistics") ||
      path.includes("/order-overview/statistics")
    )
      return statsBody;
    if (path.includes("/order-overview")) {
      const q = new URL(path, "https://local.invalid").searchParams;
      return {
        ...listBody,
        items: [
          {
            ...orderRow,
            merchant_id: q.get("merchant_id") || "1000",
            rule_id: q.get("rule_id") || orderRow.rule_id,
          },
        ],
      };
    }
    if (path.includes("/orders")) return listBody;
    return {};
  });
}

async function open(w: ReturnType<typeof render>) {
  await w.get('[aria-label="商户ID"]').setValue("1000");
  await w.get("form.pa-filter").trigger("submit");
  await flushPromises();
}

it("accepts the server's numeric total (the live API shape) without a format error", async () => {
  const request = vi.fn(async (path: string) =>
    path.includes("/orders") ? { items: [], total: 0, limit: 20 } : {},
  );
  const w = render(request, "merchant");
  await flushPromises();
  expect(request.mock.calls.some(([p]) => String(p).includes("/orders"))).toBe(
    true,
  );
  expect(w.text()).not.toContain("响应格式错误");
  w.unmount();
});

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
    "用户ID",
    "系统订单号",
    "分账规则",
    "支付方式(通道ID)",
    "子通道ID",
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
  expect(w.get('[title="支付通道ID：7"]').text()).toBe("(7)");
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
    (p) =>
      p.includes("/order-overview") &&
      p.includes("merchant_id=1000") &&
      !p.includes("statistics"),
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
  expect(path).toContain("/channel-sharing/order-overview?");
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
    id: "101",
    merchant_id: "1000",
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
        options.beforeDispatch?.();
        return {
          id: "1",
          sharing_id: "101",
          merchant_id: "1000",
          receiver_id: returnBody.receiver_id,
          external_no: returnBody.external_no,
          amount: returnBody.amount,
          state: "pending",
        };
      }
      // detail fetch for the opened record
      if (/\/orders\/sharing_aaa(\?|$)/.test(path)) return detail;
      return undefined;
    }),
  );
  await open(w);
  await w.get('[data-action="return"]').trigger("click");
  await flushPromises();
  // Opening derives the intent scope with a Web Crypto digest, which Node
  // resolves on its thread pool; submits are ignored until loading ends.
  await vi.waitFor(() => expect(w.text()).not.toContain("正在读取原分账记录"));
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

it("prefills the admin merchant and rule from a record link without rounding either id", async () => {
  const merchantID = "9007199254740995",
    ruleID = "9007199254740993";
  const router = makeRouter(
    `/admin/profitsharing/order?merchant_id=${merchantID}&rule_id=${ruleID}`,
  );
  await router.isReady();
  const request = listRequest(),
    w = render(request, "admin", undefined, router);
  await flushPromises();
  expect(
    (w.get('[aria-label="商户ID"]').element as HTMLInputElement).value,
  ).toBe(merchantID);
  expect((w.vm as any).ruleId).toBe(ruleID);
  const qs = new URL(request.mock.calls[0]![0], "https://example.invalid")
    .searchParams;
  expect(qs.get("merchant_id")).toBe(merchantID);
  expect(qs.get("rule_id")).toBe(ruleID);
  expect(
    request.mock.calls.every(
      ([, options]) => !options?.method || options.method === "GET",
    ),
  ).toBe(true);
  w.unmount();
});

it("never sends a merchant query owner supplied to a merchant record link", async () => {
  const router = makeRouter(
    "/merchant/profitsharing/order?merchant_id=9007199254740995&rule_id=5",
  );
  await router.isReady();
  const request = listRequest(),
    w = render(request, "merchant", undefined, router);
  await flushPromises();
  expect(request.mock.calls.length).toBeGreaterThan(0);
  for (const [path] of request.mock.calls)
    expect(
      new URL(path, "https://example.invalid").searchParams.has("merchant_id"),
    ).toBe(false);
  expect(w.find('[aria-label="商户ID"]').exists()).toBe(false);
  expect((w.vm as any).ruleId).toBe("5");
  w.unmount();
});

it("switches both admin deep-link filters on the same component and clears them on reset", async () => {
  const router = makeRouter(
    "/admin/profitsharing/order?merchant_id=1000&rule_id=5",
  );
  await router.isReady();
  const request = listRequest(),
    w = render(request, "admin", undefined, router);
  await flushPromises();
  await router.push("/admin/profitsharing/order?merchant_id=2000&rule_id=6");
  await flushPromises();
  expect(
    (w.get('[aria-label="商户ID"]').element as HTMLInputElement).value,
  ).toBe("2000");
  expect((w.vm as any).ruleId).toBe("6");
  const path = request.mock.calls.at(-1)![0];
  expect(path).toContain("merchant_id=2000");
  expect(path).toContain("rule_id=6");
  const before = request.mock.calls.length;
  await w.get("form.pa-filter").trigger("reset");
  await flushPromises();
  expect(router.currentRoute.value.query.merchant_id).toBeUndefined();
  expect(router.currentRoute.value.query.rule_id).toBeUndefined();
  expect(
    (w.get('[aria-label="商户ID"]').element as HTMLInputElement).value,
  ).toBe("");
  expect((w.vm as any).ruleId).toBe("");
  expect(request.mock.calls.length).toBe(before + 1);
  w.unmount();
});

it.each([
  "merchant_id=9223372036854775808&rule_id=5",
  "merchant_id=1&merchant_id=2&rule_id=5",
  "merchant_id=1000&rule_id=1.5",
  "channel_id=0",
  "subchannel_id=1&subchannel_id=2",
])("does not broaden invalid admin link filters: %s", async (query) => {
  const router = makeRouter("/admin/profitsharing/order?" + query);
  await router.isReady();
  const request = listRequest(),
    w = render(request, "admin", undefined, router);
  await flushPromises();
  expect(request).not.toHaveBeenCalled();
  expect(w.find('[role="alert"]').exists()).toBe(true);
  await w.get('[data-action="refresh"]').trigger("click");
  await flushPromises();
  expect(request).not.toHaveBeenCalled();
  w.unmount();
});

it("does not let an earlier merchant read overwrite a newly selected record scope", async () => {
  let finish!: (value: unknown) => void;
  const oldResult = new Promise((done) => {
    finish = done;
  });
  const router = makeRouter(
    "/admin/profitsharing/order?merchant_id=1000&rule_id=5",
  );
  await router.isReady();
  const request = vi.fn(async (path: string) =>
    path.includes("merchant_id=1000")
      ? oldResult
      : {
          ...listBody,
          items: [
            {
              ...orderRow,
              merchant_id: "2000",
              rule_id: "6",
              external_no: "new_scope",
            },
          ],
        },
  );
  const w = render(request, "admin", undefined, router);
  await flushPromises();
  await router.push("/admin/profitsharing/order?merchant_id=2000&rule_id=6");
  await flushPromises();
  expect(w.text()).toContain("new_scope");
  finish({ ...listBody, items: [{ ...orderRow, external_no: "old_scope" }] });
  await flushPromises();
  expect(w.text()).toContain("new_scope");
  expect(w.text()).not.toContain("old_scope");
  w.unmount();
});
