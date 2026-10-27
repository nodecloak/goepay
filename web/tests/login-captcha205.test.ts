// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import LoginCaptcha205 from "../src/components/LoginCaptcha205.vue";

type Gate = {
  verify(): Promise<Record<string, string>>;
  invalidate(): void;
  reload(): Promise<void>;
};
const mounted: VueWrapper[] = [];
const id = "a".repeat(32),
  token = "t".repeat(43),
  challenge = "c".repeat(32);
function response(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({ code: status === 200 ? 0 : -1, message: "", data }),
    { status },
  );
}
function data(version: 0 | 1) {
  return {
    version,
    gt: id,
    challenge: version === 0 ? challenge : "",
    new_captcha: version === 0,
    success: 1,
    captcha_token: token,
  };
}
function native(version: 0 | 1) {
  return version === 0
    ? {
        geetest_challenge: challenge,
        geetest_validate: "d".repeat(32),
        geetest_seccode: "controlled205",
      }
    : {
        captcha_id: id,
        lot_number: challenge,
        pass_token: "controlled205",
        gen_time: "1793000000",
        captcha_output: "controlled-output205",
      };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
function start(props: Record<string, unknown> = {}) {
  const wrapper = mount(LoginCaptcha205, {
    props: { active: true, username: "merchant205", ...props },
    attachTo: document.body,
  });
  mounted.push(wrapper);
  return wrapper;
}
function gate(wrapper: VueWrapper) {
  return wrapper.vm as unknown as Gate;
}
function setupSDK(version: 0 | 1, proof: unknown = native(version)) {
  let success = () => {},
    close = () => {},
    fail = () => {};
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
      close = callback;
      return widget;
    },
    onError(callback: () => void) {
      fail = callback;
      return widget;
    },
    getValidate: () => proof,
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
    close: () => close(),
    fail: () => fail(),
  };
}
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("205 default inactive gate makes no public request and cannot authorize a login", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const wrapper = start({ active: false });
  await expect(gate(wrapper).verify()).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
  expect(wrapper.text()).toBe("");
});
it("205 disabled metadata preserves password login without CAPTCHA or a TestPay intent", async () => {
  const fetch = vi.fn(async (_path: string, _options?: RequestInit) =>
    response({ enabled: false }),
  );
  vi.stubGlobal("fetch", fetch);
  const wrapper = start();
  await flushPromises();
  await expect(gate(wrapper).verify()).resolves.toEqual({});
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(fetch.mock.calls[0]?.[0]).toBe(
    "/console/v1/public/login-captcha?role=merchant&mode=password",
  );
  expect(wrapper.text()).toContain("无需人机验证");
});
it.each([
  null,
  { enabled: 1 },
  { enabled: "false" },
  { enabled: false, captcha_key: "forbidden" },
])("205 malformed metadata stays closed (%j)", async (metadata) => {
  const fetch = vi.fn(async () => response(metadata));
  vi.stubGlobal("fetch", fetch);
  const wrapper = start();
  await flushPromises();
  await expect(gate(wrapper).verify()).rejects.toThrow();
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(wrapper.find('[role="alert"]').exists()).toBe(true);
});
it.each([0, 1] as const)(
  "205 V%s verifies the bound username and emits only native fields plus wrapper without storage",
  async (version) => {
    const sdk = setupSDK(version),
      writes = vi.spyOn(Storage.prototype, "setItem");
    const fetch = vi.fn(async (path: string) =>
      path.endsWith("/init")
        ? response(data(version))
        : response({ enabled: true }),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = start();
    await flushPromises();
    const proof = gate(wrapper).verify();
    await flushPromises();
    expect(
      version === 0 ? sdk.widget.verify : sdk.widget.showCaptcha,
    ).toHaveBeenCalledTimes(1);
    sdk.success();
    await expect(proof).resolves.toEqual({
      captcha_token: token,
      ...native(version),
    });
    const init = fetch.mock.calls.find(([path]) => path.endsWith("/init"));
    expect(init).toBeDefined();
    const calls = vi.mocked(globalThis.fetch).mock.calls;
    expect(JSON.parse(calls[1]?.[1]?.body as string)).toEqual({
      username: "merchant205",
    });
    expect(calls[1]?.[1]).toMatchObject({
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      redirect: "error",
    });
    expect(
      calls.every(([path]) =>
        String(path).startsWith("/console/v1/public/login-captcha"),
      ),
    ).toBe(true);
    expect(writes).not.toHaveBeenCalled();
    expect(sdk.widget.destroy).toHaveBeenCalledTimes(1);
  },
);
it("205 widget cancellation never returns a proof or submits account credentials", async () => {
  const sdk = setupSDK(1),
    fetch = vi.fn(async (path: string) =>
      response(path.endsWith("/init") ? data(1) : { enabled: true }),
    );
  vi.stubGlobal("fetch", fetch);
  const wrapper = start();
  await flushPromises();
  const proof = gate(wrapper).verify();
  const rejected = expect(proof).rejects.toThrow("取消");
  await flushPromises();
  sdk.close();
  await rejected;
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(sdk.widget.destroy).toHaveBeenCalled();
  expect(document.activeElement).toBe(wrapper.get('[role="alert"]').element);
});
it.each([
  { ...native(1), captcha_id: "e".repeat(32) },
  { ...native(1), role: "admin" },
  { ...native(1), lot_number: "private-url" },
])("205 malformed or mismatched native proof is rejected (%j)", async (raw) => {
  const sdk = setupSDK(1, raw);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      response(path.endsWith("/init") ? data(1) : { enabled: true }),
    ),
  );
  const wrapper = start();
  await flushPromises();
  const proof = gate(wrapper).verify(),
    rejected = expect(proof).rejects.toThrow();
  await flushPromises();
  sdk.success();
  await rejected;
});
it("205 username change discards a late init and cannot show or reuse the old widget", async () => {
  const pending = deferred<Response>(),
    sdk = setupSDK(1);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path.endsWith("/init") ? pending.promise : response({ enabled: true }),
    ),
  );
  const wrapper = start();
  await flushPromises();
  const proof = gate(wrapper).verify(),
    rejected = expect(proof).rejects.toThrow();
  await wrapper.setProps({ username: "another205" });
  pending.resolve(response(data(1)));
  await rejected;
  expect(sdk.initialize).not.toHaveBeenCalled();
  expect(wrapper.find('[role="alert"]').exists()).toBe(false);
});
it("205 switching client context prevents old metadata from authorizing the new screen", async () => {
  const pending = deferred<Response>();
  let reads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      ++reads === 1 ? pending.promise : response({ enabled: true }),
    ),
  );
  const wrapper = start({ contextKey: "first" });
  await wrapper.setProps({ contextKey: "second" });
  await flushPromises();
  pending.resolve(response({ enabled: false }));
  await flushPromises();
  expect(wrapper.text()).toContain("需要完成人机验证");
});
it("205 unmount cancels the widget and ignores its late success", async () => {
  const sdk = setupSDK(1);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      response(path.endsWith("/init") ? data(1) : { enabled: true }),
    ),
  );
  const wrapper = start();
  await flushPromises();
  const proof = gate(wrapper).verify(),
    rejected = expect(proof).rejects.toThrow();
  await flushPromises();
  wrapper.unmount();
  sdk.success();
  await rejected;
  expect(sdk.widget.destroy).toHaveBeenCalled();
});
it("205 init conflict requires explicit action and never retries automatically", async () => {
  const fetch = vi.fn(async (path: string) =>
    response(
      path.endsWith("/init") ? null : { enabled: true },
      path.endsWith("/init") ? 409 : 200,
    ),
  );
  vi.stubGlobal("fetch", fetch);
  const wrapper = start();
  await flushPromises();
  await expect(gate(wrapper).verify()).rejects.toThrow("配置已变化");
  await flushPromises();
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(wrapper.get("button").attributes("type")).toBe("button");
  await wrapper.get("button").trigger("click");
  await flushPromises();
  expect(fetch).toHaveBeenCalledTimes(3);
});
it("205 CSP or SDK load failure stays closed and removes the fixed official script", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      response(path.endsWith("/init") ? data(1) : { enabled: true }),
    ),
  );
  const wrapper = start();
  await flushPromises();
  const proof = gate(wrapper).verify(),
    rejected = expect(proof).rejects.toThrow("加载失败");
  await flushPromises();
  const script = document.head.querySelector<HTMLScriptElement>(
    'script[src="https://static.geetest.com/v4/gt4.js"]',
  );
  expect(script).not.toBeNull();
  expect(script?.referrerPolicy).toBe("no-referrer");
  script?.dispatchEvent(new Event("error"));
  await rejected;
  expect(
    document.head.querySelector(
      'script[src="https://static.geetest.com/v4/gt4.js"]',
    ),
  ).toBeNull();
  expect(wrapper.find('[role="alert"]').exists()).toBe(true);
});
