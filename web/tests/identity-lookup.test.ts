import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import Page from "../src/components/IdentityLookupPage.vue";
import { ApiClient } from "../src/api";
vi.mock("qrcode", () => ({
  default: { toDataURL: vi.fn(async () => "data:image/png;base64,local-test") },
}));

const id = "a".repeat(64),
  token = "b".repeat(64);
const caps = {
  merchant_id: "1000",
  available: true,
  reason: "",
  channels: [{ channel_id: "7", provider: "wxpay", app_id: "local-app" }],
};
const lookup = (status = "pending") => ({
  request_id: id,
  merchant_id: "1000",
  channel_id: "7",
  provider: "wxpay",
  app_id: "local-app",
  scope: "snsapi_base",
  status,
  expires_at: new Date(Date.now() + 120000).toISOString(),
});
const created = () => ({
  ...lookup(),
  authorization_url: `https://pay.example.invalid/identity/authorize/${token}`,
});
const mountPage = (request: (...args: any[]) => any) =>
  mount(Page, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      area: "admin",
    },
  });
async function selectChannel(w: ReturnType<typeof mountPage>) {
  await w.get('input[aria-label="商户 ID"]').setValue("1000");
  await w.get('button[data-action="capabilities"]').trigger("click");
  await flushPromises();
  await w.get("select").setValue("7");
}
it("shows unavailable configuration and never creates an authorization request", async () => {
  const read = vi.fn(async () => ({
    ...caps,
    available: false,
    reason: "本地沙箱未配置真实OAuth",
    channels: [],
  }));
  const w = mountPage(read);
  await w.get('input[aria-label="商户 ID"]').setValue("1000");
  await w.get('button[data-action="capabilities"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("本地沙箱未配置真实OAuth");
  expect(
    w.get('button[data-action="create"]').attributes("disabled"),
  ).toBeDefined();
  expect(read).toHaveBeenCalledTimes(1);
  w.unmount();
});
it("creates only an explicit application-scoped authorization and reveals once after a separate status query", async () => {
  const request = vi.fn(async (path: string, options?: { method?: string }) => {
    if (path.startsWith("/identity-lookups/capabilities")) return caps;
    if (path.endsWith("/reveal"))
      return {
        subject_id: "synthetic-sensitive-subject",
        label: "微信OpenID",
        provider: "wxpay",
        app_id: "local-app",
      };
    if (options?.method === "POST") return created();
    return lookup("verified");
  });
  const w = mountPage(request);
  await selectChannel(w);
  await w.get('button[data-action="create"]').trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/identity-lookups",
    expect.objectContaining({
      method: "POST",
      body: { merchant_id: "1000", channel_id: "7" },
    }),
  );
  expect(w.text()).not.toContain("synthetic-sensitive-subject");
  expect(w.find('button[data-action="reveal"]').exists()).toBe(false);
  await w.get('button[data-action="query"]').trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([path]) => path.endsWith("/reveal")),
  ).toHaveLength(0);
  await w.get('button[data-action="reveal"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("synthetic-sensitive-subject");
  expect(w.find('button[data-action="reveal"]').exists()).toBe(false);
  await w.get('button[data-action="clear-result"]').trigger("click");
  expect(w.text()).not.toContain("synthetic-sensitive-subject");
  expect(localStorage.length + sessionStorage.length).toBe(0);
  w.unmount();
});
it("discards a late request response after changing the merchant", async () => {
  let resolve!: (value: unknown) => void;
  const read = vi.fn(async (path: string) =>
    path.includes("capabilities") ? caps : new Promise((r) => (resolve = r)),
  );
  const w = mountPage(read);
  await selectChannel(w);
  await w.get('button[data-action="create"]').trigger("click");
  await w.get('input[aria-label="商户 ID"]').setValue("1001");
  resolve(created());
  await flushPromises();
  expect(w.find('a[data-action="authorize"]').exists()).toBe(false);
  expect(w.find('button[data-action="query"]').exists()).toBe(false);
  w.unmount();
});
it("clears revealed identifiers at expiry and never polls or reveals automatically", async () => {
  vi.useFakeTimers();
  try {
    const expiry = new Date(Date.now() + 2000).toISOString();
    const request = vi.fn(
      async (path: string, options?: { method?: string }) =>
        path.includes("capabilities")
          ? caps
          : path.endsWith("/reveal")
            ? {
                subject_id: "sensitive-expiring",
                label: "微信OpenID",
                provider: "wxpay",
                app_id: "local-app",
              }
            : options?.method === "POST"
              ? { ...created(), expires_at: expiry }
              : { ...lookup("verified"), expires_at: expiry },
    );
    const w = mountPage(request);
    await selectChannel(w);
    await w.get('[data-action="create"]').trigger("click");
    await flushPromises();
    await w.get('[data-action="query"]').trigger("click");
    await flushPromises();
    await w.get('[data-action="reveal"]').trigger("click");
    await flushPromises();
    expect(w.text()).toContain("sensitive-expiring");
    await vi.advanceTimersByTimeAsync(3000);
    expect(w.text()).not.toContain("sensitive-expiring");
    expect(w.text()).toContain("已过期");
    expect(w.get('[data-action="query"]').attributes("disabled")).toBeDefined();
    expect(request).toHaveBeenCalledTimes(4);
    w.unmount();
  } finally {
    vi.useRealTimers();
  }
});
it("uses the server-selected merchant for a merchant session without an override", async () => {
  const request = vi.fn(async (path: string) =>
    path.includes("capabilities") ? caps : created(),
  );
  const w = mount(Page, {
    props: {
      client: Object.assign(new ApiClient("merchant"), { request }),
      area: "merchant",
    },
  });
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/identity-lookups/capabilities",
    expect.anything(),
  );
  expect(w.find('input[aria-label="商户 ID"]').exists()).toBe(false);
  await w.get("select").setValue("7");
  await w.get('[data-action="create"]').trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/identity-lookups",
    expect.objectContaining({ body: { channel_id: "7" } }),
  );
  expect(w.find('[data-action="authorize"]').exists()).toBe(true);
  w.unmount();
});
it("rejects authorization links carrying a query or a different route", async () => {
  const request = vi.fn(async (path: string) =>
    path.includes("capabilities")
      ? caps
      : {
          ...created(),
          authorization_url:
            "https://pay.example.invalid/identity/authorize/" +
            token +
            "?subject_id=leak",
        },
  );
  const w = mountPage(request);
  await selectChannel(w);
  await w.get('[data-action="create"]').trigger("click");
  await flushPromises();
  expect(w.get('[role="alert"]').text()).toContain("授权地址返回格式错误");
  expect(w.find('[data-action="authorize"]').exists()).toBe(false);
  w.unmount();
});
