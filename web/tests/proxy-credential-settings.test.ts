// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import ProxyCredentialSettings from "../src/components/ProxyCredentialSettings.vue";

const endpoint = "/console/v1/admin/settings/proxy-credentials";
const base = {
  password_set: false,
  apikey_set: false,
  fingerprint: "a".repeat(64),
};
const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function reply(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({ code: status === 200 ? 0 : -1, data, message: "x" }),
    { status },
  );
}
async function setup(
  opts: {
    own?: boolean;
    adminClass?: string;
    handler?: (m: string, b: any) => Promise<Response>;
  } = {},
) {
  const client = reactive(new ApiClient("admin"));
  client.csrfToken = "proxy-current-admin";
  const calls: { method: string; body: any }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, body });
      expect(String(url)).toBe(endpoint);
      return opts.handler ? opts.handler(method, body) : reply(base);
    }),
  );
  const wrapper = mount(ProxyCredentialSettings, {
    attachTo: document.body,
    props: {
      client,
      ownSession: opts.own ?? true,
      adminClass: opts.adminClass ?? "super",
    },
  });
  mounted.push(wrapper);
  await flushPromises();
  return { wrapper, calls };
}
function button(w: VueWrapper, label: string) {
  const b = w.findAll("button").find((e) => e.text() === label);
  expect(b, label).toBeDefined();
  return b!;
}

it("hides configuration from a non-super or non-owner session", async () => {
  const { wrapper, calls } = await setup({ adminClass: "operator" });
  expect(wrapper.text()).toContain("请使用超级管理员本人会话配置代理密钥");
  expect(calls.length).toBe(0);
});

it("sets the API key through a write-only PATCH and shows 已设置, never echoing the key", async () => {
  let saved = false;
  const { wrapper, calls } = await setup({
    handler: async (method, body) => {
      if (method === "PATCH") {
        expect(body.apikey).toBe("AbCdEf2345xyzKMNPQ78");
        expect(body.expected_fingerprint).toBe("a".repeat(64));
        saved = true;
        return reply({
          ...base,
          apikey_set: true,
          fingerprint: "b".repeat(64),
        });
      }
      return reply(base);
    },
  });
  expect(wrapper.text()).toContain("API接口密钥：");
  await button(wrapper, "设置API接口密钥").trigger("click");
  await flushPromises();
  await wrapper
    .get('input[name="proxy_apikey"]')
    .setValue("AbCdEf2345xyzKMNPQ78");
  await button(wrapper, "保存API接口密钥").trigger("click");
  await flushPromises();
  expect(saved).toBe(true);
  expect(wrapper.text()).toContain("API接口密钥已保存");
  expect(wrapper.findAll(".proxy-secret")[1]!.text()).toContain("已设置");
  expect(wrapper.html()).not.toContain("AbCdEf2345xyzKMNPQ78");
  expect(calls.filter((c) => c.method === "PATCH").length).toBe(1);
});

it("the 随机 button fills a 20-char key from the original alphabet", async () => {
  const { wrapper } = await setup();
  await button(wrapper, "设置API接口密钥").trigger("click");
  await flushPromises();
  await button(wrapper, "随机").trigger("click");
  const value = (
    wrapper.get('input[name="proxy_apikey"]').element as HTMLInputElement
  ).value;
  expect(value).toMatch(
    /^[ABCDEFGHJKMNPQRSTWXYZabcdefhijkmnprstwxyz2345678]{20}$/,
  );
});

it("a stale fingerprint (409) forces a re-read without auto-retry", async () => {
  const { wrapper } = await setup({
    handler: async (method) =>
      method === "PATCH"
        ? reply(null, 409)
        : reply({ ...base, password_set: true }),
  });
  await button(wrapper, "替换代理密码").trigger("click");
  await flushPromises();
  await wrapper.get('input[name="proxy_pwd"]').setValue("relay-secret");
  await button(wrapper, "保存代理密码").trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("代理密钥状态已变化");
});

it("tests proxy connectivity through the configured server and shows the egress IP", async () => {
  const calls: { method: string; path: string; body: any }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, path: String(url), body });
      if (String(url).endsWith("/settings/proxy-test"))
        return reply({ reachable: true, ip: "198.51.100.77" });
      return reply(base);
    }),
  );
  const { reactive } = await import("vue");
  const client = reactive(new ApiClient("admin"));
  client.csrfToken = "proxy-current-admin";
  const wrapper = mount(ProxyCredentialSettings, {
    attachTo: document.body,
    props: { client, ownSession: true, adminClass: "super" },
  });
  mounted.push(wrapper);
  await flushPromises();
  await wrapper.get('[aria-label="代理IP"]').setValue("proxy.example.net");
  await wrapper.get('[aria-label="代理端口"]').setValue("1080");
  const test = wrapper.findAll("button").find((b) => b.text() === "测试连通性");
  expect(test).toBeDefined();
  await test!.trigger("click");
  await flushPromises();
  const call = calls.find((c) => c.path.endsWith("/settings/proxy-test"));
  expect(call?.body).toMatchObject({
    type: "http",
    server: "proxy.example.net",
    port: "1080",
  });
  expect(wrapper.text()).toContain("出口IP：198.51.100.77");
});
