// @vitest-environment jsdom
import { beforeAll, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);
it("a late previous merchant list cannot replace a newer query on the same route", async () => {
  let release = () => {};
  let lateRead = false;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://localhost");
      let data: any = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "route-csrf",
        };
      else if (u.pathname.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (u.pathname.endsWith("/orders")) {
        const old = u.searchParams.get("merchant_id") === "1";
        if (old) await pending;
        data = {
          items: [
            {
              trade_no: old ? "late-order-one" : "current-order-two",
              merchant_id: old ? "1" : "2",
              status: "pending",
              amount: "100",
            },
          ],
          total: 1,
        };
      }
      const response = new Response(JSON.stringify({ code: 0, data }));
      if (u.searchParams.get("merchant_id") === "1") {
        // Record when the client has actually read the late body, so the
        // final assertions cannot pass merely because it was still in flight.
        const read = response.json.bind(response);
        response.json = async () => {
          const value = await read();
          lateRead = true;
          return value;
        };
      }
      return response;
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div/>" } }],
  });
  await router.push("/admin/trade/order?uid=1");
  await router.isReady();
  const w = mount(App, { global: { plugins: [router] } });
  try {
    await flushPromises();
    await router.push("/admin/trade/order?uid=2");
    // Order rows pass through a Web Crypto digest, which Node resolves on its
    // thread pool: wait for the rows instead of assuming one flush suffices.
    await vi.waitFor(() => expect(w.text()).toContain("current-order-two"));
    release();
    await vi.waitFor(() => expect(lateRead).toBe(true));
    await flushPromises();
    expect(w.text()).toContain("current-order-two");
    expect(w.text()).not.toContain("late-order-one");
  } finally {
    release();
    w.unmount();
    vi.unstubAllGlobals();
  }
});
