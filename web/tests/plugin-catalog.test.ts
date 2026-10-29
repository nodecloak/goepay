// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import catalog from "../src/data/plugin-catalog.json";
import {
  builtinPlugin,
  builtinCount,
  filterCatalog,
  localPlugins,
  priceLabel,
  pluginTypes,
  type CatalogEntry,
} from "../src/plugin-catalog";
import AppStorePage from "../src/components/AppStorePage.vue";
import LocalPluginsPage from "../src/components/LocalPluginsPage.vue";

const items = catalog as CatalogEntry[];

it("keeps the original market categories and marks only what this Go build runs", () => {
  expect(pluginTypes.map((t) => t[1])).toEqual([
    "支付插件",
    "分账插件",
    "投诉插件",
    "进件插件",
    "短信插件",
    "邮件插件",
    "功能扩展",
    "模板",
  ]);
  const types = new Set(items.map((i) => i.type));
  for (const [type] of pluginTypes) expect(types.has(type)).toBe(true);
  expect(builtinPlugin({ type: "payment", name: "alipay" })).toBeDefined();
  expect(builtinPlugin({ type: "payment", name: "wxpay" })).toBeDefined();
  // Every payment plugin the Go build registers is now marked 已内置; its
  // built-in set is derived from the plugin registry (ported-plugins.json),
  // so a ported plugin like epay is installed, not just listed in the market.
  expect(builtinPlugin({ type: "payment", name: "epay" })).toBeDefined();
  expect(builtinPlugin({ type: "payment", name: "swiftpass2" })).toBeDefined();
  // A plugin with no Go implementation stays market-only.
  expect(builtinPlugin({ type: "sms", name: "aliyun" })).toBeUndefined();
  expect(builtinPlugin({ type: "applyments", name: "lakala" })).toBeUndefined();
  // Templates are keyed by kind, so home/pay/qrpay defaults stay distinct.
  expect(
    builtinPlugin({ type: "template", sub: "pay", name: "default" }),
  ).toBeDefined();
  expect(
    builtinPlugin({ type: "template", sub: "home", name: "index7" }),
  ).toBeUndefined();
  expect(builtinCount(items)).toBe(65);
});

it("filters by category and by title or identifier", () => {
  const payment = filterCatalog(items, "payment", "");
  expect(payment.length).toBeGreaterThan(50);
  expect(payment.every((i) => i.type === "payment")).toBe(true);
  expect(filterCatalog(items, "payment", "ALIPAY").length).toBeGreaterThan(1);
  expect(
    filterCatalog(items, "payment", "微信官方").map((i) => i.name),
  ).toContain("wxpay");
  const localPay = localPlugins(items, "payment", "").map((i) => i.name);
  // Driven by the Go plugin registry (ported-plugins.json): the native
  // adapters plus every ported payment plugin, in catalog order.
  expect(localPay.length).toBe(59);
  expect(localPay).toContain("alipay");
  expect(localPay).toContain("wxpay");
  expect(localPay).toContain("epay");
  expect(localPay).toContain("sandpay");
  expect(localPay).not.toContain("togoodfin"); // market-only, no Go port
  expect(localPlugins(items, "sms", "")).toEqual([]);
  expect(priceLabel("0.00")).toBe("免费");
  expect(priceLabel("200.00")).toBe("¥200.00");
});

it("the market page lists the original plugins with their Go state and a detail drawer", async () => {
  const w = mount(AppStorePage, { attachTo: document.body });
  await vi.waitFor(() => expect(w.text()).not.toContain("正在读取应用列表"));
  await flushPromises();
  expect(w.findAll('[role="tab"]').map((t) => t.text())).toContain("进件插件");
  expect(w.text()).toContain("支付宝官方支付");
  expect(w.text()).toContain("已内置");
  expect(w.text()).toContain("暂不支持");
  expect(w.findAll("button").some((b) => /下载|购买|安装/.test(b.text()))).toBe(
    false,
  );
  await w.get('input[aria-label="搜索应用名称或标识"]').setValue("togoodfin");
  await w.get("form").trigger("submit");
  expect(w.findAll("tbody tr").length).toBeGreaterThan(0);
  expect(w.get("tbody").text()).toContain("通莞金服");
  await w.get('button[aria-label="应用详情 通莞金服"]').trigger("click");
  const drawer = w.get('[role="dialog"][aria-label="应用详情"]');
  expect(drawer.text()).toContain("Go 版尚未实现该插件");
  w.unmount();
});

it("the local plugin page shows only built-in plugins of the chosen category", async () => {
  const w = mount(LocalPluginsPage, { attachTo: document.body });
  await vi.waitFor(() => expect(w.text()).not.toContain("正在读取插件列表"));
  await flushPromises();
  expect(w.findAll('[role="tab"]').map((t) => t.text())).not.toContain("模板");
  // Every ported payment plugin (native + registry) shows on 本地插件.
  expect(w.findAll("tbody tr").length).toBe(59);
  expect(w.get("tbody").text()).toContain("威富通MD5");
  const mail = w.findAll('[role="tab"]').find((t) => t.text() === "邮件插件")!;
  await mail.trigger("click");
  expect(w.get("tbody").text()).toContain("smtp");
  const sms = w.findAll('[role="tab"]').find((t) => t.text() === "短信插件")!;
  await sms.trigger("click");
  expect(w.get("tbody").text()).toContain("暂无数据");
  w.unmount();
});
