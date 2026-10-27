// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import CaptchaKeySettings203 from "../src/components/CaptchaKeySettings203.vue";
const endpoint = "/console/v1/admin/settings/captcha";
const initial = { configured: false, fingerprint: "a".repeat(64) };
const secret = "controlled-new-captcha-secret203";
const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function reply(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({
      code: status === 200 ? 0 : -1,
      data,
      message: status === 409 ? "changed203" : "rejected203",
    }),
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
    configured?: boolean;
    own?: boolean;
    adminClass?: string;
    handler?: (method: string, body: unknown) => Promise<Response>;
  } = {},
) {
  const client = reactive(new ApiClient("admin"));
  client.csrfToken = "captcha203-current-admin";
  const calls: {
    method: string;
    path: string;
    body: unknown;
    signal?: AbortSignal | null;
  }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET",
        body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, path: String(url), body, signal: init?.signal });
      expect(String(url)).toBe(endpoint);
      if (method === "PATCH")
        expect(new Headers(init?.headers).get("X-CSRF-Token")).toBe(
          "captcha203-current-admin",
        );
      return options.handler
        ? options.handler(method, body)
        : reply({ ...initial, configured: options.configured ?? false });
    }),
  );
  const wrapper = mount(CaptchaKeySettings203, {
    attachTo: document.body,
    props: {
      client,
      ownSession: options.own ?? true,
      adminClass: options.adminClass ?? "super",
    },
  });
  mounted.push(wrapper);
  await flushPromises();
  return { wrapper, client, calls };
}
function button(w: VueWrapper, label: string) {
  const found = w.findAll("button").find((entry) => entry.text() === label);
  expect(found, `visible ${label}`).toBeDefined();
  return found!;
}
async function replacement(w: VueWrapper) {
  await button(
    w,
    w.text().includes("已配置") ? "替换密钥" : "设置密钥",
  ).trigger("click");
  await w.get('input[type="password"]').setValue(secret);
  await w.get("form").trigger("submit");
  await flushPromises();
}

describe("isolated CAPTCHA key vault settings203", () => {
  it("reads metadata only and never offers to reveal an old secret or contact a provider", async () => {
    const f = await setup({ configured: true });
    expect(f.wrapper.text()).toContain("已配置");
    expect(f.wrapper.find('input[type="password"]').exists()).toBe(false);
    expect(f.wrapper.html()).not.toContain(initial.fingerprint);
    expect(f.calls.map((call) => call.method)).toEqual(["GET"]);
  });
  it("replaces only after confirmation and immediately clears the submitted plaintext", async () => {
    const pending = deferred<Response>();
    const f = await setup({
      handler: async (method) =>
        method === "PATCH" ? pending.promise : reply(initial),
    });
    await replacement(f.wrapper);
    expect(f.calls).toHaveLength(1);
    await button(f.wrapper, "确认保存").trigger("click");
    expect(f.calls[1].body).toEqual({
      action: "replace",
      key: secret,
      expected_fingerprint: initial.fingerprint,
    });
    expect(
      f.wrapper
        .findAll<HTMLInputElement>('input[type="password"]')
        .every((field) => field.element.value === ""),
    ).toBe(true);
    expect(
      button(f.wrapper, "重新读取密钥状态").attributes("disabled"),
    ).toBeDefined();
    pending.resolve(reply({ configured: true, fingerprint: "b".repeat(64) }));
    await flushPromises();
    expect(f.wrapper.text()).toContain("验证码密钥已保存");
    expect(f.wrapper.text()).not.toMatch(/验证码验证成功|验证码服务已连接/);
    expect(f.wrapper.html()).not.toContain(secret);
  });
  it("clears only after explicit confirmation and sends no key or public switch fields", async () => {
    const f = await setup({
      configured: true,
      handler: async (method) =>
        reply(
          method === "PATCH"
            ? { configured: false, fingerprint: "b".repeat(64) }
            : { ...initial, configured: true },
        ),
    });
    await button(f.wrapper, "清除密钥").trigger("click");
    expect(f.calls).toHaveLength(1);
    await button(f.wrapper, "确认清除").trigger("click");
    await flushPromises();
    expect(f.calls[1].body).toEqual({
      action: "clear",
      expected_fingerprint: initial.fingerprint,
    });
    expect(f.wrapper.text()).toContain("未配置");
    expect(f.wrapper.find('input[type="password"]').exists()).toBe(false);
  });
  it("cancels confirmation with Escape, restores focus, and clears an explicitly cancelled draft", async () => {
    const f = await setup();
    await replacement(f.wrapper);
    expect(document.activeElement).toBe(button(f.wrapper, "返回修改").element);
    await f.wrapper.get('[role="group"]').trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(document.activeElement).toBe(
      button(f.wrapper, "保存新密钥").element,
    );
    expect(f.calls).toHaveLength(1);
    await button(f.wrapper, "取消编辑").trigger("click");
    expect(f.wrapper.find('input[type="password"]').exists()).toBe(false);
    await button(f.wrapper, "设置密钥").trigger("click");
    expect(f.wrapper.get('input[type="password"]').element).toHaveProperty(
      "value",
      "",
    );
  });
  it.each(["conflict", "network", "malformed"])(
    "requires explicit metadata reread after %s, without replaying the secret",
    async (mode) => {
      let reads = 0;
      const f = await setup({
        handler: async (method) => {
          if (method === "GET") {
            reads++;
            return reply(initial);
          }
          if (mode === "conflict") return reply(null, 409);
          if (mode === "network")
            throw new TypeError("controlled interruption203");
          return reply({ ...initial, key: "must-not-render203" });
        },
      });
      await replacement(f.wrapper);
      await button(f.wrapper, "确认保存").trigger("click");
      await flushPromises();
      expect(f.wrapper.text()).toContain("重新读取");
      expect(f.wrapper.find("form").exists()).toBe(false);
      expect(f.wrapper.html()).not.toContain(secret);
      expect(f.wrapper.html()).not.toContain("must-not-render203");
      expect(f.calls).toHaveLength(2);
      await button(f.wrapper, "重新读取密钥状态").trigger("click");
      await flushPromises();
      expect(reads).toBe(2);
      expect(f.calls.filter((call) => call.method === "PATCH")).toHaveLength(1);
    },
  );
  it.each([
    { own: false, adminClass: "super" },
    { own: true, adminClass: "operator" },
  ])(
    "never reads or writes outside an original super administrator session: %j",
    async (options) => {
      const f = await setup(options);
      expect(f.calls).toEqual([]);
      expect(f.wrapper.find("form").exists()).toBe(false);
      expect(f.wrapper.text()).toContain("超级管理员");
    },
  );
  it("keeps new credentials and fingerprints out of browser storage", async () => {
    const storage = vi.spyOn(Storage.prototype, "setItem");
    const f = await setup();
    await replacement(f.wrapper);
    await button(f.wrapper, "返回修改").trigger("click");
    expect(storage).not.toHaveBeenCalled();
    expect(document.cookie).not.toContain(secret);
  });
  it.each(["client", "csrf", "role", "own", "unmount"])(
    "burns a pending replacement after %s changes",
    async (change) => {
      const pending = deferred<Response>();
      const f = await setup({
        handler: async (method) =>
          method === "PATCH" ? pending.promise : reply(initial),
      });
      await replacement(f.wrapper);
      await button(f.wrapper, "确认保存").trigger("click");
      const write = f.calls.find((call) => call.method === "PATCH");
      expect(write).toBeDefined();
      if (change === "client") {
        const next = reactive(new ApiClient("admin"));
        next.csrfToken = "next-admin203";
        await f.wrapper.setProps({ client: next });
      } else if (change === "csrf") f.client.csrfToken = "next-admin203";
      else if (change === "role")
        await f.wrapper.setProps({ adminClass: "operator" });
      else if (change === "own")
        await f.wrapper.setProps({ ownSession: false });
      else {
        f.wrapper.unmount();
        mounted.splice(mounted.indexOf(f.wrapper), 1);
      }
      await flushPromises();
      expect(write!.signal?.aborted).toBe(true);
      pending.resolve(reply({ configured: true, fingerprint: "b".repeat(64) }));
      await flushPromises();
      expect(document.body.textContent).not.toContain("验证码密钥已保存");
      expect(document.body.innerHTML).not.toContain(secret);
      expect(f.calls.filter((call) => call.method === "PATCH")).toHaveLength(1);
    },
  );
});
