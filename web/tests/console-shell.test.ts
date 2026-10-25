import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import { resolvePage } from "../src/menu";
import MenuTree from "../src/components/MenuTree.vue";

const wrappers: ReturnType<typeof mount>[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount());
  vi.unstubAllGlobals();
});
async function setup(area: "admin" | "merchant" = "admin") {
  const fetcher = vi.fn(async (input: string, _init?: RequestInit) => {
    const data = input.endsWith("/session")
      ? {
          user: {
            id: area === "admin" ? "1" : "1000",
            name: "本地测试",
            role: area,
            admin_class: "super",
          },
          csrf_token: "synthetic-csrf",
        }
      : input.endsWith("/dashboard")
        ? {
            orders: 1,
            paid_orders: 1,
            available_money: "1.00",
            frozen_money: "0.00",
          }
        : { items: [], total: 0 };
    return new Response(JSON.stringify({ code: 0, data }));
  });
  vi.stubGlobal("fetch", fetcher);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div/>" } }],
  });
  await router.push(`/${area}/dashboard`);
  await router.isReady();
  const w = mount(App, { global: { plugins: [router] } });
  wrappers.push(w);
  await flushPromises();
  return { w, router, fetcher };
}
it("collapses the desktop rail without losing accessible menu names or route selection", async () => {
  const { w, router } = await setup();
  expect(w.get(".workspace").attributes("data-area")).toBe("admin");
  await w.get('button[aria-label="收起侧栏"]').trigger("click");
  expect(w.get(".workspace").classes()).toContain("sidebar-collapsed");
  expect(
    w.get('button[aria-label="展开侧栏"]').attributes("aria-expanded"),
  ).toBe("false");
  await router.push("/admin/trade/order");
  await flushPromises();
  expect(w.get('a[aria-current="page"]').attributes("href")).toBe(
    "/admin/trade/order",
  );
  expect(w.get('[aria-label="面包屑"]').text()).toContain(
    resolvePage("admin", "/trade/order")!.title,
  );
});
it("tracks selected admin work tabs, closes the active tab and clears them on area/session change", async () => {
  const { w, router } = await setup();
  const home = resolvePage("admin", "/dashboard")!.title;
  const orders = resolvePage("admin", "/trade/order")!.title;
  await router.push("/admin/trade/order");
  await flushPromises();
  expect(w.get('[role="tab"][aria-selected="true"]').text()).toContain(orders);
  expect(w.findAll('[role="tab"]')).toHaveLength(2);
  await w.get(`button[aria-label="关闭页签：${orders}"]`).trigger("click");
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/admin/dashboard");
  expect(w.get('[role="tab"][aria-selected="true"]').text()).toContain(home);
  await router.push("/merchant/dashboard");
  await flushPromises();
  expect(w.find('[role="tablist"]').exists()).toBe(false);
  expect(w.get(".workspace").attributes("data-area")).toBe("merchant");
  expect(localStorage.length + sessionStorage.length).toBe(0);
});
it("closes the mobile drawer on navigation and Escape without posting requests", async () => {
  const { w, router, fetcher } = await setup();
  const button = w.get('button[aria-label="展开菜单"]');
  await button.trigger("click");
  expect(button.attributes("aria-expanded")).toBe("true");
  await router.push("/admin/trade/order");
  await flushPromises();
  expect(button.attributes("aria-expanded")).toBe("false");
  await button.trigger("click");
  window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
  await flushPromises();
  expect(button.attributes("aria-expanded")).toBe("false");
  expect(
    fetcher.mock.calls.every(
      ([, init]) => !init || !((init as RequestInit).method === "POST"),
    ),
  ).toBe(true);
});
it("expands the selected collapsed menu group without closing its current children", async () => {
  const { router } = await setup();
  const w = mount(MenuTree, {
    props: {
      area: "admin",
      active: "/trade/order",
      collapsed: true,
      nodes: [
        {
          id: "Trade",
          title: "交易管理",
          path: "/trade",
          hidden: false,
          roles: [],
          children: [
            {
              id: "Orders",
              title: "订单管理",
              path: "/trade/order",
              hidden: false,
              roles: [],
              children: [],
            },
          ],
        },
      ],
    },
    global: { plugins: [router] },
  });
  wrappers.push(w);
  expect(w.get("details").attributes("open")).toBeDefined();
  await w.get("summary").trigger("click");
  expect(w.emitted("expand")).toHaveLength(1);
  expect(w.get("details").attributes("open")).toBeDefined();
});
it("uses the full mobile drawer when a previously collapsed desktop shell is resized", async () => {
  const { w } = await setup();
  await w.get('button[aria-label="收起侧栏"]').trigger("click");
  expect(w.get(".workspace").classes()).toContain("sidebar-collapsed");
  vi.stubGlobal("innerWidth", 375);
  window.dispatchEvent(new Event("resize"));
  await flushPromises();
  await w.get('button[aria-label="展开菜单"]').trigger("click");
  expect(w.get(".workspace").classes()).not.toContain("sidebar-collapsed");
  expect(w.get("aside").attributes("inert")).toBeUndefined();
});
