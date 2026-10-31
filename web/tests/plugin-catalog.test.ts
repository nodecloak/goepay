// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import catalog from "../src/data/plugin-catalog.json";
import {
  builtinPlugin,
  builtinCount,
  builtinTemplates,
  filterCatalog,
  localPlugins,
  priceLabel,
  pluginTypes,
  withBuiltinTemplates,
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
  // Templates are keyed by kind, so home/pay/qrpay defaults stay distinct,
  // and every original home template (index1-index10) is built in.
  expect(
    builtinPlugin({ type: "template", sub: "pay", name: "default" }),
  ).toBeDefined();
  expect(
    builtinPlugin({ type: "template", sub: "home", name: "index7" }),
  ).toBeDefined();
  expect(
    builtinPlugin({ type: "template", sub: "pay", name: "index7" }),
  ).toBeUndefined();
  expect(builtinCount(items)).toBe(75);
});

it("adds GOEPAY's own templates to the market and orders templates by kind", () => {
  const all = withBuiltinTemplates(items);
  const templates = all.filter((i) => i.type === "template");
  expect(templates.map((i) => `${i.sub}:${i.name}`)).toEqual([
    "home:default",
    ...Array.from({ length: 10 }, (_, n) => `home:index${n + 1}`),
    "home:minimal",
    "home:sakura",
    "home:starry",
    "home:manga",
    "home:mecha",
    "home:candy",
    "home:custom",
    "pay:default",
    "pay:compact",
    "pay:anime",
    "qrpay:default",
    "qrpay:compact",
  ]);
  expect(templates.every((i) => builtinPlugin(i))).toBe(true);
  const minimal = templates.find((i) => i.name === "minimal")!;
  expect(minimal.author).toBe("GOEPAY");
  expect(minimal.title).toBe("单卡极简首页模板");
  // GOEPAY's own styles are not described as redone original templates.
  for (const name of ["minimal", "sakura", "candy"])
    expect(
      builtinPlugin({ type: "template", sub: "home", name })!.note,
    ).not.toContain("按原模板");
  expect(
    builtinPlugin({ type: "template", sub: "home", name: "index3" })!.note,
  ).toContain("按原模板");
  const anime = templates.find((i) => i.sub === "pay" && i.name === "anime")!;
  expect(anime.title).toBe("二次元支付页模板");
  // Every generated template is in the market exactly once.
  expect(templates.length).toBe(builtinTemplates.length);
  // Other categories keep the original order and size.
  expect(all.filter((i) => i.type !== "template")).toEqual(
    items.filter((i) => i.type !== "template"),
  );
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
  expect(w.text()).not.toContain("不能在线购买");
  expect(w.findAll("button").some((b) => /下载|购买|安装/.test(b.text()))).toBe(
    false,
  );
  await w.get('input[aria-label="搜索应用名称或标识"]').setValue("togoodfin");
  await w.get("form").trigger("submit");
  expect(w.findAll("tbody tr").length).toBeGreaterThan(0);
  expect(w.get("tbody").text()).toContain("通莞金服");
  await w.get('button[aria-label="应用详情 通莞金服"]').trigger("click");
  const drawer = w.get('[role="dialog"][aria-label="应用详情"]');
  expect(drawer.text()).toContain("该插件尚未实现，暂不可使用");
  w.unmount();
});

it("the market pages long lists and shows every template as installed", async () => {
  const w = mount(AppStorePage, { attachTo: document.body });
  await vi.waitFor(() => expect(w.text()).not.toContain("正在读取应用列表"));
  await flushPromises();
  const payments = filterCatalog(items, "payment", "").length;
  expect(w.findAll("tbody tr").length).toBe(20);
  expect(w.get(".pa-pager").text()).toContain(`共 ${payments} 条`);
  const first = w.get("tbody tr").text();
  await w.get('button[aria-label="下一页"]').trigger("click");
  expect(w.get("tbody tr").text()).not.toBe(first);
  // Switching tabs starts again at page 1; the templates fill more than one
  // page, so walk every page.
  const tpl = w.findAll('[role="tab"]').find((t) => t.text() === "模板")!;
  await tpl.trigger("click");
  expect(w.findAll("tbody tr").length).toBe(
    Math.min(20, builtinTemplates.length),
  );
  expect(w.get(".pa-pager").text()).toContain(
    `共 ${builtinTemplates.length} 条`,
  );
  const rows: string[] = [],
    shots: (string | undefined)[] = [];
  for (let n = 0; n < 10; n++) {
    rows.push(...w.findAll("tbody tr").map((r) => r.text()));
    // Every template row carries its preview picture.
    shots.push(...w.findAll("img.plugin-shot").map((i) => i.attributes("src")));
    const next = w.get('button[aria-label="下一页"]');
    if (next.attributes("disabled") !== undefined) break;
    await next.trigger("click");
  }
  expect(rows.length).toBe(builtinTemplates.length);
  expect(rows.every((r) => r.includes("已内置"))).toBe(true);
  const all = rows.join("\n");
  expect(all).toContain("深色科技风首页模板");
  expect(all).toContain("单卡极简首页模板");
  expect(all).toContain("樱花物语二次元首页模板");
  expect(all).toContain("二次元支付页模板");
  expect(all).not.toContain("暂不支持");
  expect(shots.length).toBe(builtinTemplates.length);
  expect(shots).toContain("/static/template-previews/home-index7.jpg?v=3");
  expect(shots).toContain("/static/template-previews/home-sakura.jpg?v=3");
  expect(shots).toContain("/static/template-previews/pay-anime.jpg");
  expect(shots).toContain("/static/template-previews/qrpay-compact.jpg");
  // Back on page 1, the drawer shows the picture large.
  for (
    let n = 0;
    n < 10 &&
    w.get('button[aria-label="上一页"]').attributes("disabled") === undefined;
    n++
  )
    await w.get('button[aria-label="上一页"]').trigger("click");
  await w
    .get('button[aria-label="应用详情 深色科技风首页模板"]')
    .trigger("click");
  const big = w.get(
    '[role="dialog"][aria-label="应用详情"] img.plugin-preview',
  );
  expect(big.attributes("src")).toBe(
    "/static/template-previews/home-index7.jpg?v=3",
  );
  expect(big.attributes("alt")).toBe("深色科技风首页模板 预览截图");
  w.unmount();
});

it("the local plugin page shows only built-in plugins of the chosen category", async () => {
  const w = mount(LocalPluginsPage, { attachTo: document.body });
  await vi.waitFor(() => expect(w.text()).not.toContain("正在读取插件列表"));
  await flushPromises();
  expect(w.findAll('[role="tab"]').map((t) => t.text())).not.toContain("模板");
  // Every ported payment plugin (native + registry) shows on 本地插件, 20 a page.
  expect(w.findAll("tbody tr").length).toBe(20);
  expect(w.get(".pa-pager").text()).toContain("共 59 条");
  await w.get('input[aria-label="搜索插件名称或标识"]').setValue("威富通");
  await w.get("form").trigger("submit");
  expect(w.get("tbody").text()).toContain("威富通MD5");
  await w
    .findAll("button")
    .find((b) => b.text() === "重置")!
    .trigger("click");
  const mail = w.findAll('[role="tab"]').find((t) => t.text() === "邮件插件")!;
  await mail.trigger("click");
  expect(w.get("tbody").text()).toContain("smtp");
  const sms = w.findAll('[role="tab"]').find((t) => t.text() === "短信插件")!;
  await sms.trigger("click");
  expect(w.get("tbody").text()).toContain("暂无数据");
  w.unmount();
});

// The market advertises three paid addons (支付宝批量转账有密 / 支付宝安全发 /
// 微信客服支付) captured from epay_pro, but this Go build ships no page, API or
// adapter for any of them. They MUST stay 暂不支持 so the console never claims a
// plugin the service cannot run; this guards against a future edit silently
// adding them to builtinPlugins without the backing implementation.
const unimplementedAddons = ["alipayfund", "alipaysatf", "wxkfpay"] as const;

it("keeps claimed-but-unimplemented addons marked 暂不支持", () => {
  for (const name of unimplementedAddons) {
    const entry = items.find((i) => i.type === "addons" && i.name === name);
    expect(entry, `addon ${name} missing from catalog`).toBeDefined();
    expect(
      builtinPlugin({ type: "addons", name }),
      `addon ${name} must not be marked 已内置`,
    ).toBeUndefined();
  }
  // None of them surface on 本地插件 either (that list is built-ins only).
  expect(localPlugins(items, "addons", "").map((i) => i.name)).not.toEqual(
    expect.arrayContaining([...unimplementedAddons]),
  );
});

it("the market tags unimplemented addons 暂不支持 with the honest Go-state note", async () => {
  const w = mount(AppStorePage, { attachTo: document.body });
  await vi.waitFor(() => expect(w.text()).not.toContain("正在读取应用列表"));
  await flushPromises();
  const addons = w
    .findAll('[role="tab"]')
    .find((t) => t.text() === "功能扩展")!;
  await addons.trigger("click");
  const body = w.get("tbody");
  expect(body.text()).toContain("支付宝安全发");
  expect(body.text()).toContain("微信客服支付");
  // No addon row claims support.
  const rows = w.findAll("tbody tr");
  for (const name of unimplementedAddons) {
    const row = rows.find((r) => r.text().includes(name));
    expect(row, `row for ${name}`).toBeTruthy();
    expect(row!.text()).toContain("暂不支持");
    expect(row!.text()).not.toContain("已内置");
  }
  await w.get('button[aria-label="应用详情 支付宝安全发"]').trigger("click");
  const drawer = w.get('[role="dialog"][aria-label="应用详情"]');
  expect(drawer.text()).toContain("暂不支持");
  expect(drawer.text()).toContain("该插件尚未实现，暂不可使用");
  w.unmount();
});
