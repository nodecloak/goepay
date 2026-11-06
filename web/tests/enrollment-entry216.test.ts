// @vitest-environment jsdom
import { beforeAll, afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import App from "../src/App.vue";
import PublicEnrollment from "../src/components/PublicEnrollment.vue";
import { capturePromotionPrefill } from "../src/referral";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);

const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  document.body.innerHTML = "";
  window.history.replaceState({}, "", "/");
  capturePromotionPrefill();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function ok(data: unknown) {
  return new Response(JSON.stringify({ code: 0, message: "", data }), {
    status: 200,
  });
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => (resolve = yes));
  return { promise, resolve };
}
const capabilities = (open = true) => ({
  registration_open: open,
  email_enabled: true,
  sms_enabled: false,
  invite_required: false,
  review_required: false,
  expires_seconds: 600,
  verify_type: "",
});
const merchantSession = () => ({
  user: { id: "10001", name: "已登录商户", role: "merchant" },
  csrf_token: "entry216-local-session",
});

async function setup(
  location: string,
  options: {
    authenticated?: boolean;
    session?: Promise<Response>;
    capability?: Promise<Response>;
    registrationOpen?: boolean;
    promotion?: string;
  } = {},
) {
  window.history.replaceState(
    {},
    "",
    options.promotion
      ? "/merchant/login#promotion_token=" + options.promotion
      : "/",
  );
  capturePromotionPrefill();
  const calls: { path: string; method: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const path = String(input),
        method = init?.method || "GET";
      calls.push({ path, method });
      if (path.endsWith("/session")) {
        if (options.session) return options.session;
        return options.authenticated
          ? ok(merchantSession())
          : new Response(JSON.stringify({ code: -1, message: "请先登录" }), {
              status: 401,
            });
      }
      if (path === "/console/v1/public/capabilities")
        return options.capability || ok(capabilities(options.registrationOpen));
      if (path === "/console/v1/public/layout")
        return ok({ zhuce: "", footer: "", format: "html" });
      if (path === "/console/v1/public/merchant-access")
        return ok({ close_keylogin: 1, user_refund: 0 });
      if (path === "/console/v1/merchant/dashboard")
        return ok({
          orders: 0,
          paid_orders: 0,
          available_money: "0.00",
          frozen_money: "0.00",
        });
      return new Response(
        JSON.stringify({
          code: -1,
          message: "Optional public data unavailable",
        }),
        { status: 503 },
      );
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push(location);
  await router.isReady();
  const w = mount(App, { global: { plugins: [router] } });
  mounted.push(w);
  await flushPromises();
  return { w, router, calls };
}

function backToLogin(w: VueWrapper) {
  const button = w.findAll("button").find((b) => b.text() === "返回登录");
  expect(
    button,
    "the real registration form provides return to login",
  ).toBeDefined();
  return button!;
}

describe("homepage registration entry", () => {
  it("opens the real registration form from the homepage URL without sending a registration", async () => {
    const { w, calls } = await setup("/merchant/login?enroll=register");
    expect(w.findComponent(PublicEnrollment).exists()).toBe(true);
    expect(w.find('input[name="enrollment-email"]').exists()).toBe(true);
    expect(w.find('input[name="password"]').exists()).toBe(false);
    expect(
      calls.filter((r) => r.path.endsWith("/public/capabilities")),
    ).toHaveLength(1);
    expect(calls.every((r) => r.method === "GET")).toBe(true);
  });

  it("consumes the entry mode when returning to login while preserving the ordinary destination", async () => {
    const { w, router } = await setup(
      "/merchant/login?enroll=register&return=%2Fmerchant%2Faccount%2Fapi-info#entry",
    );
    await backToLogin(w).trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.path).toBe("/merchant/login");
    expect(router.currentRoute.value.query).toEqual({
      return: "/merchant/account/api-info",
    });
    expect(router.currentRoute.value.hash).toBe("#entry");
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
    expect(w.find('input[name="password"]').exists()).toBe(true);
    await router.push(
      "/merchant/login?return=%2Fmerchant%2Faccount%2Fapi-info",
    );
    await flushPromises();
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
  });

  it("handles same-area navigation into registration and clears it on an invalid mode or admin route", async () => {
    const { w, router } = await setup("/merchant/login");
    await router.push("/merchant/login?enroll=register");
    await flushPromises();
    expect(w.findComponent(PublicEnrollment).exists()).toBe(true);
    await router.push("/merchant/login?enroll=recover");
    await flushPromises();
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
    await router.push("/admin/login?enroll=register");
    await flushPromises();
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
    expect(w.find('input[name="password"]').exists()).toBe(true);
  });

  it.each([
    "/merchant/login?enroll=register&enroll=register",
    "/merchant/login?enroll=unknown",
    "/merchant/login?enroll=https%3A%2F%2Felsewhere.invalid",
    "/merchant/account/api-info?enroll=register",
    "/admin/login?enroll=register",
  ])("rejects ambiguous or misplaced entry mode: %s", async (location) => {
    const { w, router } = await setup(location);
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
    expect(router.currentRoute.value.fullPath).not.toContain(
      "https://elsewhere.invalid",
    );
  });

  it("does not expose registration to an existing session or follow an external return target", async () => {
    const probe = deferred<Response>();
    const { w, router, calls } = await setup(
      "/merchant/login?enroll=register&return=https%3A%2F%2Felsewhere.invalid",
      { session: probe.promise },
    );
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
    probe.resolve(ok(merchantSession()));
    await flushPromises();
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
    expect(calls.some((r) => r.path.endsWith("/public/capabilities"))).toBe(
      false,
    );
    expect(router.currentRoute.value.path).toBe("/merchant/dashboard");
    expect(router.currentRoute.value.query).toEqual({});
  });

  it("preserves the original promotion prefill registration flow", async () => {
    const token = "rp_" + "a".repeat(43);
    const { w, calls } = await setup("/merchant/login", { promotion: token });
    expect(w.findComponent(PublicEnrollment).exists()).toBe(true);
    expect(
      w.get<HTMLInputElement>('input[aria-label="推广令牌"]').element.value,
    ).toBe(token);
    expect(window.location.hash).toBe("");
    expect(calls.every((r) => r.method === "GET")).toBe(true);
    await backToLogin(w).trigger("click");
    await flushPromises();
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
  });

  it("keeps registration disabled according to the server capabilities", async () => {
    const { w, calls } = await setup("/merchant/login?enroll=register", {
      registrationOpen: false,
    });
    expect(w.findComponent(PublicEnrollment).exists()).toBe(true);
    expect(w.text()).toContain("当前未开放商户注册");
    await w
      .get('input[name="enrollment-email"]')
      .setValue("closed-entry@example.test");
    const send = w.findAll("button").find((b) => b.text() === "发送验证码");
    expect(send!.attributes("disabled")).toBeDefined();
    const submit = w.findAll("button").find((b) => b.text() === "完成注册");
    expect(submit!.attributes("disabled")).toBeDefined();
    expect(calls.every((r) => r.method === "GET")).toBe(true);
  });

  it("cannot reopen a closed entry when its old capabilities reply arrives later", async () => {
    const capability = deferred<Response>();
    const { w, router } = await setup("/merchant/login?enroll=register", {
      capability: capability.promise,
    });
    expect(w.findComponent(PublicEnrollment).exists()).toBe(true);
    await backToLogin(w).trigger("click");
    await flushPromises();
    capability.resolve(ok(capabilities()));
    await flushPromises();
    expect(router.currentRoute.value.query.enroll).toBeUndefined();
    expect(w.findComponent(PublicEnrollment).exists()).toBe(false);
    expect(w.find('input[name="password"]').exists()).toBe(true);
  });
});
