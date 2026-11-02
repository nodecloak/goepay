import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ParentChannelPage from "../src/components/ParentChannelPage.vue";
import { ApiClient } from "../src/api";

// Wave-2: the 支付通道 page is reskinned to the design kit — KitFilterBar fields,
// a KitSwitch 状态 pill, KitPagination, a kit-styled add/edit modal — while Pro's
// model (filters, buttons, columns, row actions, dialog fields) is unchanged.
const row = {
  id: "2",
  name: "Wechat",
  pay_type: "wxpay",
  plugin: "wxpay",
  configured: true,
  source_channel_id: "2",
  revision: "1",
  account_version: "1",
  enabled: true,
  archived: false,
  mode: 0,
  rate: "100.00",
  fee_rate_bps: 0,
  fingerprint: "a".repeat(64),
  account: { plugin: "wxpay", app_types: ["1"], mode: 0, fields: [] },
  created_at: "2026-10-26T00:00:00Z",
  updated_at: "2026-10-26T00:00:00Z",
};
function client() {
  const c = new ApiClient("admin");
  c.csrfToken = "t";
  vi.spyOn(c, "request").mockImplementation(async (path: string) =>
    path.includes("/templates")
      ? {
          items: [
            {
              source_channel_id: "2",
              name: "Wechat",
              pay_type: "wxpay",
              fields: [],
              fingerprint: "b".repeat(64),
              merchant_count: 1,
            },
          ],
          total: 1,
        }
      : path.includes("?")
        ? { items: [row], total: 1, page: 1, size: 20 }
        : row,
  );
  return c;
}

it("renders kit filter fields, a kit switch, Pro columns and kit pagination", async () => {
  const c = client();
  const w = mount(ParentChannelPage, { props: { client: c, allowed: true } });
  await flushPromises();
  // Kit filter bar with Pro's fields.
  expect(w.find(".pa-filter").exists()).toBe(true);
  expect(w.find('input[aria-label="ID或名称"]').exists()).toBe(true);
  expect(w.find('button[aria-label="支付方式"]').exists()).toBe(true);
  expect(w.find('button[aria-label="支付插件"]').exists()).toBe(true);
  expect(w.find('button[aria-label="通道状态"]').exists()).toBe(true);
  // Pro columns unchanged.
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers).toEqual([
    "选择",
    "ID",
    "显示名称",
    "通道模式",
    "分成比例",
    "支付方式",
    "支付插件",
    "状态",
    "操作",
  ]);
  expect(w.find('input[placeholder="通道ID/名称"]').exists()).toBe(true);
  expect(w.find("[data-parent-parameters]").text()).toBe("配置密钥");
  // 状态 is a kit KitSwitch reflecting the enabled flag.
  const toggle = w.get('input[role="switch"][aria-label="支付通道 2 状态"]');
  expect((toggle.element as HTMLInputElement).checked).toBe(true);
  // Kit pagination is present.
  expect(w.find(".pa-pager").exists()).toBe(true);
  expect(w.find(".pa-pager").text()).toContain("共 1 条");
  w.unmount();
});

it("opens a kit-styled add dialog with Pro's fields and one save button", async () => {
  const c = client();
  const w = mount(ParentChannelPage, { props: { client: c, allowed: true } });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "新增通道")!
    .trigger("click");
  await flushPromises();
  const dialog = w.get('.pa-modal[role="dialog"]');
  expect(dialog.text()).toContain("新增通道");
  expect(dialog.find('input[aria-label="通道名称"]').exists()).toBe(true);
  expect(dialog.find('input[aria-label="分成比例"]').exists()).toBe(true);
  // The save button keeps its Pro test hook and is a kit primary button.
  const save = dialog.get("[data-parent-save]");
  expect(save.classes()).toContain("pa-btn--primary");
  w.unmount();
});
