import { beforeAll, describe, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);
describe("console login", () => {
  it("authorizes the server initial admin role for the dashboard", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              code: 0,
              data: url.endsWith("/session")
                ? {
                    user: { id: "1", name: "Admin", role: "admin" },
                    csrf_token: "token",
                  }
                : {
                    orders: 1,
                    paid_orders: 1,
                    available_money: "1.00",
                    frozen_money: "0.00",
                  },
            }),
          ),
        ),
      ),
    );
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/:pathMatch(.*)*", component: { template: "<div />" } },
      ],
    });
    await router.push("/admin/dashboard");
    await router.isReady();
    const wrapper = mount(App, { global: { plugins: [router] } });
    await flushPromises();
    expect(wrapper.findAll(".stat")).toHaveLength(4);
    wrapper.unmount();
    vi.unstubAllGlobals();
  });
  it("keeps users on login when credentials are rejected and shows the server error", async () => {
    const fetcher = vi
      .fn()
      .mockImplementation((url: string, options?: RequestInit) => {
        if (url === "/console/v1/admin/session")
          return Promise.resolve(
            new Response(JSON.stringify({ code: 401, message: "请登录" }), {
              status: 401,
            }),
          );
        if (url === "/console/v1/admin/login" && options?.method === "POST")
          return Promise.resolve(
            new Response(
              JSON.stringify({ code: 401, message: "用户名或密码错误" }),
              { status: 401 },
            ),
          );
        if (url === "/console/v1/public/layout")
          return Promise.resolve(
            new Response(
              JSON.stringify({
                code: 0,
                data: { zhuce: "", footer: "", format: "html" },
              }),
            ),
          );
        return Promise.resolve(
          new Response(
            JSON.stringify({
              code: -1,
              message: "optional public data unavailable",
            }),
            { status: 503 },
          ),
        );
      });
    vi.stubGlobal("fetch", fetcher);
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/:pathMatch(.*)*", component: { template: "<div />" } },
      ],
    });
    await router.push("/admin/dashboard");
    await router.isReady();
    const wrapper = mount(App, { global: { plugins: [router] } });
    await flushPromises();
    await wrapper.get("input[name=username]").setValue("operator");
    await wrapper.get("input[name=password]").setValue("bad-password");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(wrapper.text()).toContain("用户名或密码错误");
    expect(wrapper.find("input[name=password]").exists()).toBe(true);
    wrapper.unmount();
    vi.unstubAllGlobals();
  });
});
