// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import AccountPage from "../src/components/AccountPage.vue";
import { ApiClient } from "../src/api";

const mounted: VueWrapper[] = [];
const clipboardDescriptor = Object.getOwnPropertyDescriptor(
  navigator,
  "clipboard",
);
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  if (clipboardDescriptor)
    Object.defineProperty(navigator, "clipboard", clipboardDescriptor);
  else Reflect.deleteProperty(navigator, "clipboard");
});

async function fixture(pid: unknown = "1000") {
  vi.stubGlobal(
    "location",
    new URL("https://gateway.example.invalid:8443/merchant/account/api-info"),
  );
  const copy = vi.fn(async () => {});
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: copy },
  });
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic-connection-info";
  const request = vi.spyOn(client, "request").mockResolvedValue({
    pid,
    algorithm: "MD5",
    api_key_masked: "abcd****wxyz",
    api_key: "should-never-render-raw-key",
    merchant_private_key: "should-never-render-private-key",
  } as never);
  const w = mount(AccountPage, {
    props: { client, section: "api-info" },
    global: {
      stubs: {
        MerchantRSAKeyRotation: true,
        MerchantSignaturePolicy: true,
        MerchantRefundEvents201: true,
      },
    },
  });
  mounted.push(w);
  await flushPromises();
  return { w, request, copy };
}

it("shows same-deployment integration addresses, local documentation and copies only the selected public value", async () => {
  const { w, request, copy } = await fixture();
  expect(w.text()).toContain("接口地址");
  expect(w.text()).toContain("https://gateway.example.invalid:8443/");
  expect(w.text()).toContain(
    "https://gateway.example.invalid:8443/api/pay/submit",
  );
  expect(w.get('a[href="/doc/"]').text()).toBe("开发文档");
  expect(w.get('a[href="/doc/pay_submit.html"]').text()).toBe("支付提交文档");
  await w.get('button[aria-label="复制接口地址"]').trigger("click");
  await flushPromises();
  expect(copy).toHaveBeenLastCalledWith(
    "https://gateway.example.invalid:8443/",
  );
  await w.get('button[aria-label="复制支付提交地址"]').trigger("click");
  await flushPromises();
  expect(copy).toHaveBeenLastCalledWith(
    "https://gateway.example.invalid:8443/api/pay/submit",
  );
  await w.get('button[aria-label="复制商户ID"]').trigger("click");
  await flushPromises();
  expect(copy).toHaveBeenLastCalledWith("1000");
  expect(request).toHaveBeenCalledTimes(1);
  expect(request.mock.calls[0]?.[0]).toBe("/account/api-info");
  expect(w.text()).toContain("abcd****wxyz");
  expect(w.text()).not.toContain("should-never-render");
});

it.each(["9223372036854775807", 1001])(
  "copies an exact supported merchant ID %s",
  async (pid) => {
    const { w, copy } = await fixture(pid);
    await w.get('button[aria-label="复制商户ID"]').trigger("click");
    await flushPromises();
    expect(copy).toHaveBeenCalledWith(String(pid));
  },
);

it.each([9007199254740992, "9223372036854775808", -1, { id: 1000 }])(
  "does not offer a rounded or invalid merchant ID for copying: %s",
  async (pid) => {
    const { w, copy } = await fixture(pid);
    expect(w.find('button[aria-label="复制商户ID"]').exists()).toBe(false);
    expect(copy).not.toHaveBeenCalled();
  },
);
