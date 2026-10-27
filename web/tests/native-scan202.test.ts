// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import MerchantScanPage192 from "../src/components/MerchantScanPage192.vue";
import { ApiClient } from "../src/api";

const wrappers: VueWrapper[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  document.querySelectorAll("script[src]").forEach((s) => s.remove());
  sessionStorage.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function cashier202(userAgent: string) {
  vi.spyOn(navigator, "userAgent", "get").mockReturnValue(userAgent);
  const sends: { path: string; body: any }[] = [];
  const config = {
    debug: false,
    appId: "wx-native-202",
    timestamp: 1793000000,
    nonceStr: "native-nonce-202",
    signature: "0123456789abcdef0123456789abcdef01234567",
    jsApiList: ["scanQRCode"],
  };
  const client = new ApiClient("merchant");
  client.csrfToken = "native-202-csrf";
  client.request = (async (path: string, options: any = {}) => {
    if (options.method === "POST") sends.push({ path, body: options.body });
    if (path === "/scanpay/capabilities")
      return {
        can_prepare: true,
        platform_open: true,
        pay_types: ["wxpay", "alipay"],
      };
    if (path === "/scanpay/jsconfig") return config;
    if (path === "/scanpay/prepare")
      return {
        request_id: options.body.request_id,
        trade_no: "202001",
        money: "1.00",
        charge_money: "1.00",
        net_money: "0.98",
        fee_money: "0.02",
        remark: "",
        type: "wxpay",
        state: "prepared",
        can_apply: true,
        can_query: false,
      };
    throw new Error("Unexpected native scan HTTP boundary: " + path);
  }) as typeof client.request;
  const wrapper = mount(MerchantScanPage192, {
    props: { client, ownSession: true, merchantID: "1" },
    global: { stubs: { RouterLink: true } },
  });
  wrappers.push(wrapper);
  await flushPromises();
  await wrapper.get('[aria-label="金额键 1"]').trigger("click");
  return { wrapper, client, sends, config };
}

async function loadSDK202() {
  await flushPromises();
  document
    .querySelectorAll("script[src]")
    .forEach((s) => s.dispatchEvent(new Event("load")));
  await flushPromises();
}

it("WeChat native result prepares once and requires the existing explicit charge confirmation", async () => {
  let ready: (() => void) | undefined;
  let scan: any;
  const configured: any[] = [];
  vi.stubGlobal("wx", {
    config: (value: any) => configured.push(value),
    ready: (callback: () => void) => (ready = callback),
    error: () => {},
    scanQRCode: (options: any) => (scan = options),
  });
  const { wrapper, sends, config } = await cashier202("MicroMessenger/8.0");
  await wrapper.get("button.scan-main192").trigger("click");
  await loadSDK202();
  expect(configured).toEqual([config]);
  expect(sends).toEqual([
    { path: "/scanpay/jsconfig", body: { url: location.href.split("#")[0] } },
  ]);
  ready?.();
  await flushPromises();
  expect(scan?.needResult).toBe(1);
  expect(scan?.scanType).toEqual(["qrCode", "barCode"]);
  scan.success({ resultStr: "  134567890123456789  " });
  scan.success({ resultStr: "134567890123456789" });
  await flushPromises();
  expect(sends.filter((r) => r.path === "/scanpay/prepare")).toHaveLength(1);
  expect(sends.find((r) => r.path === "/scanpay/prepare")?.body.auth_code).toBe(
    "134567890123456789",
  );
  expect(sends.some((r) => r.path.endsWith("/apply"))).toBe(false);
  expect(wrapper.text()).toContain("确认收款 ¥1.00");
  expect(location.href).not.toContain("134567890123456789");
  expect(JSON.stringify({ ...sessionStorage, ...localStorage })).not.toContain(
    "134567890123456789",
  );
});

it.each(["code", "barCode", "qrCode", "result"])(
  "Alipay native %s result enters preparation without a charge",
  async (field) => {
    let scan: any;
    vi.stubGlobal("ap", { scan: (options: any) => (scan = options) });
    const { wrapper, sends } = await cashier202("AlipayClient/10.0");
    await wrapper.get("button.scan-main192").trigger("click");
    await loadSDK202();
    expect(scan?.type).toBe("bar");
    scan.success({ [field]: "  2512345678901234  " });
    await flushPromises();
    expect(sends.filter((r) => r.path === "/scanpay/prepare")).toHaveLength(1);
    expect(
      sends.find((r) => r.path === "/scanpay/prepare")?.body.auth_code,
    ).toBe("2512345678901234");
    expect(sends.some((r) => r.path.endsWith("/apply"))).toBe(false);
  },
);

it.each(["merchant", "client", "csrf", "owner", "cancel", "unmount"])(
  "a late native callback after %s changes cannot prepare or charge",
  async (change) => {
    let scan: any;
    vi.stubGlobal("ap", { scan: (options: any) => (scan = options) });
    const { wrapper, client, sends } = await cashier202("AlipayClient/10.0");
    await wrapper.get("button.scan-main192").trigger("click");
    await loadSDK202();
    expect(scan?.type).toBe("bar");
    if (change === "merchant") await wrapper.setProps({ merchantID: "2" });
    if (change === "client")
      await wrapper.setProps({
        client: Object.assign(new ApiClient("merchant"), {
          csrfToken: client.csrfToken,
          request: client.request,
        }),
      });
    if (change === "csrf") client.csrfToken = "replacement-native202-csrf";
    if (change === "owner") await wrapper.setProps({ ownSession: false });
    if (change === "cancel")
      await wrapper
        .findAll("button")
        .find((b) => b.text() === "取消扫码")!
        .trigger("click");
    if (change === "unmount") {
      wrapper.unmount();
      wrappers.splice(wrappers.indexOf(wrapper), 1);
    }
    scan.success({ code: "2512345678901234" });
    await flushPromises();
    expect(sends).toEqual([]);
  },
);

it("native SDK loading uses its fixed HTTPS asset and retains manual fallback after SDK failure", async () => {
  const { wrapper, sends } = await cashier202("AlipayClient/10.0");
  await wrapper.get("button.scan-main192").trigger("click");
  await flushPromises();
  const scripts = [
    ...document.querySelectorAll<HTMLScriptElement>("script[src]"),
  ];
  expect(scripts.map((s) => s.src)).toEqual([
    "https://gw.alipayobjects.com/as/g/h5-lib/alipayjsapi/3.1.1/alipayjsapi.min.js",
  ]);
  scripts[0].dispatchEvent(new Event("error"));
  await flushPromises();
  expect(wrapper.text()).toContain("原生扫码组件加载失败");
  const input = wrapper.get('input[autocomplete="off"]');
  await input.setValue("2512345678901234");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(sends.filter((r) => r.path === "/scanpay/prepare")).toHaveLength(1);
  expect(sends.some((r) => r.path.endsWith("/apply"))).toBe(false);
});
