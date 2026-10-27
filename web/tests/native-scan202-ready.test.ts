// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import MerchantScanPage192 from "../src/components/MerchantScanPage192.vue";
import { ApiClient } from "../src/api";

const wrappers: VueWrapper[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount());
  sessionStorage.clear();
  document.querySelectorAll("script[src]").forEach((script) => script.remove());
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

interface ScanResult202 {
  success(value: { resultStr: string }): void;
  cancel(): void;
}

async function readyCashier202(failSecondConfiguration = false) {
  vi.spyOn(navigator, "userAgent", "get").mockReturnValue("MicroMessenger/8.0");
  const calls: string[] = [];
  const scans: ScanResult202[] = [];
  const waiting: (() => void)[] = [];
  const errors: (() => void)[] = [];
  let sdkReady = false;
  let configuredNonce = "";
  let configurations = 0;

  // A ready SDK invokes later ready subscribers synchronously. Configuring a
  // new round must happen before the consumer relies on that ready callback.
  vi.stubGlobal("wx", {
    ready(callback: () => void) {
      if (sdkReady) callback();
      else waiting.push(callback);
    },
    error(callback: () => void) {
      errors.push(callback);
    },
    config(value: { nonceStr: string }) {
      configurations++;
      sdkReady = false;
      calls.push("config:" + value.nonceStr);
      if (configurations === 2 && failSecondConfiguration) {
        errors.forEach((callback) => callback());
        return;
      }
      configuredNonce = value.nonceStr;
      sdkReady = true;
      waiting.splice(0).forEach((callback) => callback());
    },
    scanQRCode(options: ScanResult202) {
      calls.push("scan:" + configuredNonce);
      scans.push(options);
    },
  });

  const sends: { path: string; body: Record<string, unknown> }[] = [];
  let configReads = 0;
  const client = new ApiClient("merchant");
  client.csrfToken = "native-ready202-csrf";
  client.request = (async (path: string, options: any = {}) => {
    if (options.method === "POST") sends.push({ path, body: options.body });
    if (path === "/scanpay/capabilities") {
      return {
        can_prepare: true,
        platform_open: true,
        pay_types: ["wxpay"],
      };
    }
    if (path === "/scanpay/jsconfig") {
      configReads++;
      return {
        debug: false,
        appId: "wx-ready202",
        timestamp: 1793000000 + configReads,
        nonceStr: "ready202-round-" + configReads,
        signature: "0123456789abcdef0123456789abcdef01234567",
        jsApiList: ["scanQRCode"],
      };
    }
    if (path === "/scanpay/prepare") {
      return {
        request_id: options.body.request_id,
        trade_no: "202002",
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
    }
    throw new Error("Unexpected native-ready202 HTTP boundary: " + path);
  }) as typeof client.request;

  const wrapper = mount(MerchantScanPage192, {
    props: { client, ownSession: true, merchantID: "1" },
    global: { stubs: { RouterLink: true } },
  });
  wrappers.push(wrapper);
  await flushPromises();
  await wrapper.get('[aria-label="金额键 1"]').trigger("click");

  const start = async () => {
    await wrapper.get("button.scan-main192").trigger("click");
    await flushPromises();
  };
  await start();
  expect(calls).toEqual([
    "config:ready202-round-1",
    "scan:ready202-round-1",
  ]);
  await wrapper
    .findAll("button")
    .find((button) => button.text() === "取消扫码")!
    .trigger("click");
  await flushPromises();
  return { wrapper, sends, scans, calls, start };
}

it("a second WeChat scan configures its current round before an already-ready callback can scan", async () => {
  const { wrapper, sends, scans, calls, start } = await readyCashier202();
  await start();
  expect(calls).toEqual([
    "config:ready202-round-1",
    "scan:ready202-round-1",
    "config:ready202-round-2",
    "scan:ready202-round-2",
  ]);

  scans[0].success({ resultStr: "134567890123456789" });
  await flushPromises();
  expect(sends.filter((send) => send.path === "/scanpay/prepare")).toHaveLength(0);
  scans[1].success({ resultStr: "134567890123456789" });
  scans[1].success({ resultStr: "134567890123456789" });
  await flushPromises();
  expect(sends.filter((send) => send.path === "/scanpay/prepare")).toHaveLength(1);
  expect(sends.some((send) => send.path.endsWith("/apply"))).toBe(false);
  expect(wrapper.text()).toContain("确认收款 ¥1.00");
  expect(location.href).not.toContain("134567890123456789");
  expect(JSON.stringify({ ...sessionStorage, ...localStorage })).not.toContain(
    "134567890123456789",
  );
});

it("a rejected second configuration cannot scan using the previous ready WeChat configuration", async () => {
  const { wrapper, sends, scans, calls, start } = await readyCashier202(true);
  await start();
  expect(calls).toEqual([
    "config:ready202-round-1",
    "scan:ready202-round-1",
    "config:ready202-round-2",
  ]);
  expect(scans).toHaveLength(1);
  scans[0].success({ resultStr: "134567890123456789" });
  await flushPromises();
  expect(wrapper.text()).toContain("微信扫码配置未通过");
  expect(sends.filter((send) => send.path === "/scanpay/prepare")).toHaveLength(0);
  expect(sends.some((send) => send.path.endsWith("/apply"))).toBe(false);
});
