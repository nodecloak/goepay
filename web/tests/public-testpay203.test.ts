// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  flushPromises,
  mount,
  type DOMWrapper,
  type VueWrapper,
} from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import App from "../src/App.vue";

const prefix = "/console/v1/public/testpay";
const initial = {
  csrf_token: "c".repeat(43),
  trade_no: "TP-ORIGINAL203",
  money: "1.00",
  paytype: [{ id: "700203", name: "alipay", showname: "支付宝203" }],
  sitename: "公开测试203",
  captcha_open: 0,
};
const site = {
  sitename: "公开测试203",
  title: "公开测试203",
  keywords: "",
  description: "",
  orgname: "",
  email: "",
  kfqq: "",
  kfqq_url: "",
  wework_url: "",
  qqqun: "",
  login_subtitle: "",
  homepage: "0",
  homepage_url: "",
};
type Call = {
  path: string;
  method: string;
  body: unknown;
  signal?: AbortSignal | null;
};
const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function response(data: unknown, status = 200, message = "") {
  return new Response(
    JSON.stringify({ code: status === 200 ? 0 : -1, data, message }),
    { status },
  );
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
async function setup(
  options: {
    route?: string;
    state?: unknown;
    onInit?: () => Promise<Response>;
    onSubmit?: (call: Call) => Promise<Response>;
    captcha?: () => Promise<Response>;
  } = {},
) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | Request | URL, init?: RequestInit) => {
      const path = String(input),
        method = init?.method || "GET";
      const call = {
        path,
        method,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
        signal: init?.signal,
      };
      calls.push(call);
      if (path === prefix + "/init")
        return options.onInit
          ? options.onInit()
          : response(options.state ?? initial);
      if (path === prefix + "/submit")
        return options.onSubmit
          ? options.onSubmit(call)
          : response(null, 503, "payment unavailable203");
      if (path.startsWith(prefix + "/success?"))
        return response(null, 409, "原测试订单尚未核实支付");
      if (path.startsWith("/console/v1/public/captcha"))
        return options.captcha
          ? options.captcha()
          : response(null, 503, "验证码服务不可用203");
      if (path === "/console/v1/public/site") return response(site);
      if (path === "/console/v1/public/layout")
        return response({ zhuce: "", footer: "", format: "html" });
      if (/\/console\/v1\/(admin|merchant)\/session$/.test(path))
        return response(null, 401, "console login is unrelated203");
      return response(null, 503, "optional service unavailable203");
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<span />" } }],
  });
  await router.push(options.route ?? "/test");
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
    writes: () => calls.filter((call) => call.method !== "GET"),
  };
}
function panel(wrapper: VueWrapper) {
  const found = wrapper.find('[aria-label="测试支付"]');
  expect(
    found.exists(),
    "the original /test route must mount public TestPay before console login",
  ).toBe(true);
  return found;
}
function payButton(scope: DOMWrapper<Element>) {
  const button = scope
    .findAll("button")
    .find((entry) => entry.text() === "支付宝203");
  expect(button, "configured public payment method").toBeDefined();
  return button!;
}

describe("original public TestPay203 entry", () => {
  it("opens /test without a console session and shows original server-owned fields", async () => {
    const f = await setup();
    const area = panel(f.wrapper);
    expect(area.text()).toContain("公开测试203");
    expect(area.text()).toContain("商户订单号");
    expect(area.text()).toContain("商品名称");
    expect(area.get('input[value="支付测试"]').element).toHaveProperty(
      "value",
      "支付测试",
    );
    expect(area.text()).toContain("付款金额");
    const trade = area.get('[name="trade_no"]');
    expect(trade.element).toHaveProperty("value", "TP-ORIGINAL203");
    expect(
      trade.attributes("disabled") !== undefined ||
        trade.attributes("readonly") !== undefined,
    ).toBe(true);
    expect(area.get('[name="money"]').element).toHaveProperty("value", "1.00");
    expect(payButton(area).exists()).toBe(true);
    expect(
      f.calls.some((call) =>
        /\/console\/v1\/(admin|merchant)\/(session|login)$/.test(call.path),
      ),
    ).toBe(false);
    expect(
      f.calls.some((call) =>
        call.path.startsWith("/console/v1/public/captcha"),
      ),
    ).toBe(false);
    expect(f.writes()).toEqual([]);
  });

  it("submits only the original public fields after an explicit payment method click and blocks duplicate pending writes", async () => {
    const pending = deferred<Response>();
    const f = await setup({ onSubmit: () => pending.promise });
    const area = panel(f.wrapper);
    await area.get('[name="money"]').setValue("1.23");
    expect(f.writes()).toEqual([]);
    await payButton(area).trigger("click");
    await payButton(area).trigger("click");
    expect(
      f.writes().map((call) => ({ path: call.path, body: call.body })),
    ).toEqual([
      {
        path: prefix + "/submit",
        body: {
          money: "1.23",
          typeid: "700203",
          csrf_token: initial.csrf_token,
        },
      },
    ]);
    expect(payButton(area).attributes("disabled")).toBeDefined();
    pending.resolve(response(null, 503, "synthetic payment interruption203"));
    await flushPromises();
    expect(area.text()).not.toContain("订单已支付成功");
  });

  it.each(["0", "-1", "1.001", "1e3", "NaN"])(
    "does not submit a non-canonical positive payment amount: %s",
    async (money) => {
      const f = await setup();
      const area = panel(f.wrapper);
      await area.get('[name="money"]').setValue(money);
      await payButton(area).trigger("click");
      expect(f.writes()).toEqual([]);
      expect(area.get('[role="alert"]').text()).toContain("金额");
    },
  );

  it("does not invent payment methods when the server has no permitted catalogue", async () => {
    const f = await setup({ state: { ...initial, paytype: [] } });
    const area = panel(f.wrapper);
    expect(
      area
        .findAll("button")
        .some((entry) => /支付宝|微信支付/.test(entry.text())),
    ).toBe(false);
    expect(area.text()).toMatch(/暂无|不可用|未配置/);
    expect(f.writes()).toEqual([]);
  });

  it("keeps a network-unknown submit locked rather than making another payment intent automatically", async () => {
    const f = await setup({
      onSubmit: async () => {
        throw new TypeError("synthetic network lost203");
      },
    });
    const area = panel(f.wrapper);
    await payButton(area).trigger("click");
    await flushPromises();
    expect(area.get('[role="alert"]').text()).toMatch(/未知|核对|确认/);
    const retry = area
      .findAll("button")
      .find((entry) => entry.text() === "支付宝203");
    if (retry) {
      expect(retry.attributes("disabled")).toBeDefined();
      await retry.trigger("click");
    }
    expect(f.writes()).toHaveLength(1);
    expect(
      f.calls.filter((call) => call.path === prefix + "/init"),
    ).toHaveLength(1);
    expect(area.text()).not.toContain("订单已支付成功");
  });

  it("rechecks the server's unresolved original A after fresh intent B is rejected at submit", async () => {
    const originalTrade = "TP-PREVIOUS-A203";
    const freshTrade = "TP-PREISSUED-B203";
    const f = await setup({
      state: { ...initial, trade_no: freshTrade },
      onSubmit: async () =>
        response(
          { trade_no: originalTrade },
          409,
          "原测试订单结果尚未核实，请只读核对",
        ),
    });
    const area = panel(f.wrapper);
    expect(area.get('[name="trade_no"]').element).toHaveProperty(
      "value",
      freshTrade,
    );
    await payButton(area).trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(1);
    expect(payButton(area).attributes("disabled")).toBeDefined();
    const recheck = area
      .findAll("button")
      .find((entry) => entry.text() === "核对原测试订单");
    expect(
      recheck,
      "409 must offer a read-only original-order check",
    ).toBeDefined();
    await recheck!.trigger("click");
    await flushPromises();
    expect(
      f.calls
        .filter((call) => call.path.startsWith(prefix + "/success?"))
        .map((call) => ({ path: call.path, method: call.method })),
    ).toEqual([
      {
        path: prefix + "/success?trade_no=" + originalTrade,
        method: "GET",
      },
    ]);
    expect(f.calls.some((call) => call.path.includes(freshTrade))).toBe(false);
    expect(
      f.calls.filter((call) => call.path === prefix + "/init"),
    ).toHaveLength(1);
    expect(f.writes()).toHaveLength(1);
    expect(area.text()).not.toContain("订单已支付成功");
  });

  it("never converts an ok query parameter into a paid result when the original order is unconfirmed", async () => {
    const f = await setup({ route: "/test?ok=1&trade_no=CALLER-CLAIM203" });
    const area = panel(f.wrapper);
    expect(
      f.calls.some(
        (call) => call.path === prefix + "/success?trade_no=CALLER-CLAIM203",
      ),
    ).toBe(true);
    expect(area.text()).not.toContain("订单已支付成功");
    expect(f.writes()).toEqual([]);
  });

  it("requires the testpay CAPTCHA purpose and blocks payment if the verification service is unavailable", async () => {
    const f = await setup({ state: { ...initial, captcha_open: 1 } });
    const area = panel(f.wrapper);
    expect(
      f.calls
        .filter((call) => call.path.startsWith("/console/v1/public/captcha"))
        .map((call) => call.path),
    ).toEqual(["/console/v1/public/captcha?purpose=testpay"]);
    expect(area.get('[role="alert"]').text()).toMatch(/验证码|CAPTCHA/i);
    expect(payButton(area).attributes("disabled")).toBeDefined();
    await payButton(area).trigger("click");
    expect(f.writes()).toEqual([]);
    expect(document.querySelector('script[src*="geetest.com"]')).toBeNull();
    expect(area.text()).not.toMatch(/验证成功|订单已支付成功/);
  });

  it.each([
    { ...initial, csrf_token: "" },
    { ...initial, trade_no: 203 },
    { ...initial, captcha_open: "1" },
    { ...initial, money: 1 },
    {
      ...initial,
      paytype: [
        { id: "700203", name: "alipay", showname: "A" },
        { id: "700203", name: "wxpay", showname: "B" },
      ],
    },
    { ...initial, captcha_key: "must-not-render203" },
  ])(
    "does not enable payment for malformed public initialization data: %j",
    async (state) => {
      const f = await setup({ state });
      const area = panel(f.wrapper);
      expect(area.get('[role="alert"]').text()).toMatch(/格式|读取|核对|加载/);
      expect(
        area
          .findAll("button")
          .some(
            (entry) =>
              entry.text() === "支付宝203" &&
              entry.attributes("disabled") === undefined,
          ),
      ).toBe(false);
      expect(area.html()).not.toContain("must-not-render203");
      expect(f.writes()).toEqual([]);
    },
  );

  it("does not persist public CSRF, order identifiers or CAPTCHA material in browser storage", async () => {
    const storage = vi.spyOn(Storage.prototype, "setItem");
    const f = await setup();
    panel(f.wrapper);
    expect(storage).not.toHaveBeenCalled();
    expect(document.cookie).not.toContain(initial.csrf_token);
    expect(document.cookie).not.toContain(initial.trade_no);
  });

  it("discards an initialization result immediately after leaving the public test route", async () => {
    const pending = deferred<Response>();
    const f = await setup({ onInit: () => pending.promise });
    panel(f.wrapper);
    const init = f.calls.find((call) => call.path === prefix + "/init");
    expect(init).toBeDefined();
    await f.router.push("/merchant/dashboard");
    await flushPromises();
    expect(init!.signal?.aborted).toBe(true);
    pending.resolve(response({ ...initial, trade_no: "TP-OLD-CONTEXT203" }));
    await flushPromises();
    expect(document.body.innerHTML).not.toContain("TP-OLD-CONTEXT203");
    expect(f.writes()).toEqual([]);
  });

  it("aborts a pending public submit when its screen unmounts", async () => {
    const pending = deferred<Response>();
    const f = await setup({ onSubmit: () => pending.promise });
    const area = panel(f.wrapper);
    await payButton(area).trigger("click");
    expect(f.writes()).toHaveLength(1);
    f.wrapper.unmount();
    mounted.splice(mounted.indexOf(f.wrapper), 1);
    expect(f.writes()[0].signal?.aborted).toBe(true);
    pending.resolve(response(null, 409, "late-public-error203"));
    await flushPromises();
    expect(document.body.textContent).not.toContain("late-public-error203");
    expect(f.writes()).toHaveLength(1);
  });
});
