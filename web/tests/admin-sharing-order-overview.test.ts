import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { computed } from "vue";
import { webcrypto } from "node:crypto";
import { fundsPrincipal212 } from "../src/funds-intent212";
import { createRouter, createMemoryHistory } from "vue-router";
import { ApiClient } from "../src/api";
import ChannelSharingOrderList from "../src/components/ChannelSharingOrderList.vue";

const views: VueWrapper[] = [];

function namedButton(view: VueWrapper, name: string) {
  const button = view.findAll("button").find((b) => b.text().trim() === name);
  if (!button) throw Error("Missing button " + name);
  return button;
}

it("a verified batch result retains a failed list refresh instead of showing false empty success", async () => {
  let reads = 0;
  const { view } = await setup((path, options) => {
    if (path.startsWith("/channel-sharing/order-overview?") && ++reads === 2)
      throw Error("synthetic refresh failed");
    if (path.endsWith("/query")) {
      const owner = options.body.merchant_id;
      return {
        id: owner === "751" ? "101" : "102",
        merchant_id: owner,
        external_no: "same-number",
        state: "succeeded",
      };
    }
  });
  await view.get('thead input[type="checkbox"]').setValue(true);
  await view.get('[data-action="batch"]').trigger("click");
  await flushPromises();
  await namedButton(view, "确认查询").trigger("click");
  await flushPromises();
  expect(view.text()).toContain("已核对 2 / 2 条分账记录");
  expect(view.get('[role="alert"]').text()).toContain(
    "synthetic refresh failed",
  );
});

it("changing the route before selected-query confirmation closes the old review without sending", async () => {
  const { view, router, request } = await setup();
  await view.get('thead input[type="checkbox"]').setValue(true);
  await view.get('[data-action="batch"]').trigger("click");
  await flushPromises();
  await router.replace("/admin/profitsharing/order?merchant_id=751");
  await flushPromises();
  expect(view.find('[role="dialog"]').exists()).toBe(false);
  expect(request.mock.calls.some(([p]) => p.endsWith("/query"))).toBe(false);
  expect(view.findAll("tbody tr")).toHaveLength(1);
});
afterEach(() => {
  views.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
});
const row = (owner = "751", id = "101") => ({
  id,
  merchant_id: owner,
  subchannel_id: "0",
  external_no: "same-number",
  out_no: "PS" + id,
  state: "succeeded",
  share_amount: "20",
  order_amount: "100",
  provider: "wxpay",
  method: "native",
  rule_id: "5",
  channel_id: "7",
  created_at: "2026-11-01T01:02:03Z",
});
async function setup(
  extra?: (path: string, options?: any) => unknown,
  initial = "/admin/profitsharing/order",
) {
  vi.stubGlobal("crypto", webcrypto);
  Object.defineProperty(navigator, "locks", {
    configurable: true,
    value: {
      request: async (
        _key: string,
        _options: unknown,
        action: (lock: object) => unknown,
      ) => action({}),
    },
  });
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic";
  const download = vi.spyOn(client, "downloadCSV").mockResolvedValue();
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, opts) => {
      const value = await extra?.(path, opts);
      if (value !== undefined) return value as never;
      if (path.includes("/statistics"))
        return {
          count: "2",
          share_amount: "40",
          released_amount: "0",
          success_count: "2",
          fail_count: "0",
        } as never;
      if (path.includes("/order-overview")) {
        const owner = new URL(path, "https://local.invalid").searchParams.get(
          "merchant_id",
        );
        const items = [row(), row("752", "102")].filter(
          (r) => !owner || r.merchant_id === owner,
        );
        return { items, total: items.length, limit: 20 } as never;
      }
      throw Error("Unexpected request: " + path);
    });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div/>" } }],
  });
  await router.push(initial);
  await router.isReady();
  const view = mount(ChannelSharingOrderList, {
    attachTo: document.body,
    props: { client, area: "admin" },
    global: {
      plugins: [router],
      stubs: { teleport: true },
      provide: {
        [fundsPrincipal212 as symbol]: computed(() => ({
          area: "admin",
          id: "1",
          issuer: "synthetic",
          csrf: client.csrfToken,
        })),
      },
    },
  });
  views.push(view);
  await flushPromises();
  return { view, client, request, download, router };
}
it("automatically shows both owners, preserves duplicate business numbers and real child absence", async () => {
  const { view, request } = await setup();
  expect(
    request.mock.calls.some(([p]) =>
      p.startsWith("/channel-sharing/order-overview"),
    ),
  ).toBe(true);
  expect(view.findAll("tbody tr")).toHaveLength(2);
  expect(view.findAll("thead th").map((h) => h.text())).toContain("用户ID");
  expect(view.findAll("thead th").map((h) => h.text())).toContain("子通道ID");
  expect(view.findAll("tbody tr")[0].text()).toContain("751");
  expect(view.findAll("tbody tr")[1].text()).toContain("752");
  expect(view.findAll("tbody tr")[0].text()).not.toContain("子通道ID：7");
});

it("selected cross-owner sharing queries require confirmation and preserve each original owner", async () => {
  const owners: string[] = [];
  const { view, request } = await setup((path, options) => {
    if (path.endsWith("/query")) {
      const owner = options.body.merchant_id;
      owners.push(owner);
      return {
        id: owner === "751" ? "101" : "102",
        merchant_id: owner,
        external_no: "same-number",
        state: "succeeded",
      };
    }
  });
  await view.get('thead input[type="checkbox"]').setValue(true);
  expect(
    view.get('[data-action="batch"]').attributes("disabled"),
  ).toBeUndefined();
  await view.get('[data-action="batch"]').trigger("click");
  await flushPromises();
  expect(owners).toEqual([]);
  expect(view.get('[role="dialog"]').text()).toContain("751");
  expect(view.get('[role="dialog"]').text()).toContain("752");
  await namedButton(view, "确认查询").trigger("click");
  await flushPromises();
  expect(owners).toEqual(["751", "752"]);
  expect(
    request.mock.calls.filter(([p]) =>
      p.startsWith("/channel-sharing/order-overview?"),
    ),
  ).toHaveLength(2);
  expect(view.text()).toContain("已核对 2 / 2 条分账记录");
  expect(view.find('[role="dialog"]').exists()).toBe(false);
});

it("a failed selected query stops before the next merchant and never retries automatically", async () => {
  let calls = 0;
  const { view } = await setup((path) => {
    if (path.endsWith("/query")) {
      calls++;
      throw Error("synthetic query uncertainty");
    }
  });
  await view.get('thead input[type="checkbox"]').setValue(true);
  await view.get('[data-action="batch"]').trigger("click");
  await flushPromises();
  await namedButton(view, "确认查询").trigger("click");
  await flushPromises();
  expect(calls).toBe(1);
  expect(view.text()).toContain("已核对 0 / 2 条分账记录");
  expect(view.get('[role="alert"]').text()).toContain("停止");
});

it("a selected query cannot continue into the next merchant after the CSRF session changes", async () => {
  let release: (v: unknown) => void = () => {};
  let calls = 0;
  const { view, client } = await setup((path) => {
    if (path.endsWith("/query")) {
      calls++;
      return new Promise((resolve) => {
        release = resolve;
      });
    }
  });
  await view.get('thead input[type="checkbox"]').setValue(true);
  await view.get('[data-action="batch"]').trigger("click");
  await flushPromises();
  await namedButton(view, "确认查询").trigger("click");
  await flushPromises();
  client.csrfToken = "replacement";
  release({
    id: "101",
    merchant_id: "751",
    external_no: "same-number",
    state: "succeeded",
  });
  await flushPromises();
  expect(calls).toBe(1);
  expect(view.find('[role="dialog"]').exists()).toBe(false);
});

it("clears a raw changed-session response and leaves a usable retry instead of a permanent spinner", async () => {
  let finish!: (value: unknown) => void;
  const pending = new Promise((resolve) => {
    finish = resolve;
  });
  const { view, client } = await setup((path) =>
    path.includes("/order-overview") ? pending : undefined,
  );
  client.csrfToken = "replacement";
  finish({ items: [row()], total: 1, limit: 20 });
  await flushPromises();
  expect(view.text()).not.toContain("same-number");
  expect(view.get('[role="alert"]').text()).toContain("会话");
  expect(
    view.get('[data-action="refresh"]').attributes("disabled"),
  ).toBeUndefined();
});
it("does not dispatch a row operation from data loaded under an old raw CSRF session", async () => {
  const { view, client, request } = await setup();
  client.csrfToken = "replacement";
  await view.findAll('[data-action="query"]')[0].trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, opts]) => opts?.method === "POST"),
  ).toHaveLength(0);
  expect(view.text()).not.toContain("same-number");
});
it("statistics/export use applied filters and a row query uses its original owner instead of edited drafts", async () => {
  const { view, request, download } = await setup((path, opts) =>
    opts?.method === "POST"
      ? {
          id: "101",
          merchant_id: "751",
          external_no: "same-number",
          state: "succeeded",
        }
      : undefined,
  );
  await view.get('input[aria-label="商户ID"]').setValue("751");
  await view.get("form.pa-filter").trigger("submit");
  await flushPromises();
  await view.get('input[aria-label="商户ID"]').setValue("752");
  await view.get('input[aria-label="搜索内容"]').setValue("unapplied-draft");
  await view.get('[data-action="stats"]').trigger("click");
  await flushPromises();
  expect(
    [...request.mock.calls]
      .reverse()
      .find(([p]) => p.includes("/statistics"))?.[0],
  ).toContain("merchant_id=751");
  expect(
    [...request.mock.calls]
      .reverse()
      .find(([p]) => p.includes("/statistics"))?.[0],
  ).not.toContain("unapplied-draft");
  await view.get('[data-action="export"]').trigger("click");
  await flushPromises();
  expect(download.mock.calls[0][0]).toContain("merchant_id=751");
  expect(download.mock.calls[0][0]).not.toContain("unapplied-draft");
  await view.findAll('[data-action="query"]')[0].trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.find(
      ([p, o]) => p.endsWith("/query") && o?.method === "POST",
    )?.[1]?.body,
  ).toEqual({ merchant_id: "751" });
});
it("keeps an uncertain receiver return as one original request and only allows original-result queries", async () => {
  let body: any;
  let sends = 0;
  const { view, request } = await setup((path, opts) => {
    if (path === "/channel-sharing/returns" && opts?.method === "POST") {
      opts.beforeDispatch?.();
      sends++;
      body = opts.body;
      throw Error("connection lost");
    }
    if (path.includes("/orders/same-number"))
      return {
        id: "101",
        merchant_id: "751",
        external_no: "same-number",
        completed: true,
        funds_released: true,
        items: [{ receiver_id: "9", amount: "20", state: "succeeded" }],
      };
    if (path.includes("/returns/") && path.endsWith("/query"))
      return {
        id: "301",
        sharing_id: "101",
        merchant_id: "751",
        receiver_id: "9",
        external_no: body.external_no,
        amount: body.amount,
        state: "succeeded",
      };
  });
  await view.findAll('[data-action="return"]')[0].trigger("click");
  await flushPromises();
  const amount = view
    .findAll("input")
    .find((x) => x.attributes("aria-label") === "回退金额（元）");
  expect(amount).toBeDefined();
  await amount!.setValue("0.10");
  await view.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  await view.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(sends).toBe(1);
  expect(body.merchant_id).toBe("751");
  expect(view.get('[role="dialog"]').text()).toContain("查询回退结果");
  await view.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(sends).toBe(1);
  await view
    .findAll('[role="dialog"] button')
    .find((b) => b.text() === "查询回退结果")!
    .trigger("click");
  await flushPromises();
  const check = request.mock.calls.find(
    ([p]) => p.includes("/returns/") && p.endsWith("/query"),
  );
  expect(check?.[0]).toContain(body.external_no);
  expect(check?.[1]?.body).toEqual({ merchant_id: "751" });
  expect(sends).toBe(1);
});
