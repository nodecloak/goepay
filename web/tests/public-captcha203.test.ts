// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import PublicTestPay203 from "../src/components/PublicTestPay203.vue";
const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
const id = "a".repeat(32),
  challenge = "c".repeat(32),
  token = "t".repeat(43),
  csrf = "s".repeat(43);
function reply(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({
      code: status === 200 ? 0 : -1,
      data,
      message: status === 200 ? "" : "controlled rejection203",
    }),
    { status },
  );
}
async function setup(version: 0 | 1, rawProof?: Record<string, string>) {
  let success = () => {},
    closed = () => {};
  const native =
    rawProof ??
    (version === 1
      ? {
          captcha_id: id,
          lot_number: "c".repeat(32),
          pass_token: "controlled-pass203",
          gen_time: String(Math.floor(Date.now() / 1000)),
          captcha_output: "controlled-output203",
        }
      : {
          geetest_challenge: challenge,
          geetest_validate: "d".repeat(32),
          geetest_seccode: "controlled-seccode203",
        });
  const widget = {
    onReady(callback: () => void) {
      queueMicrotask(callback);
      return widget;
    },
    onSuccess(callback: () => void) {
      success = callback;
      return widget;
    },
    onError(_callback: () => void) {
      return widget;
    },
    onClose(callback: () => void) {
      closed = callback;
      return widget;
    },
    getValidate: () => native,
    showCaptcha: vi.fn(),
    verify: vi.fn(),
    reset: vi.fn(),
    destroy: vi.fn(),
  };
  const initialize = vi.fn(
    (_options: unknown, callback: (value: typeof widget) => void) =>
      callback(widget),
  );
  vi.stubGlobal("initGeetest", initialize);
  vi.stubGlobal("initGeetest4", initialize);
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, options?: RequestInit) => {
      if (path === "/console/v1/public/testpay/init")
        return reply({
          csrf_token: csrf,
          trade_no: "TP-CAPTCHA203",
          money: "1.00",
          paytype: [{ id: "700203", name: "alipay", showname: "支付宝203" }],
          sitename: "Controlled203",
          captcha_open: 1,
        });
      if (path === "/console/v1/public/captcha?purpose=testpay")
        return reply({
          version,
          gt: id,
          challenge: version === 0 ? challenge : "",
          new_captcha: version === 0,
          success: 1,
          captcha_token: token,
        });
      if (path === "/console/v1/public/testpay/submit") {
        writes.push(JSON.parse(String(options?.body)));
        return reply(null, 422);
      }
      throw Error("unexpected public CAPTCHA request");
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<span/>" } }],
  });
  await router.push("/test");
  await router.isReady();
  const wrapper = mount(PublicTestPay203, {
    attachTo: document.body,
    global: { plugins: [router] },
  });
  mounted.push(wrapper);
  await flushPromises();
  return {
    wrapper,
    widget,
    initialize,
    writes,
    native,
    success: () => success(),
    close: () => closed(),
  };
}
function pay(wrapper: VueWrapper) {
  const button = wrapper
    .findAll("button")
    .find((entry) => entry.text() === "支付宝203");
  expect(button).toBeDefined();
  return button!;
}
it.each([0, 1] as const)(
  "V%s waits for controlled SDK proof and posts only its version's fields plus purpose-bound wrapper",
  async (version) => {
    const f = await setup(version);
    expect(f.initialize).toHaveBeenCalledOnce();
    expect(document.querySelector('script[src*="geetest.com"]')).toBeNull();
    await pay(f.wrapper).trigger("click");
    expect(f.writes).toEqual([]);
    expect(
      version === 1 ? f.widget.showCaptcha : f.widget.verify,
    ).toHaveBeenCalledOnce();
    await pay(f.wrapper).trigger("click");
    f.success();
    await flushPromises();
    expect(f.writes).toEqual([
      {
        money: "1.00",
        typeid: "700203",
        csrf_token: csrf,
        captcha_token: token,
        ...f.native,
      },
    ]);
    expect(f.wrapper.text()).not.toContain("订单已支付成功");
    expect(f.widget.reset).toHaveBeenCalledOnce();
  },
);
it.each([0, 1] as const)(
  "V%s cancellation creates no payment or substitute CAPTCHA proof",
  async (version) => {
    const f = await setup(version);
    await pay(f.wrapper).trigger("click");
    f.close();
    await flushPromises();
    expect(f.writes).toEqual([]);
    expect(pay(f.wrapper).attributes("disabled")).toBeUndefined();
    expect(f.wrapper.text()).not.toMatch(/验证成功|订单已支付成功/);
  },
);
it("ignores a controlled SDK result after its public screen is unmounted", async () => {
  const f = await setup(1);
  await pay(f.wrapper).trigger("click");
  f.wrapper.unmount();
  mounted.splice(mounted.indexOf(f.wrapper), 1);
  f.success();
  await flushPromises();
  expect(f.writes).toEqual([]);
  expect(f.widget.destroy).toHaveBeenCalledOnce();
});
it("rejects mixed or unexpected SDK output instead of treating client success as payment authority", async () => {
  const f = await setup(1, {
    captcha_id: id,
    lot_number: challenge,
    pass_token: "pass203",
    gen_time: "1",
    captcha_output: "output203",
    geetest_validate: "mixed203",
  });
  await pay(f.wrapper).trigger("click");
  f.success();
  await flushPromises();
  expect(f.writes).toEqual([]);
  expect(f.wrapper.get('[role="alert"]').text()).toMatch(/格式|验证码/);
});
