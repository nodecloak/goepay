import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ResourcePage from "../src/components/ResourcePage.vue";
import type { Area } from "../src/menu";

const views: VueWrapper[] = [];
afterEach(() => {
  views.splice(0).forEach((view) => view.unmount());
  vi.restoreAllMocks();
});
function setup(resource: string, area: Area = "admin") {
  const client = new ApiClient(area),
    calls: string[] = [];
  const weixin = resource === "weixin-apps",
    roll = resource === "pay-rolls";
  const fields = weixin
    ? [
        { name: "name", label: "名称", type: "string" },
        { name: "app_id", label: "App ID", type: "string" },
        {
          name: "kind",
          label: "类别",
          type: "select",
          options: [
            { value: "official_account", label: "公众号" },
            { value: "mini_program", label: "小程序" },
          ],
        },
        { name: "enabled", label: "启用资料", type: "boolean" },
      ]
    : roll
      ? [
          { name: "name", label: "名称", type: "string" },
          { name: "type", label: "方式", type: "string" },
          { name: "strategy", label: "策略", type: "string" },
          { name: "channel_ids", label: "通道", type: "json" },
          { name: "enabled", label: "启用", type: "boolean" },
        ]
      : [
          { name: "merchant_id", label: "商户 ID", type: "string" },
          { name: "domain", label: "域名", type: "string" },
          { name: "approved", label: "审核通过", type: "boolean" },
          { name: "enabled", label: "启用", type: "boolean" },
          {
            name: "created_at",
            label: "添加时间",
            type: "string",
            read_only: true,
          },
          {
            name: "updated_at",
            label: "最后修改",
            type: "string",
            read_only: true,
          },
        ];
  const filters = weixin
    ? [
        {
          param: "kind",
          label: "类别",
          options: [
            { value: "", label: "全部类别" },
            { value: "official_account", label: "公众号" },
            { value: "mini_program", label: "小程序" },
          ],
        },
      ]
    : roll
      ? []
      : [
          ...(area === "admin"
            ? [
                {
                  param: "merchant_id",
                  label: "商户号",
                  type: "positive_id",
                  options: [],
                },
              ]
            : []),
          {
            param: "approved",
            label: "状态",
            options: [
              { value: "", label: "全部状态" },
              { value: "false", label: "待审核" },
              { value: "true", label: "已通过" },
            ],
          },
          {
            param: "enabled",
            label: "启用",
            options: [
              { value: "", label: "全部" },
              { value: "true", label: "是" },
              { value: "false", label: "否" },
            ],
          },
        ];
  const item = {
    id: "9007199254740993",
    merchant_id: "9007199254740995",
    domain: "shop.example.com",
    approved: true,
    enabled: false,
    created_at: "2026-11-01T01:02:03Z",
    updated_at: "2026-11-02T01:02:03Z",
    name: "公开应用",
    app_id: "wx-public-001",
    kind: "official_account",
    type: "alipay",
    strategy: "round_robin",
    channel_ids: ["1"],
  };
  const weixinItems = Array.from({ length: 60 }, (_, index) => ({
    id: String(BigInt(item.id) + BigInt(index)),
    revision: "2",
    name:
      index === 0
        ? "公开应用"
        : `${index < 30 ? "公开应用" : "小程序"} ${index + 1}`,
    kind: index < 30 ? "official_account" : "mini_program",
    app_id:
      index < 30
        ? `wx-public-${String(index + 1).padStart(3, "0")}`
        : `wx-mini-${index + 1}`,
    enabled: true,
    archived: false,
    source_channel_id: "0",
    merchant_ids: [],
    path: "",
    fingerprint: "a".repeat(64),
    secret_configured: true,
    draft: true,
    execution_ready: false,
    origin: "managed",
    read_only: false,
  }));
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    calls.push(path);
    if (weixin) {
      if (options?.method && options.method !== "GET")
        throw Error("local editor unexpectedly dispatched a mutation");
      if (path === "/weixin-applications/capabilities")
        return { can_write: true, sources: [] } as never;
      if (path === "/weixin-applications")
        return {
          items: weixinItems,
          total: weixinItems.length,
          can_write: true,
          configuration_errors: [],
        } as never;
      throw Error("unexpected generic Weixin read: " + path);
    }
    if (path.endsWith("/schema"))
      return {
        title: weixin ? "公众号小程序" : roll ? "支付通道轮询" : "授权域名",
        fields,
        filters,
        permissions: { create: true, update: true, delete: true },
        batch_delete: true,
      } as never;
    if (path.endsWith("/" + item.id)) return item as never;
    return { items: [item], total: 60 } as never;
  });
  const view = mount(ResourcePage, {
    props: { client, resource },
    attachTo: weixin ? document.body : undefined,
  });
  views.push(view);
  return { view, calls };
}

it("matches the typed Weixin ID/category/name/APPID/actions header while keeping enabled managed records locally editable", async () => {
  const { view, calls } = setup("weixin-apps");
  await flushPromises();
  expect(
    view.get('input[aria-label="搜索应用"]').attributes("placeholder"),
  ).toBe("ID、名称或APPID");
  expect(
    view
      .get('select[aria-label="筛选类别"]')
      .findAll("option")
      .map((o) => o.text()),
  ).toEqual(["全部类别", "微信公众号", "微信小程序"]);
  expect(
    view
      .findAll("thead th")
      .slice(0, 5)
      .map((x) => x.text()),
  ).toEqual(["ID", "类别", "名称", "APPID", "操作"]);
  await view
    .findAll("tbody button")
    .find((x) => x.text() === "编辑")!
    .trigger("click");
  await flushPromises();
  const enabled = view.get<HTMLInputElement>('input[aria-label="启用资料"]');
  expect(enabled.element.checked).toBe(true);
  await enabled.setValue(false);
  expect(enabled.element.checked).toBe(false);
  expect(view.text()).toContain("启用资料");
  expect(
    view.get('[role="dialog"] input[aria-label="APPSECRET"]').element,
  ).toHaveProperty("value", "");
  expect(calls).toEqual([
    "/weixin-applications",
    "/weixin-applications/capabilities",
  ]);
});

it("applies and resets Weixin search/category/page locally and uses explicit archive without permanent batch deletion", async () => {
  const { view, calls } = setup("weixin-apps");
  await flushPromises();
  const before = calls.length;
  await view.get('input[aria-label="搜索应用"]').setValue("wx-public");
  await view.get('select[aria-label="筛选类别"]').setValue("official_account");
  await view
    .findAll(".weixin-filters button")
    .find((b) => b.text() === "查询")!
    .trigger("click");
  await flushPromises();
  expect(view.find(".pa-pager").text()).toContain("共 30 条");
  await view
    .findAll(".pa-pager button")
    .find((x) => x.text() === "2")!
    .trigger("click");
  await flushPromises();
  expect(view.findAll("tbody tr")).toHaveLength(10);
  expect(view.findAll("tbody td")[0]!.text()).toBe("9007199254741013");
  const reset = view
    .findAll(".weixin-filters button")
    .find((x) => x.text() === "重置");
  expect(reset).toBeDefined();
  await reset!.trigger("click");
  await flushPromises();
  expect(calls.length).toBe(before);
  expect(view.get('input[aria-label="搜索应用"]').element).toHaveProperty(
    "value",
    "",
  );
  expect(view.get('select[aria-label="筛选类别"]').element).toHaveProperty(
    "value",
    "",
  );
  expect(view.get('.pa-pager [aria-current="page"]').text()).toBe("1");
  expect(view.findAll("tbody tr")).toHaveLength(20);
  expect(view.findAll("tbody td")[0]!.text()).toBe("9007199254740993");
  expect(view.find('tbody input[type="checkbox"]').exists()).toBe(false);
  expect(
    view.findAll("button").some((b) => b.text().includes("批量删除")),
  ).toBe(false);
  await view
    .findAll("tbody button")
    .find((b) => b.text() === "归档")!
    .trigger("click");
  await flushPromises();
  expect(view.get('[role="dialog"]').text()).toContain("历史记录继续保留");
  await view
    .findAll('[role="dialog"] button')
    .find((b) => b.text() === "取消")!
    .trigger("click");
  await flushPromises();
  expect(calls.length).toBe(before);
});

it("matches domain list labels without fabricating review time and advertises exact merchant filtering", async () => {
  const { view } = setup("domains");
  await flushPromises();
  expect(
    view.findAll(".resource-filter .pa-label").map((x) => x.text()),
  ).toEqual(["域名", "商户号", "状态"]);
  expect(view.find('input[placeholder="要搜索的域名"]').exists()).toBe(true);
  expect(view.findAll("thead th").map((x) => x.text())).toEqual([
    "",
    "ID",
    "商户号",
    "域名",
    "状态",
    "添加时间",
    "操作",
  ]);
  expect(view.find("tbody").text()).toContain("9007199254740995");
  expect(view.find("tbody").text()).toContain("2026-11-01 09:02:03");
  expect(view.find("table").text()).not.toContain("审核时间");
  expect(view.find('input[aria-label="商户号"]').exists()).toBe(true);
  await view
    .findAll("tbody button")
    .find((x) => x.text() === "编辑")!
    .trigger("click");
  await flushPromises();
  expect(view.find("#field-enabled").exists()).toBe(true);
});

it("resets domain search, merchant and status with one query", async () => {
  const { view, calls } = setup("domains");
  await flushPromises();
  await view
    .get('input[aria-label="搜索授权域名"]')
    .setValue("shop.example.com");
  await view.get('input[aria-label="商户号"]').setValue("9007199254740995");
  await view.get('.pa-select-trigger[aria-label="状态"]').trigger("click");
  await view
    .findAll('[role="option"]')
    .find((x) => x.text() === "已通过")!
    .trigger("click");
  await flushPromises();
  expect(
    new URL(calls.at(-1)!, "https://local.example").searchParams.get(
      "approved",
    ),
  ).toBe("true");
  expect(
    new URL(calls.at(-1)!, "https://local.example").searchParams.get(
      "merchant_id",
    ),
  ).toBe("9007199254740995");
  const before = calls.length;
  const reset = view
    .findAll(".resource-filter button")
    .find((x) => x.text() === "重置");
  expect(reset).toBeDefined();
  await reset!.trigger("click");
  await flushPromises();
  expect(calls.length).toBe(before + 1);
  const u = new URL(calls.at(-1)!, "https://local.example");
  expect(u.searchParams.get("q")).toBe("");
  expect(u.searchParams.has("approved")).toBe(false);
  expect(u.searchParams.has("merchant_id")).toBe(false);
});

it("retains merchant-side domain presentation and the filterless poll-group toolbar", async () => {
  const { view: merchant } = setup("domains", "merchant");
  await flushPromises();
  expect(
    merchant.findAll(".resource-filter .pa-label").map((x) => x.text()),
  ).toEqual(["搜索记录", "状态", "启用"]);
  expect(merchant.findAll("thead th").map((x) => x.text())).toContain("编号");
  const { view: roll } = setup("pay-rolls");
  await flushPromises();
  expect(roll.find(".resource-filter").exists()).toBe(false);
  expect(roll.findAll("thead th").map((x) => x.text())).toContain("ID");
});
