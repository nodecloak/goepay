import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ParentChannelPage from "../src/components/ParentChannelPage.vue";
import { ApiClient } from "../src/api";

// A ported plugin channel: pay_type qqpay, plugin easypay, with plugin-defined
// field labels/notes and an options select.
const fields = [
  {
    name: "merchant_id",
    namespace: "config",
    secret: false,
    pem: false,
    present: true,
    value: "888",
    label: "商户号",
    note: "易支付后台的商户 ID",
  },
  {
    name: "sign_type",
    namespace: "config",
    secret: false,
    pem: false,
    present: true,
    value: "MD5",
    label: "签名方式",
    options: [
      { value: "MD5", label: "MD5" },
      { value: "RSA", label: "RSA" },
    ],
  },
  {
    name: "api_key",
    namespace: "config",
    secret: true,
    pem: false,
    present: true,
  },
];
const row = {
  id: "21",
  name: "易支付通道",
  pay_type: "qqpay",
  plugin: "easypay",
  configured: true,
  source_channel_id: "21",
  revision: "1",
  account_version: "1",
  enabled: true,
  archived: false,
  mode: 0,
  rate: "0.60",
  fee_rate_bps: 9940,
  fingerprint: "a".repeat(64),
  account: { plugin: "easypay", app_types: [], mode: 0, fields },
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
};
function client() {
  const c = new ApiClient("admin");
  c.csrfToken = "first";
  vi.spyOn(c, "request").mockImplementation(async (path) =>
    path.includes("/templates")
      ? { items: [], total: 0 }
      : path.includes("?")
        ? { items: [row], total: 1, page: 1, size: 20 }
        : row,
  );
  return c;
}

it("shows the catalog pay type, the plugin name and ported filter options", async () => {
  const c = client(),
    w = mount(ParentChannelPage, { props: { client: c, allowed: true } });
  await flushPromises();
  expect(w.text()).toContain("QQ钱包");
  expect(w.text()).toContain("easypay");
  // Filters are now kit KitSelect popovers: open each and read its options.
  await w.get('button[aria-label="支付方式"]').trigger("click");
  await flushPromises();
  const types = w
    .get('button[aria-label="支付方式"]')
    .element.parentElement!.querySelectorAll('[role="option"]');
  const typeLabels = Array.from(types).map((o) => o.textContent);
  expect(typeLabels.some((t) => t?.includes("QQ钱包"))).toBe(true);
  expect(typeLabels.some((t) => t?.includes("银联支付"))).toBe(true);
  await w.get('button[aria-label="支付插件"]').trigger("click");
  await flushPromises();
  const plugins = w
    .get('button[aria-label="支付插件"]')
    .element.parentElement!.querySelectorAll('[role="option"]');
  expect(
    Array.from(plugins)
      .map((o) => o.textContent)
      .some((t) => t?.includes("easypay")),
  ).toBe(true);
  w.unmount();
});

it("renders the plugin field label, note and an options select in the editor", async () => {
  const c = client(),
    w = mount(ParentChannelPage, { props: { client: c, allowed: true } });
  await flushPromises();
  await w.get("button[data-parent-parameters]").trigger("click");
  await flushPromises();
  const html = w.get('[role="dialog"]').html();
  expect(html).toContain("商户号");
  expect(html).toContain("易支付后台的商户 ID");
  expect(w.find('select[aria-label="签名方式 操作"]').exists()).toBe(false);
  expect(
    (w.get('input[aria-label="商户号 内容"]').element as HTMLInputElement)
      .value,
  ).toBe("888");
  expect(
    (w.get('select[aria-label="签名方式 选项"]').element as HTMLSelectElement)
      .value,
  ).toBe("MD5");
  const opts = w
    .get('select[aria-label="签名方式 选项"]')
    .findAll("option")
    .map((o) => o.text());
  expect(opts).toEqual(["请选择", "MD5", "RSA"]);
  w.unmount();
});
