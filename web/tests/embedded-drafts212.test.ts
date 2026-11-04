// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import type { Component } from "vue";
import { ApiClient } from "../src/api";
import { coreRiskFields } from "../src/core-payment-risk";
import CorePaymentRisk from "../src/components/CorePaymentRisk.vue";
import PaymentDefend206 from "../src/components/PaymentDefend206.vue";
import ScanpaySettings192 from "../src/components/ScanpaySettings192.vue";
import ScanJSSettings202 from "../src/components/ScanJSSettings202.vue";
import SiteBrandSettings193 from "../src/components/SiteBrandSettings193.vue";
import CaptchaKeySettings203 from "../src/components/CaptchaKeySettings203.vue";
import PlatformMerchantAccessSettings from "../src/components/PlatformMerchantAccessSettings.vue";
import ProxyCredentialSettings from "../src/components/ProxyCredentialSettings.vue";
import OAuthCredentialSettings from "../src/components/OAuthCredentialSettings.vue";
import BuyerFeeSettings104 from "../src/components/BuyerFeeSettings104.vue";

const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
const fp = (char: string) => char.repeat(64);
function button(wrapper: VueWrapper, label: string) {
  const found = wrapper
    .findAll("button")
    .find(
      (item) =>
        item.text() === label || item.attributes("aria-label") === label,
    );
  expect(found, `visible ${label}`).toBeDefined();
  return found!;
}
function render(
  component: Component,
  request: (path: string, options?: any) => Promise<any>,
  props: Record<string, unknown> = {},
  confirmDiscard?: (label: string) => Promise<boolean>,
) {
  const entries = new Map<symbol, { label: string; dirty: () => boolean }>();
  const client = Object.assign(new ApiClient("admin"), {
    csrfToken: "embedded212-session",
    request,
  });
  const wrapper = mount(component, {
    attachTo: document.body,
    props: { client, ownSession: true, adminClass: "super", ...props },
    global: {
      provide: {
        [Symbol.for("goepay:settings-drafts212")]: {
          register(label: string, dirty: () => boolean) {
            const key = Symbol(label);
            entries.set(key, { label, dirty });
            return () => entries.delete(key);
          },
          confirmDiscard,
        },
      },
    },
  });
  mounted.push(wrapper);
  return {
    wrapper,
    dirty: () => [...entries.values()].some((entry) => entry.dirty()),
    labels: () => [...entries.values()].map((entry) => entry.label),
  };
}

const core = {
  configured: false,
  revision: "0",
  fingerprint: fp("a"),
  geo_configured: false,
  settings: Object.fromEntries(
    coreRiskFields.map((field) => [
      field.key,
      field.kind === "flag" || field.kind === "scan" ? "0" : "",
    ]),
  ),
};
const defend = {
  configured: false,
  revision: "0",
  fingerprint: fp("a"),
  can_update: true,
  settings: {
    pay_verify: "0",
    pay_verify_type: "0",
    pay_verify_check_second: "",
    pay_verify_check_count: "",
    pay_verify_check_rate: "",
    pay_verify_check_ip: "",
    pay_verify_check_uid: "",
  },
};
const scanpay = {
  scanpay_open: "0",
  configured: false,
  revision: "0",
  fingerprint: fp("a"),
  can_update: true,
};
const scanJS = {
  enabled: false,
  configured: false,
  revision: "0",
  fingerprint: fp("a"),
  profile_id: "",
  profile_revision: "",
  available: false,
  can_update: true,
  profiles: [{ id: "202", revision: "7", channel_id: "8", app_id: "wx212" }],
};
const access = {
  close_keylogin: "0",
  user_refund: "0",
  fingerprint: fp("a"),
  can_update: true,
};
const minimum = {
  kind: "global",
  channel_id: "0",
  payfee_lessthan: "0.00",
  payfee_mincost: "0.00",
  effective_payfee_lessthan: "0.00",
  effective_payfee_mincost: "0.00",
  configured: false,
  revision: "0",
  fingerprint: fp("a"),
  can_update: true,
};
type Scenario = {
  name: string;
  component: Component;
  props?: Record<string, unknown>;
  base: Record<string, unknown>;
  selector: string;
  changed: string | boolean;
  initial: string | boolean;
  prepare: string;
  confirm?: string;
  beforeSave?: (wrapper: VueWrapper) => Promise<unknown>;
  saved: (body: any) => Record<string, unknown>;
};
const scenarios: Scenario[] = [
  {
    name: "core payment risk",
    component: CorePaymentRisk,
    props: { allowed: true },
    base: core,
    selector: "#risk177-pay_iplimit",
    changed: "2",
    initial: "",
    prepare: "核对并保存风控设置",
    confirm: "确认执行",
    saved: (body) => ({
      ...core,
      configured: true,
      revision: "1",
      fingerprint: fp("b"),
      settings: body.settings,
    }),
  },
  {
    name: "payment verification",
    component: PaymentDefend206,
    base: defend,
    selector: '[name="pay_verify"]',
    changed: "2",
    initial: "0",
    prepare: "核对并保存支付验证设置",
    confirm: "确认保存",
    saved: (body) => ({
      ...defend,
      configured: true,
      revision: "1",
      fingerprint: fp("b"),
      settings: body.settings,
    }),
  },
  {
    name: "barcode payment switch",
    component: ScanpaySettings192,
    base: scanpay,
    selector: 'input[type="checkbox"]',
    changed: true,
    initial: false,
    prepare: "保存付款码设置",
    confirm: "确认开启付款码支付",
    saved: (body) => ({
      ...scanpay,
      scanpay_open: body.scanpay_open,
      configured: true,
      revision: "1",
      fingerprint: fp("b"),
    }),
  },
  {
    name: "native scan authorization before selecting a profile",
    component: ScanJSSettings202,
    base: scanJS,
    selector: 'input[type="checkbox"]',
    changed: true,
    initial: false,
    prepare: "保存原生扫码设置",
    confirm: "确认授权原生扫码",
    beforeSave: async (wrapper) => wrapper.get("select").setValue("202"),
    saved: (body) => ({
      ...scanJS,
      enabled: body.enabled,
      profile_id: body.profile_id,
      profile_revision: body.profile_revision,
      available: true,
      configured: true,
      revision: "1",
      fingerprint: fp("b"),
    }),
  },
  {
    name: "merchant self service refund permission",
    component: PlatformMerchantAccessSettings,
    props: { field: "user_refund" },
    base: access,
    selector: 'input[type="checkbox"]',
    changed: true,
    initial: false,
    prepare: "保存平台功能",
    confirm: "确认保存平台功能",
    saved: (body) => ({
      ...access,
      close_keylogin: body.close_keylogin,
      user_refund: body.user_refund,
      fingerprint: fp("b"),
    }),
  },
  {
    name: "global minimum buyer fee",
    component: BuyerFeeSettings104,
    props: { kind: "global", allowed: true },
    base: minimum,
    selector: '[name="payfee_lessthan"]',
    changed: "0.01",
    initial: "0.00",
    prepare: "保存设置",
    saved: (body) => ({
      ...minimum,
      payfee_lessthan: body.payfee_lessthan,
      effective_payfee_lessthan: body.payfee_lessthan,
      configured: true,
      revision: "1",
      fingerprint: fp("b"),
    }),
  },
];
async function submit(wrapper: VueWrapper, scenario: Scenario) {
  await scenario.beforeSave?.(wrapper);
  await button(wrapper, scenario.prepare).trigger("click");
  if (scenario.confirm)
    await button(wrapper, scenario.confirm).trigger("click");
  await flushPromises();
}

describe("embedded settings participate in the parent unsaved draft guard", () => {
  it.each(scenarios)(
    "$name cannot resume a reread after permission changes during discard confirmation",
    async (scenario) => {
      let resolveDiscard!: (allowed: boolean) => void;
      const request = vi.fn(async (path: string) =>
        path.endsWith("/actions")
          ? { items: [], total: 0 }
          : structuredClone(scenario.base),
      );
      const panel = render(
        scenario.component,
        request,
        scenario.props,
        () => new Promise<boolean>((yes) => (resolveDiscard = yes)),
      );
      await flushPromises();
      await panel.wrapper.get(scenario.selector).setValue(scenario.changed);
      const readsBefore = request.mock.calls.length;
      await panel.wrapper
        .findAll("button")
        .find((item) => /刷新|重新读取/.test(item.text()))!
        .trigger("click");
      await panel.wrapper.setProps(
        scenario.props?.allowed ? { allowed: false } : { ownSession: false },
      );
      resolveDiscard(true);
      await flushPromises();
      expect(request.mock.calls).toHaveLength(readsBefore);
      expect(panel.dirty()).toBe(false);
    },
  );
  it.each(scenarios)(
    "$name reread preserves the draft until its parent confirms discarding",
    async (scenario) => {
      let discard = false;
      const request = vi.fn(async (path: string) =>
        path.endsWith("/actions")
          ? { items: [], total: 0 }
          : structuredClone(scenario.base),
      );
      const panel = render(
        scenario.component,
        request,
        scenario.props,
        async () => discard,
      );
      await flushPromises();
      await panel.wrapper.get(scenario.selector).setValue(scenario.changed);
      const readsBefore = request.mock.calls.length;
      const reload = () =>
        panel.wrapper
          .findAll("button")
          .find((item) => /刷新|重新读取/.test(item.text()))!;
      await reload().trigger("click");
      await flushPromises();
      expect(request.mock.calls).toHaveLength(readsBefore);
      expect(panel.dirty()).toBe(true);
      const input = panel.wrapper.get(scenario.selector)
        .element as HTMLInputElement;
      expect(
        typeof scenario.changed === "boolean" ? input.checked : input.value,
      ).toBe(scenario.changed);
      discard = true;
      await reload().trigger("click");
      await flushPromises();
      expect(request.mock.calls.length).toBeGreaterThan(readsBefore);
      expect(panel.dirty()).toBe(false);
    },
  );
  it.each(scenarios)(
    "$name tracks actual edits, restoration and a verified save",
    async (scenario) => {
      const request = vi.fn(async (path: string, options?: any) =>
        path.endsWith("/actions")
          ? { items: [], total: 0 }
          : options?.method === "PATCH"
            ? scenario.saved(options.body)
            : structuredClone(scenario.base),
      );
      const panel = render(scenario.component, request, scenario.props);
      await flushPromises();
      expect(panel.dirty()).toBe(false);
      await panel.wrapper.get(scenario.selector).setValue(scenario.changed);
      expect(panel.dirty()).toBe(true);
      await panel.wrapper.get(scenario.selector).setValue(scenario.initial);
      expect(panel.dirty()).toBe(false);
      await panel.wrapper.get(scenario.selector).setValue(scenario.changed);
      await submit(panel.wrapper, scenario);
      expect(
        request.mock.calls.filter((call) => call[1]?.method === "PATCH"),
      ).toHaveLength(1);
      expect(panel.dirty()).toBe(false);
      panel.wrapper.unmount();
      expect(panel.labels()).toEqual([]);
    },
  );
  it.each(scenarios)(
    "$name retains an unconfirmed save until a successful readonly check",
    async (scenario) => {
      let failRead = false;
      const request = vi.fn(async (path: string, options?: any) => {
        if (path.endsWith("/actions")) return { items: [], total: 0 };
        if (options?.method === "PATCH" || failRead)
          throw new TypeError("controlled uncertain result212");
        return structuredClone(scenario.base);
      });
      const panel = render(scenario.component, request, scenario.props);
      await flushPromises();
      await panel.wrapper.get(scenario.selector).setValue(scenario.changed);
      await submit(panel.wrapper, scenario);
      expect(panel.dirty()).toBe(true);
      failRead = true;
      const reload = () =>
        panel.wrapper
          .findAll("button")
          .find((item) => /刷新|重新读取/.test(item.text()))!;
      await reload().trigger("click");
      await flushPromises();
      expect(panel.dirty()).toBe(true);
      failRead = false;
      await reload().trigger("click");
      await flushPromises();
      expect(panel.dirty()).toBe(false);
      expect(
        request.mock.calls.filter((call) => call[1]?.method === "PATCH"),
      ).toHaveLength(1);
    },
  );
});

const credentials = [
  {
    name: "CAPTCHA key",
    component: CaptchaKeySettings203,
    base: { configured: false, fingerprint: fp("a") },
    open: "设置密钥",
    selector: '[name="new_captcha_key"]',
    save: "保存新密钥",
    confirm: "确认保存",
    cancel: "取消编辑",
    saved: { configured: true, fingerprint: fp("b") },
  },
  {
    name: "proxy API key",
    component: ProxyCredentialSettings,
    base: { password_set: false, apikey_set: false, fingerprint: fp("a") },
    open: "设置API接口密钥",
    selector: '[name="proxy_apikey"]',
    save: "保存API接口密钥",
    cancel: "取消修改",
    saved: { password_set: false, apikey_set: true, fingerprint: fp("b") },
  },
  {
    name: "OAuth key",
    component: OAuthCredentialSettings,
    base: {
      qq_set: false,
      wx_set: false,
      alipay_set: false,
      telegram_set: false,
      fingerprint: fp("a"),
    },
    open: "设置QQ互联 APPKEY",
    selector: 'input[type="password"]',
    save: "保存QQ互联 APPKEY",
    cancel: "取消修改",
    saved: {
      qq_set: true,
      wx_set: false,
      alipay_set: false,
      telegram_set: false,
      fingerprint: fp("b"),
    },
  },
];
it.each(credentials)(
  "$name protects an in-memory secret and clears only a cancelled or verified draft",
  async (scenario) => {
    let fail = false;
    const request = vi.fn(async (_path: string, options?: any) => {
      if (options?.method === "PATCH") {
        if (fail) throw new TypeError("controlled uncertain credential212");
        return scenario.saved;
      }
      return scenario.base;
    });
    const panel = render(scenario.component, request, {}, async () => false);
    await flushPromises();
    expect(panel.dirty()).toBe(false);
    await button(panel.wrapper, scenario.open).trigger("click");
    await panel.wrapper
      .get(scenario.selector)
      .setValue("synthetic-in-memory-secret212");
    expect(panel.dirty()).toBe(true);
    const readsBefore = request.mock.calls.length;
    await panel.wrapper
      .findAll("button")
      .find((item) => /重新读取/.test(item.text()))!
      .trigger("click");
    await flushPromises();
    expect(request.mock.calls).toHaveLength(readsBefore);
    expect(panel.wrapper.get(scenario.selector).element).toHaveProperty(
      "value",
      "synthetic-in-memory-secret212",
    );
    await button(panel.wrapper, scenario.cancel).trigger("click");
    expect(panel.dirty()).toBe(false);
    await button(panel.wrapper, scenario.open).trigger("click");
    await panel.wrapper
      .get(scenario.selector)
      .setValue("synthetic-in-memory-secret212");
    await panel.wrapper.get("form").trigger("submit");
    if (scenario.confirm)
      await button(panel.wrapper, scenario.confirm).trigger("click");
    await flushPromises();
    expect(panel.dirty()).toBe(false);
    expect(panel.wrapper.html()).not.toContain("synthetic-in-memory-secret212");
    const replace = panel.wrapper
      .findAll("button")
      .find((item) => /^(替换|修改)/.test(item.text()))!;
    await replace.trigger("click");
    await panel.wrapper
      .get(scenario.selector)
      .setValue("second-synthetic-secret212");
    fail = true;
    await panel.wrapper.get("form").trigger("submit");
    if (scenario.confirm)
      await button(panel.wrapper, scenario.confirm).trigger("click");
    await flushPromises();
    expect(panel.dirty()).toBe(true);
    expect(panel.wrapper.html()).not.toContain("second-synthetic-secret212");
    expect(
      request.mock.calls.filter((call) => call[1]?.method === "PATCH"),
    ).toHaveLength(2);
  },
);

it.each([
  {
    component: OAuthCredentialSettings,
    base: {
      qq_set: true,
      wx_set: true,
      alipay_set: false,
      telegram_set: false,
      fingerprint: fp("a"),
    },
    label: "QQ互联 APPKEY",
  },
  {
    component: ProxyCredentialSettings,
    base: { password_set: true, apikey_set: true, fingerprint: fp("a") },
    label: "代理密码",
  },
])(
  "protects a staged credential clear until undo or explicit discard",
  async (scenario) => {
    const request = vi.fn(async () => scenario.base);
    const panel = render(scenario.component, request, {}, async () => false);
    await flushPromises();
    expect(panel.dirty()).toBe(false);
    await button(panel.wrapper, `清除${scenario.label}`).trigger("click");
    expect(panel.dirty()).toBe(true);
    await panel.wrapper
      .findAll("button")
      .find((item) => /重新读取/.test(item.text()))!
      .trigger("click");
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    expect(panel.wrapper.text()).toContain("保存后清除");
    await button(panel.wrapper, `撤销清除${scenario.label}`).trigger("click");
    expect(panel.dirty()).toBe(false);
    expect(request).toHaveBeenCalledTimes(1);
  },
);

it("brand selection uploads immediately, protects an in-flight operation and has no draft after verified upload", async () => {
  const base = { revision: "0", fingerprint: fp("a"), logo: null, icon: null };
  let resolve!: (reply: Response) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(() => new Promise<Response>((yes) => (resolve = yes))),
  );
  const panel = render(SiteBrandSettings193, async () => base);
  await flushPromises();
  expect(panel.dirty()).toBe(false);
  const input = panel.wrapper.get('input[type="file"]');
  Object.defineProperty(input.element, "files", {
    configurable: true,
    value: [new File(["synthetic"], "logo.png", { type: "image/png" })],
  });
  await input.trigger("change");
  expect(panel.dirty()).toBe(true);
  resolve(
    new Response(
      JSON.stringify({
        code: 0,
        data: {
          ...base,
          revision: "1",
          fingerprint: fp("b"),
          logo: { id: fp("c"), mime: "image/png", width: 16, height: 16 },
        },
      }),
      { status: 200 },
    ),
  );
  await flushPromises();
  expect(panel.wrapper.text()).toContain("上传成功");
  expect(panel.dirty()).toBe(false);
  expect((input.element as HTMLInputElement).value).toBe("");
});

it("an uncertain image upload remains protected until a confirmed successful reread, while an invalid local file has no draft", async () => {
  const base = { revision: "0", fingerprint: fp("a"), logo: null, icon: null };
  const fetch = vi.fn(async () => {
    throw new TypeError("controlled upload interruption212");
  });
  vi.stubGlobal("fetch", fetch);
  let discard = false;
  const request = vi.fn(async () => base);
  const panel = render(SiteBrandSettings193, request, {}, async () => discard);
  await flushPromises();
  const input = panel.wrapper.get('input[type="file"]');
  Object.defineProperty(input.element, "files", {
    configurable: true,
    value: [new File(["synthetic"], "bad.txt", { type: "text/plain" })],
  });
  await input.trigger("change");
  await flushPromises();
  expect(fetch).not.toHaveBeenCalled();
  expect(panel.dirty()).toBe(false);
  await button(panel.wrapper, "重新读取品牌图片").trigger("click");
  await flushPromises();
  const validInput = panel.wrapper.get('input[type="file"]');
  Object.defineProperty(validInput.element, "files", {
    configurable: true,
    value: [new File(["synthetic"], "logo.png", { type: "image/png" })],
  });
  await validInput.trigger("change");
  await flushPromises();
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(panel.dirty()).toBe(true);
  const readsBefore = request.mock.calls.length;
  await button(panel.wrapper, "重新读取品牌图片").trigger("click");
  await flushPromises();
  expect(request.mock.calls).toHaveLength(readsBefore);
  expect(panel.dirty()).toBe(true);
  discard = true;
  await button(panel.wrapper, "重新读取品牌图片").trigger("click");
  await flushPromises();
  expect(panel.dirty()).toBe(false);
});
