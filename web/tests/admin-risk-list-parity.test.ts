import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import { ApiClient } from "../src/api";

const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
});
function setup(resource: "admin-users" | "risk-records") {
  const client = new ApiClient("admin"),
    calls: string[] = [];
  const admin = resource === "admin-users";
  const fields = admin
    ? [
        { name: "username", label: "账号", type: "string" },
        { name: "display_name", label: "名称", type: "string" },
        {
          name: "admin_class",
          label: "权限等级",
          type: "select",
          options: [
            { value: "super", label: "超级管理员" },
            { value: "operator", label: "普通管理员" },
          ],
        },
        { name: "permissions", label: "菜单权限", type: "permission_tree" },
        { name: "enabled", label: "启用", type: "boolean" },
        {
          name: "created_at",
          label: "添加时间",
          type: "string",
          read_only: true,
        },
        {
          name: "last_login_at",
          label: "上次登录",
          type: "string",
          read_only: true,
        },
        {
          name: "totp_enabled",
          label: "TOTP状态",
          type: "boolean",
          read_only: true,
        },
      ]
    : [
        { name: "merchant_id", label: "商户号", type: "string" },
        {
          name: "decision",
          label: "风控类型",
          type: "select",
          options: [{ value: "deny", label: "拦截" }],
        },
        { name: "reason", label: "风控内容", type: "string" },
        { name: "source", label: "来源", type: "string" },
        { name: "trade_no", label: "订单号", type: "string" },
        { name: "url", label: "风控网址", type: "url" },
        { name: "created_at", label: "时间", type: "string", read_only: true },
      ];
  const item = {
    id: "9007199254740993",
    username: "operator",
    display_name: "操作员",
    admin_class: "operator",
    permissions: ["/trade/order"],
    enabled: true,
    created_at: "2026-11-01T01:02:03Z",
    last_login_at: null,
    totp_enabled: false,
    merchant_id: "101",
    decision: "deny",
    reason: "test",
    source: "manual",
    trade_no: "T1",
    url: "https://shop.example.invalid",
  };
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    calls.push(path);
    if (path.endsWith("/schema"))
      return {
        title: admin ? "管理员列表" : "风控记录",
        fields,
        permissions: { create: true, update: true, delete: false },
      } as never;
    if (path.endsWith("/" + item.id)) return item as never;
    return { items: [item], total: 1 } as never;
  });
  const view = mount(ResourcePage, { props: { client, resource } });
  mounted.push(view);
  return { view, calls };
}
it("shows original administrator columns, real metadata and localized status without exposing the menu tree", async () => {
  const { view, calls } = setup("admin-users");
  await flushPromises();
  expect(view.findAll("thead th").map((x) => x.text())).toEqual([
    "UID",
    "用户名",
    "权限",
    "添加时间",
    "上次登录",
    "TOTP状态",
    "状态",
    "操作",
  ]);
  expect(view.find("tbody").text()).toContain("普通管理员");
  expect(view.find("tbody").text()).toContain("2026-11-01 09:02:03");
  expect(view.find("tbody").text()).toContain("未开启");
  expect(view.find("tbody").text()).not.toContain("/trade/order");
  await view
    .get('input[aria-label="搜索管理员列表"]')
    .setValue("9007199254740993");
  await view.get("form.resource-filter").trigger("submit");
  await flushPromises();
  expect(
    new URL(calls.at(-1)!, "https://local.example").searchParams.get("q"),
  ).toBe("9007199254740993");
  const reset = view
    .findAll(".resource-filter button")
    .find((b) => b.text() === "重置");
  expect(reset).toBeDefined();
  const before = calls.length;
  await reset!.trigger("click");
  await flushPromises();
  expect(calls.length).toBe(before + 1);
});
it("shows risk URL/time while retaining source and trade number in local details", async () => {
  const { view } = setup("risk-records");
  await flushPromises();
  expect(view.findAll("thead th").map((x) => x.text())).toEqual([
    "ID",
    "商户号",
    "风控类型",
    "风控内容",
    "风控网址",
    "时间",
    "操作",
  ]);
  expect(view.find("tbody").text()).toContain("拦截");
  expect(view.find("tbody").text()).toContain("2026-11-01 09:02:03");
  await view
    .findAll("tbody button")
    .find((b) => b.text() === "详情")!
    .trigger("click");
  await flushPromises();
  expect(view.get('[role="dialog"]').text()).toContain("manual");
  expect(view.get('[role="dialog"]').text()).toContain("T1");
});
