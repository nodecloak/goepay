// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import App from "../src/App.vue";
import actualPlatform from "../e2e/globalaccess179-http-fixture.json";
import actualSession from "../e2e/merchant174-key-login-http-fixture.json";
import actualDashboard from "../e2e/subchannel168b-dashboard-fixture.json";
import actualSite from "../e2e/fixtures/site191-http.json";

const metadataPath =
  "/console/v1/public/login-captcha?role=merchant&mode=password";
const initPath = "/console/v1/public/login-captcha/init";
const username = "merchant205",
  password = "synthetic-app205-password",
  id = "a".repeat(32),
  token = "t".repeat(43),
  challenge = "c".repeat(32);
type Call = {
  path: string;
  method: string;
  body?: Record<string, unknown>;
  signal?: AbortSignal | null;
};
type Options = {
  route?: string;
  version?: 0 | 1;
  metadata?: () => Promise<Response>;
  init?: (call: Call) => Promise<Response>;
  login?: (call: Call) => Promise<Response>;
  session?: (role: string, count: number) => Promise<Response>;
};
const mounted: VueWrapper[] = [],
  unexpected: string[][] = [];

it("205 confines an unauthenticated merchant return path to the dedicated login document", async () => {
  const f = await setup({ route: "/merchant/trade/order?status=0#unpaid" });
  expect(f.router.currentRoute.value.path).toBe("/merchant/login");
  expect(f.router.currentRoute.value.query.return).toBe(
    "/merchant/trade/order?status=0#unpaid",
  );
  captchaPanel(f.wrapper);
});

it("205 leaves the login document after an existing durable session is read", async () => {
  const f = await setup({
    route: "/merchant/login?return=%2Fmerchant%2Fdashboard%3Ftime%3Dtoday",
    session: async () => response(actualSession.response.data),
  });
  expect(f.router.currentRoute.value.path).toBe("/merchant/dashboard");
  expect(f.router.currentRoute.value.query.time).toBe("today");
  expect(f.calls.filter((call) => call.path === metadataPath)).toEqual([]);
  expect(f.wrapper.text()).not.toContain("页面不存在");
});

it("205 carries only a neutral reauthentication instruction across a password-change document boundary", async () => {
  const f = await setup({ route: "/merchant/login?reauth=password", metadata: async () => response({ enabled: false }) });
  expect(f.wrapper.text()).toContain("请使用当前有效密码重新登录");
  expect(f.wrapper.text()).not.toContain("密码已修改");
  expect(f.credentialPosts()).toEqual([]);
  expect(f.initPosts()).toEqual([]);
});

it("205 rejects an external or recursive login return after session recovery", async () => {
  for (const target of [
    "https://outside.example.invalid/merchant/dashboard",
    "//outside.example.invalid/merchant/dashboard",
    "/merchant/login?return=/merchant/dashboard",
    "/merchant/not-a-menu",
  ]) {
    const f = await setup({
      route: "/merchant/login?return=" + encodeURIComponent(target),
      session: async () => response(actualSession.response.data),
    });
    expect(f.router.currentRoute.value.fullPath).toBe("/merchant/dashboard");
    expect(f.calls.filter((call) => call.path === metadataPath)).toEqual([]);
  }
});
function response(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({
      code: status === 200 ? 0 : -1,
      message: status === 200 ? "" : "controlled login205 rejection",
      data,
    }),
    { status },
  );
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
function challengeData(version: 0 | 1) {
  return {
    version,
    gt: id,
    challenge: version === 0 ? challenge : "",
    new_captcha: version === 0,
    success: 1,
    captcha_token: token,
  };
}
function nativeProof(version: 0 | 1) {
  return version === 0
    ? {
        geetest_challenge: challenge,
        geetest_validate: "d".repeat(32),
        geetest_seccode: "controlled-app205",
      }
    : {
        captcha_id: id,
        lot_number: challenge,
        pass_token: "controlled-app205",
        gen_time: "1793000000",
        captcha_output: "controlled-output-app205",
      };
}
function sdk205(version: 0 | 1) {
  let success = () => {},
    closed = () => {};
  const widget = {
    onReady(callback: () => void) {
      queueMicrotask(callback);
      return widget;
    },
    onSuccess(callback: () => void) {
      success = callback;
      return widget;
    },
    onClose(callback: () => void) {
      closed = callback;
      return widget;
    },
    onError(_callback: () => void) {
      return widget;
    },
    getValidate: () => nativeProof(version),
    showCaptcha: vi.fn(),
    verify: vi.fn(),
    destroy: vi.fn(),
  };
  const initialize = vi.fn(
    (_options: unknown, callback: (value: typeof widget) => void) =>
      callback(widget),
  );
  vi.stubGlobal("initGeetest", initialize);
  vi.stubGlobal("initGeetest4", initialize);
  return {
    widget,
    initialize,
    success: () => success(),
    close: () => closed(),
  };
}
async function setup(options: Options = {}) {
  const calls: Call[] = [],
    rejected: string[] = [],
    sessionReads: Record<string, number> = {};
  unexpected.push(rejected);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const call: Call = {
        path: String(input),
        method: init?.method ?? "GET",
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
        signal: init?.signal,
      };
      calls.push(call);
      const { path, method } = call;
      if (method === "GET" && path === "/console/v1/public/site")
        return response(actualSite.default);
      if (method === "GET" && path === "/console/v1/public/layout")
        return response({ zhuce: "", footer: "", format: "html" });
      if (method === "GET" && path === "/console/v1/public/merchant-access")
        return response(actualPlatform.public_default);
      const session = /^\/console\/v1\/(merchant|admin)\/session$/.exec(path);
      if (method === "GET" && session) {
        const role = session[1]!;
        sessionReads[role] = (sessionReads[role] ?? 0) + 1;
        return options.session
          ? options.session(role, sessionReads[role]!)
          : response(null, 401);
      }
      if (method === "GET" && path === metadataPath)
        return options.metadata
          ? options.metadata()
          : response({ enabled: true });
      if (method === "POST" && path === initPath)
        return options.init
          ? options.init(call)
          : response(challengeData(options.version ?? 1));
      if (
        method === "POST" &&
        [
          "/console/v1/merchant/login",
          "/console/v1/admin/login",
          "/console/v1/merchant/key-login",
        ].includes(path)
      )
        return options.login ? options.login(call) : response(null, 401);
      if (method === "GET" && path === "/console/v1/merchant/dashboard")
        return response(actualDashboard.group100);
      // Every fetch is intercepted. Optional endpoints are also explicit: a
      // missing route cannot silently look like a successful future method.
      rejected.push(`${method} ${path}`);
      return response(null, 503);
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<span />" } }],
  });
  await router.push(options.route ?? "/merchant/dashboard");
  await router.isReady();
  const wrapper = mount(App, {
    attachTo: document.body,
    global: { plugins: [router] },
  });
  mounted.push(wrapper);
  await flushPromises();
  return {
    wrapper,
    router,
    calls,
    credentialPosts: () =>
      calls.filter(
        (call) =>
          call.method === "POST" &&
          /\/console\/v1\/(merchant|admin)\/(login|key-login)$/.test(call.path),
      ),
    initPosts: () =>
      calls.filter((call) => call.path === initPath && call.method === "POST"),
    metadataReads: () => calls.filter((call) => call.path === metadataPath),
  };
}
function captchaPanel(wrapper: VueWrapper) {
  expect(
    wrapper.find('[aria-label="登录验证码"]').exists(),
    "real App must mount the merchant password CAPTCHA gate",
  ).toBe(true);
}
async function fillPassword(wrapper: VueWrapper, name = username) {
  await wrapper.get('[name="username"]').setValue(name);
  await wrapper.get('[name="password"]').setValue(password);
}
async function submit(wrapper: VueWrapper) {
  await wrapper.get("form").trigger("submit");
  await flushPromises();
}
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  for (const calls of unexpected.splice(0))
    expect(calls, "unexpected API request in App login fixture").toEqual([]);
});

it.each([0, 1] as const)(
  "205 App V%s completes init and native proof before forwarding credentials through the real ApiClient",
  async (version) => {
    const sdk = sdk205(version),
      storage = vi.spyOn(Storage.prototype, "setItem"),
      f = await setup({ version });
    captchaPanel(f.wrapper);
    expect(f.metadataReads()).toHaveLength(1);
    await fillPassword(f.wrapper);
    await submit(f.wrapper);
    expect(f.initPosts()).toHaveLength(1);
    expect(f.initPosts()[0]?.body).toEqual({ username });
    expect(f.credentialPosts()).toEqual([]);
    expect(
      version === 0 ? sdk.widget.verify : sdk.widget.showCaptcha,
    ).toHaveBeenCalledTimes(1);
    sdk.success();
    await flushPromises();
    expect(f.credentialPosts()).toHaveLength(1);
    const call = f.credentialPosts()[0]!;
    expect(call.path).toBe("/console/v1/merchant/login");
    expect(call.body).toEqual({
      username,
      password,
      captcha_token: token,
      ...nativeProof(version),
    });
    expect(f.calls.indexOf(f.initPosts()[0]!)).toBeLessThan(
      f.calls.indexOf(call),
    );
    expect(storage).not.toHaveBeenCalled();
  },
);

it.each(["unavailable", "malformed"])(
  "205 App rejects merchant credential submission when CAPTCHA metadata is %s",
  async (kind) => {
    const f = await setup({
      metadata: async () =>
        kind === "unavailable"
          ? response(null, 503)
          : response({ enabled: "false" }),
    });
    captchaPanel(f.wrapper);
    await fillPassword(f.wrapper);
    await submit(f.wrapper);
    expect(f.credentialPosts()).toEqual([]);
    expect(f.initPosts()).toEqual([]);
    expect(f.wrapper.find('[role="alert"]').exists()).toBe(true);
  },
);

it("205 App preserves the original disabled merchant password body", async () => {
  const f = await setup({ metadata: async () => response({ enabled: false }) });
  captchaPanel(f.wrapper);
  await fillPassword(f.wrapper);
  await submit(f.wrapper);
  expect(f.initPosts()).toEqual([]);
  expect(f.credentialPosts()).toHaveLength(1);
  expect(f.credentialPosts()[0]?.body).toEqual({ username, password });
});

it("205 App administrator password login remains independent of merchant CAPTCHA metadata", async () => {
  const f = await setup({
    route: "/admin/dashboard",
    metadata: async () => response(null, 503),
  });
  await fillPassword(f.wrapper, "admin205");
  await submit(f.wrapper);
  expect(f.metadataReads()).toEqual([]);
  expect(f.initPosts()).toEqual([]);
  expect(f.credentialPosts()).toHaveLength(1);
  expect(f.credentialPosts()[0]).toMatchObject({
    path: "/console/v1/admin/login",
    body: { username: "admin205", password },
  });
});

it("205 App merchant key mode keeps the existing typed login path when password CAPTCHA metadata fails", async () => {
  const f = await setup({ metadata: async () => response(null, 503) });
  await f.wrapper.get('[data-login-mode="key"]').trigger("click");
  await flushPromises();
  const reads = f.metadataReads().length;
  await f.wrapper
    .get('[name="merchant_id"]')
    .setValue(actualSession.merchant_id);
  await f.wrapper.get('[name="api_key"]').setValue("synthetic-key205");
  await submit(f.wrapper);
  expect(f.metadataReads()).toHaveLength(reads);
  expect(f.initPosts()).toEqual([]);
  expect(f.credentialPosts()).toHaveLength(1);
  expect(f.credentialPosts()[0]).toMatchObject({
    path: "/console/v1/merchant/key-login",
    body: {
      merchant_id: actualSession.merchant_id,
      api_key: "synthetic-key205",
      code: "",
    },
  });
});

it("205 App discards a late proof init after the bound username changes", async () => {
  const pending = deferred<Response>(),
    sdk = sdk205(1),
    f = await setup({ init: async () => pending.promise });
  captchaPanel(f.wrapper);
  await fillPassword(f.wrapper);
  await submit(f.wrapper);
  expect(f.initPosts()).toHaveLength(1);
  // Exercise the actual v-model input event, including a late event already
  // queued when the UI disables the field for verification.
  const field = f.wrapper.get<HTMLInputElement>('[name="username"]');
  field.element.value = "changed205";
  field.element.dispatchEvent(new Event("input", { bubbles: true }));
  await flushPromises();
  pending.resolve(response(challengeData(1)));
  await flushPromises();
  sdk.success();
  await flushPromises();
  expect(f.credentialPosts()).toEqual([]);
  expect(sdk.initialize).not.toHaveBeenCalled();
});

it("205 App rejects a cancelled password widget's late proof after switching to key mode", async () => {
  const sdk = sdk205(1),
    f = await setup();
  captchaPanel(f.wrapper);
  await fillPassword(f.wrapper);
  await submit(f.wrapper);
  expect(sdk.initialize).toHaveBeenCalledTimes(1);
  sdk.close();
  await flushPromises();
  await f.wrapper.get('[data-login-mode="key"]').trigger("click");
  await flushPromises();
  expect(f.wrapper.find('[name="api_key"]').exists()).toBe(true);
  sdk.success();
  await flushPromises();
  expect(f.credentialPosts()).toEqual([]);
  await f.wrapper
    .get('[name="merchant_id"]')
    .setValue(actualSession.merchant_id);
  await f.wrapper.get('[name="api_key"]').setValue("new-key205");
  await submit(f.wrapper);
  expect(f.credentialPosts()).toHaveLength(1);
  expect(f.credentialPosts()[0]?.path).toBe("/console/v1/merchant/key-login");
  expect(f.credentialPosts()[0]?.body).not.toHaveProperty("captcha_token");
});

it("205 App leaving the merchant login discards a late proof without writing into the admin screen", async () => {
  const sdk = sdk205(1),
    f = await setup();
  captchaPanel(f.wrapper);
  await fillPassword(f.wrapper);
  await submit(f.wrapper);
  expect(sdk.initialize).toHaveBeenCalledTimes(1);
  await f.router.push("/admin/dashboard");
  await flushPromises();
  sdk.success();
  await flushPromises();
  expect(f.wrapper.text()).toContain("管理员登录");
  expect(f.credentialPosts()).toEqual([]);
  expect(sdk.widget.destroy).toHaveBeenCalled();
});

it("205 App unknown password login result blocks a new proof/write until read-only session verification", async () => {
  const sdk = sdk205(1),
    f = await setup({
      login: async () => {
        throw new TypeError("controlled lost response205");
      },
      session: async (role, count) =>
        role === "merchant" && count > 1
          ? response(actualSession.response.data)
          : response(null, 401),
    });
  captchaPanel(f.wrapper);
  await fillPassword(f.wrapper);
  await submit(f.wrapper);
  sdk.success();
  await flushPromises();
  expect(f.credentialPosts()).toHaveLength(1);
  expect(f.wrapper.find('[data-action="check-login"]').exists()).toBe(true);
  expect(
    f.wrapper.get('form button[type="submit"]').attributes("disabled"),
  ).toBeDefined();
  await submit(f.wrapper);
  expect(f.credentialPosts()).toHaveLength(1);
  expect(f.initPosts()).toHaveLength(1);
  await f.wrapper.get('[data-action="check-login"]').trigger("click");
  await flushPromises();
  expect(
    f.calls.filter((call) => call.path === "/console/v1/merchant/session"),
  ).toHaveLength(2);
  expect(f.credentialPosts()).toHaveLength(1);
  expect(f.wrapper.text()).toContain(actualSession.response.data.user.name);
});

it("205 App retains existing unknown key-login session recovery without requesting a password proof", async () => {
  const f = await setup({
    login: async () => {
      throw new TypeError("controlled lost key response205");
    },
    session: async (role, count) =>
      role === "merchant" && count > 1
        ? response(actualSession.response.data)
        : response(null, 401),
  });
  await f.wrapper.get('[data-login-mode="key"]').trigger("click");
  await flushPromises();
  await f.wrapper
    .get('[name="merchant_id"]')
    .setValue(actualSession.merchant_id);
  await f.wrapper.get('[name="api_key"]').setValue("synthetic-key205");
  await submit(f.wrapper);
  expect(f.wrapper.find('[data-action="check-login"]').exists()).toBe(true);
  expect(f.initPosts()).toEqual([]);
  await submit(f.wrapper);
  expect(f.credentialPosts()).toHaveLength(1);
  await f.wrapper.get('[data-action="check-login"]').trigger("click");
  await flushPromises();
  expect(
    f.calls.filter((call) => call.path === "/console/v1/merchant/session"),
  ).toHaveLength(2);
  expect(f.credentialPosts()).toHaveLength(1);
});
