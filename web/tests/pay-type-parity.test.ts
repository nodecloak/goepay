import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";
import Page from "../src/components/PayTypePage.vue";
import Method from "../src/components/KitPayMethod.vue";
afterEach(() => vi.restoreAllMocks());
it("renders the stored display name and links to orders filtered by the actual type without writing", async () => {
  const client = new ApiClient("admin");
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path) =>
      path.endsWith("/schema")
        ? {
            title: "支付方式",
            fields: [],
            permissions: { create: false, update: false, delete: false },
            actions: [],
          }
        : {
            items: [
              {
                id: "1",
                type: "alipay",
                name: "企业支付宝",
                enabled: true,
                device: "0",
              },
            ],
          },
    );
  const w = mount(Page, { props: { client } });
  await flushPromises();
  expect(w.find("tbody tr").text()).toContain("企业支付宝");
  expect(w.find('a[href="/admin/trade/order?type=alipay"]').text()).toBe(
    "订单",
  );
  expect(
    request.mock.calls.every(
      ([, options]) => !options?.method || options.method === "GET",
    ),
  ).toBe(true);
  w.unmount();
});
it("keeps brand icons and default names while safely rendering a configured label", () => {
  const custom = mount(Method, {
    props: { type: "alipay", label: "<b>企业支付</b>" },
  });
  expect(custom.text()).toContain("<b>企业支付</b>");
  expect(custom.find("b").exists()).toBe(false);
  expect(custom.find("img").exists()).toBe(true);
  custom.unmount();
  const normal = mount(Method, { props: { type: "wxpay" } });
  expect(normal.text()).toContain("微信支付");
  normal.unmount();
});
