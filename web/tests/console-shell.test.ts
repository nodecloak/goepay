import { beforeAll, afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import { resolvePage } from "../src/menu";
import MenuTree from "../src/components/MenuTree.vue";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);

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

it("renders the kit top bar (search trigger, Ctrl K menu, fullscreen, avatar) and no page-title row", async () => {
  const { w, router } = await setup();
  await router.push("/admin/trade/order");
  await flushPromises();
  // The shell no longer prints a page-title h1 row above the cards.
  expect(w.find(".page-heading").exists()).toBe(false);
  expect(w.findAll("h1").length).toBe(0);
  // Top-bar right cluster: search trigger with the Ctrl K hint.
  const trigger = w.get(".search-trigger");
  expect(trigger.text()).toContain("搜索");
  expect(trigger.text()).toContain("Ctrl K");
  // Fullscreen + theme tool buttons present.
  expect(w.find('.topbar-tools button[aria-label="全屏"]').exists()).toBe(true);
  expect(
    w.findAll(".topbar-tools .theme-toggle").length,
  ).toBeGreaterThanOrEqual(1);
  // Ctrl K opens the menu search palette.
  window.dispatchEvent(
    new KeyboardEvent("keydown", { key: "k", ctrlKey: true }),
  );
  await flushPromises();
  expect(w.find(".search-menu").exists()).toBe(true);
  // It lists role-visible leaf menu entries; filtering narrows them.
  const orders = resolvePage("admin", "/trade/order")!.title;
  expect(w.find(".search-list").exists()).toBe(true);
  await w.get(".search-input").setValue(orders);
  await flushPromises();
  const items = w.findAll(".search-item");
  expect(items.length).toBeGreaterThanOrEqual(1);
  expect(items.every((i) => i.text().includes(orders))).toBe(true);
  // Enter navigates to the active entry.
  await w.get(".search-input").trigger("keydown", { key: "Enter" });
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/admin/trade/order");
});
it("opens the avatar menu with 账户安全 and 退出登录", async () => {
  const { w } = await setup();
  const avatar = w.get(".topbar-avatar .avatar");
  // Avatar shows the first character of the display name.
  expect(avatar.text()).toBe("本");
  await avatar.trigger("click");
  await flushPromises();
  const menu = w.get(".avatar-menu");
  expect(menu.text()).toContain("账户安全");
  expect(menu.text()).toContain("退出登录");
});
it("gives refresh a home at the right end of the list table tools", async () => {
  const { w, router, fetcher } = await setup();
  await router.push("/admin/trade/order");
  await flushPromises();
  const refresh = w.get(".table-tools .table-refresh");
  expect(refresh.attributes("aria-label")).toContain("刷新");
  const before = fetcher.mock.calls.length;
  await refresh.trigger("click");
  await flushPromises();
  expect(fetcher.mock.calls.length).toBeGreaterThan(before);
});

it("redirects a logged-in admin off /admin/login to the dashboard (or the requested return)", async () => {
  const fetcher = vi.fn(async (input: string) => {
    const data = input.endsWith("/session")
      ? {
          user: {
            id: "1",
            name: "本地测试",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "synthetic-csrf",
        }
      : input.endsWith("/dashboard")
        ? { orders: 1 }
        : { items: [], total: 0 };
    return new Response(JSON.stringify({ code: 0, data }));
  });
  vi.stubGlobal("fetch", fetcher);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div/>" } }],
  });
  // Landing on the login URL while a session is live must not strand the shell.
  await router.push("/admin/login");
  await router.isReady();
  const w = mount(App, { global: { plugins: [router] } });
  wrappers.push(w);
  await flushPromises();
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/admin/dashboard");
  // A safe ?return= is honoured.
  await router.push("/admin/login?return=/admin/trade/order");
  await flushPromises();
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/admin/trade/order");
  // An unsafe/foreign return falls back to the dashboard.
  await router.push("/admin/login?return=https://evil.example/x");
  await flushPromises();
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/admin/dashboard");
});
