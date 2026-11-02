import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import App from "../src/App.vue";
import { kycInfo233 } from "./fixtures/kyc233";
import site from "../e2e/fixtures/site191-http.json";
const mounted: VueWrapper[] = [];
const response = (data: unknown, status = 200) =>
  new Response(
    JSON.stringify({ code: status === 200 ? 0 : -1, message: "", data }),
    { status },
  );
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function setup(wrongMerchant = false) {
  let loggedIn = false;
  const id = "a".repeat(32),
    calls: { path: string; method: string; body?: unknown }[] = [];
  const session = {
    user: {
      id: wrongMerchant ? "1001" : "1000",
      role: "merchant",
      name: "测试商户233",
    },
    csrf_token: "session233",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const path = String(input),
        method = init?.method || "GET",
        body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, method, body });
      if (path === "/console/v1/public/site") return response(site.default);
      if (path === "/console/v1/public/layout")
        return response({ zhuce: "", footer: "", format: "html" });
      if (path === "/console/v1/public/merchant-access")
        return response({ close_keylogin: 1, user_refund: 1 });
      if (path === "/console/v1/public/connect/options")
        return response({
          providers: { qq: false, wx: false, alipay: false, telegram: false },
        });
      if (path.startsWith("/console/v1/public/login-captcha?"))
        return response({ enabled: false });
      if (path === "/console/v1/merchant/session")
        return loggedIn ? response(session) : response(null, 401);
      if (path === "/console/v1/merchant/login") {
        loggedIn = true;
        return response(session);
      }
      if (path === "/console/v1/merchant/account/certificate/info")
        return response(
          kycInfo233({
            cert_open: 3,
            attempt_id: wrongMerchant ? "b".repeat(32) : id,
            status: "pending",
            auth_code_ready: !wrongMerchant,
          }),
        );
      if (path === "/console/v1/merchant/account/certificate/query")
        return response(
          kycInfo233({ cert: 1, cert_open: 3, status: "passed" }),
        );
      throw Error(`unexpected fixture request ${method} ${path}`);
    }),
  );
  const storage = vi.spyOn(Storage.prototype, "setItem");
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<span />" } }],
  });
  await router.push("/merchant/account/certificate");
  await router.isReady();
  const wrapper = mount(App, {
    attachTo: document.body,
    global: { plugins: [router] },
  });
  mounted.push(wrapper);
  await flushPromises();
  return { wrapper, router, calls, storage, id };
}
it("resumes server-held OAuth authorization after merchant login using an empty-code query", async () => {
  const f = await setup();
  expect(f.router.currentRoute.value.path).toBe("/merchant/login");
  expect(f.router.currentRoute.value.query.return).toBe(
    "/merchant/account/certificate",
  );
  await f.wrapper.get('[name="username"]').setValue("testmerchant233");
  await f.wrapper.get('[name="password"]').setValue("synthetic-password233");
  await f.wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(f.router.currentRoute.value.fullPath).toBe(
    "/merchant/account/certificate",
  );
  expect(
    f.calls.filter((c) => c.path.endsWith("/certificate/query")),
  ).toHaveLength(0);
  const query = f.wrapper
    .findAll("button")
    .find((b) => b.text() === "查询认证结果")!;
  await query.trigger("click");
  await flushPromises();
  expect(f.calls.filter((c) => c.path.endsWith("/certificate/query"))).toEqual([
    {
      path: "/console/v1/merchant/account/certificate/query",
      method: "POST",
      body: { attempt_id: f.id, state: "", auth_code: "" },
    },
  ]);
  for (const call of f.storage.mock.calls)
    expect(JSON.stringify(call)).not.toMatch(/auth_code|certification-return/);
});
it("does not offer an OAuth query to a different merchant without its own server-held authorization", async () => {
  const f = await setup(true);
  await f.wrapper.get('[name="username"]').setValue("othermerchant233");
  await f.wrapper.get('[name="password"]').setValue("synthetic-password233");
  await f.wrapper.get("form").trigger("submit");
  await flushPromises();
  const query = f.wrapper
    .findAll("button")
    .find((b) => b.text() === "查询认证结果")!;
  expect(query.attributes("disabled")).toBeDefined();
  expect(
    f.calls.filter((c) => c.path.endsWith("/certificate/query")),
  ).toHaveLength(0);
});
