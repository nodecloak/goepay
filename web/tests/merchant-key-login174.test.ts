import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import { beforeAll, expect, it, vi, afterEach } from "vitest";
import App from "../src/App.vue";
import actualPlatform from "../e2e/globalaccess179-http-fixture.json";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);
afterEach(() => vi.unstubAllGlobals());
const mid = "9007199254740993";
const ok = (data: unknown) =>
  new Response(JSON.stringify({ code: 0, data }), { status: 200 });
async function setup(
  handler?: (url: string, options?: RequestInit) => Promise<Response>,
) {
  const fetcher = vi.fn(async (url: string, o?: RequestInit) => {
    if (url === "/console/v1/public/merchant-access")
      return new Response(
        JSON.stringify({
          code: 0,
          message: "",
          data: actualPlatform.public_default,
        }),
      );
    if (url.endsWith("/session"))
      return new Response(JSON.stringify({ code: -1, message: "请登录" }), {
        status: 401,
      });
    return handler ? handler(url, o) : ok({});
  });
  vi.stubGlobal("fetch", fetcher);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/merchant/dashboard");
  const w = mount(App, { global: { plugins: [router] } });
  await flushPromises();
  return { w, router, fetcher };
}
async function fill(w: ReturnType<typeof mount>) {
  await w.get('[data-login-mode="key"]').trigger("click");
  await w.get('[name="merchant_id"]').setValue(mid);
  await w.get('[name="api_key"]').setValue("only-in-body");
}
it("merchant key mode is explicit and secrets clear before a pending POST; one submission only", async () => {
  let resolve!: (r: Response) => void;
  const f = await setup(async (url) =>
    url.endsWith("/key-login") ? new Promise((r) => (resolve = r)) : ok({}),
  );
  await fill(f.w);
  await f.w.get("form").trigger("submit");
  expect(f.w.get('[name="api_key"]').element).toHaveProperty("value", "");
  await f.w.get("form").trigger("submit");
  expect(
    f.fetcher.mock.calls.filter((x) => x[0].endsWith("/key-login")),
  ).toHaveLength(1);
  expect(
    f.fetcher.mock.calls.find((x) => x[0].endsWith("/key-login"))?.[1]?.body,
  ).toBe(
    JSON.stringify({ merchant_id: mid, api_key: "only-in-body", code: "" }),
  );
  resolve(
    ok({
      user: { id: mid, name: "Owner", role: "merchant" },
      csrf_token: "c".repeat(43),
    }),
  );
  await flushPromises();
  expect(f.w.find('[name="api_key"]').exists()).toBe(false);
  f.w.unmount();
});
it("a malformed or lost success forbids blind resubmit and offers a read-only session check", async () => {
  const f = await setup(async () =>
    ok({
      user: { id: "2", name: "Other", role: "merchant" },
      csrf_token: "c".repeat(43),
    }),
  );
  await fill(f.w);
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.w.text()).toContain("重新检查登录状态");
  expect(
    f.w.get('form button[type="submit"]').attributes("disabled"),
  ).toBeDefined();
  await f.w.get("form").trigger("submit");
  expect(
    f.fetcher.mock.calls.filter((x) => x[0].endsWith("/key-login")),
  ).toHaveLength(1);
  await f.w.get('[data-action="check-login"]').trigger("click");
  await flushPromises();
  expect(
    f.fetcher.mock.calls.filter((x) => x[0].endsWith("/session")),
  ).toHaveLength(2);
  f.w.unmount();
});
it("late merchant login cannot enter a different area and leaving key mode erases the draft", async () => {
  let resolve!: (r: Response) => void;
  const f = await setup(async () => new Promise((r) => (resolve = r)));
  await fill(f.w);
  await f.w.get('[data-login-mode="password"]').trigger("click");
  await f.w.get('[data-login-mode="key"]').trigger("click");
  expect(f.w.get('[name="api_key"]').element).toHaveProperty("value", "");
  await f.w.get('[name="api_key"]').setValue("key");
  await f.w.get("form").trigger("submit");
  await f.router.push("/admin/dashboard");
  await flushPromises();
  resolve(
    ok({
      user: { id: mid, name: "WrongArea", role: "merchant" },
      csrf_token: "c".repeat(43),
    }),
  );
  await flushPromises();
  expect(f.w.text()).toContain("管理员登录");
  expect(f.w.text()).not.toContain("WrongArea");
  expect(f.w.find('[data-login-mode="key"]').exists()).toBe(false);
  f.w.unmount();
});
