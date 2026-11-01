import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import PayTypePage from "../src/components/PayTypePage.vue";
import { ApiClient } from "../src/api";

const schema = {
  title: "支付方式",
  description: "本地支付方式资料，不自动创建支付适配器。",
  fields: [
    {
      name: "name",
      label: "展示名称",
      type: "string",
      required: true,
      max_length: 100,
    },
    {
      name: "type",
      label: "类型",
      type: "select",
      required: true,
      options: [
        "alipay",
        "wxpay",
        "qqpay",
        "bank",
        "jdpay",
        "paypal",
        "douyinpay",
        "ecny",
        "sandbox",
      ].map((v) => ({ label: v, value: v })),
    },
    {
      name: "device",
      label: "支持设备",
      type: "select",
      required: true,
      default: "0",
      options: [
        { label: "PC+Mobile", value: "0" },
        { label: "PC", value: "1" },
        { label: "Mobile", value: "2" },
      ],
    },
    { name: "enabled", label: "启用资料", type: "boolean", default: true },
  ],
  permissions: { create: true, update: true, delete: true },
  actions: [],
};
// Server-merged listing: 7 originals, alipay/wxpay open, the rest closed, all
// with id "0" (no record yet). qqpay already carries a record (id 12, closed).
const listed = [
  { id: "0", type: "alipay", name: "支付宝", enabled: true, device: "0" },
  { id: "0", type: "wxpay", name: "微信支付", enabled: true, device: "0" },
  { id: "12", type: "qqpay", name: "QQ钱包", enabled: false, device: "2" },
  { id: "0", type: "bank", name: "银联支付", enabled: false, device: "0" },
  { id: "0", type: "jdpay", name: "京东支付", enabled: false, device: "0" },
  { id: "0", type: "paypal", name: "PayPal", enabled: false, device: "1" },
  { id: "0", type: "douyinpay", name: "抖音支付", enabled: false, device: "0" },
];
function client() {
  const c = new ApiClient("admin");
  c.csrfToken = "t";
  vi.spyOn(c, "request").mockImplementation(async (path: string) =>
    path.endsWith("/schema") ? schema : { items: listed, total: listed.length },
  );
  return c;
}

it("shows all 7 original pay types with their effective 状态 and Pro columns", async () => {
  const c = client();
  const w = mount(PayTypePage, { props: { client: c } });
  await flushPromises();
  expect(w.text()).toContain("系统共有");
  expect(w.text()).toContain("7");
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers).toEqual(["调用值", "名称", "支持设备", "状态", "操作"]);
  const rows = w.findAll("tbody tr");
  expect(rows).toHaveLength(7);
  // 支持设备 column renders the per-row device class (epay display_device):
  // alipay/0 -> PC+Mobile, qqpay/2 -> Mobile, paypal/1 -> PC.
  expect(rows[0]!.findAll("td")[2]!.text()).toBe("PC+Mobile");
  expect(rows[2]!.findAll("td")[2]!.text()).toBe("Mobile");
  expect(rows[5]!.findAll("td")[2]!.text()).toBe("PC");
  // Each catalog type shows its 调用值 and a KitSwitch reflecting effective state.
  expect(w.text()).toContain("alipay");
  expect(w.text()).toContain("douyinpay");
  const switches = w.findAll('input[role="switch"]');
  expect(switches).toHaveLength(7);
  expect((switches[0]!.element as HTMLInputElement).checked).toBe(true); // alipay open
  expect((switches[3]!.element as HTMLInputElement).checked).toBe(false); // bank closed
  // 订单 link points at the order list filtered by this pay type.
  expect(w.find('a[href="/admin/trade/order?type=alipay"]').exists()).toBe(
    true,
  );
  w.unmount();
});

it("POSTs a new record when toggling a type that has no record yet", async () => {
  const c = client();
  const w = mount(PayTypePage, { props: { client: c } });
  await flushPromises();
  // bank is row index 3, id "0" (no record) — enabling it creates a record.
  const switches = w.findAll('input[role="switch"]');
  await switches[3]!.setValue(true);
  await flushPromises();
  const posts = vi
    .mocked(c.request)
    .mock.calls.filter(([, o]) => o?.method === "POST");
  expect(posts).toHaveLength(1);
  expect(posts[0]![1]!.body).toMatchObject({
    type: "bank",
    enabled: true,
    name: "银联支付",
    device: "0",
  });
  w.unmount();
});

it("PATCHes the existing record when toggling a type that already has one", async () => {
  const c = client();
  const w = mount(PayTypePage, { props: { client: c } });
  await flushPromises();
  // qqpay is row index 2, id "12" — enabling it PATCHes that record only.
  const switches = w.findAll('input[role="switch"]');
  await switches[2]!.setValue(true);
  await flushPromises();
  const calls = vi.mocked(c.request).mock.calls;
  const patches = calls.filter(([, o]) => o?.method === "PATCH");
  expect(patches).toHaveLength(1);
  expect(patches[0]![0]).toBe("/resources/pay-types/12");
  expect(patches[0]![1]!.body).toEqual({ enabled: true });
  expect(calls.filter(([, o]) => o?.method === "POST")).toHaveLength(0);
  w.unmount();
});

it("opens a kit dialog for 新增 with the schema fields", async () => {
  const c = client();
  const w = mount(PayTypePage, {
    props: { client: c },
    attachTo: document.body,
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text().includes("新增"))!
    .trigger("click");
  await flushPromises();
  // KitModal teleports to body.
  const dialog = document.body.querySelector('[role="dialog"]');
  expect(dialog).not.toBeNull();
  expect(dialog!.textContent).toContain("新增支付方式");
  const labels = Array.from(dialog!.querySelectorAll("label")).map((e) =>
    e.textContent?.trim(),
  );
  expect(labels[0]).toContain("调用值");
  expect(labels[1]).toContain("显示名称");
  expect(dialog!.textContent).toContain("支持设备");
  w.unmount();
});
