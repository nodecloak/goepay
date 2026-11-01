import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import { ApiClient } from "../src/api";

const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
});
const fields = [
  { name: "kind", label: "黑名单类型", type: "select" },
  { name: "value", label: "黑名单内容", type: "string" },
  { name: "reason", label: "备注", type: "string" },
  { name: "enabled", label: "启用", type: "boolean" },
  {
    name: "expires_at",
    label: "到期时间（空为永久）",
    type: "string",
    read_only: true,
  },
  { name: "created_at", label: "添加时间", type: "string", read_only: true },
];
function setup(resource = "blacklist") {
  const client = new ApiClient("admin"),
    calls: string[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    calls.push(path);
    if (path.endsWith("/schema"))
      return {
        title: "黑名单管理",
        fields,
        permissions: { create: true, update: true, delete: true },
        exportable: true,
        batch_delete: true,
        filters: [
          {
            param: "kind",
            label: "黑名单类型",
            options: [
              { label: "全部类型", value: "" },
              { label: "域名", value: "domain" },
            ],
          },
        ],
      } as never;
    return {
      items: [
        {
          id: "9007199254740993",
          kind: "domain",
          value: "merchant.example",
          reason: "synthetic",
          enabled: true,
          expires_at: null,
          created_at: "2026-11-01T01:02:03Z",
        },
        {
          id: "9007199254740994",
          kind: "domain",
          value: "example.org",
          reason: "synthetic",
          enabled: false,
          expires_at: "2026-11-02T00:00:00Z",
          created_at: "2026-11-01T01:02:03Z",
        },
      ],
      total: 60,
    } as never;
  });
  const w = mount(ResourcePage, { props: { client, resource } });
  mounted.push(w);
  return { w, calls, client };
}

it("matches the blacklist list labels/order while preserving expiry meaning and exact identifiers", async () => {
  const { w } = setup();
  await flushPromises();
  expect(
    w.get('input[placeholder="黑名单内容"]').attributes("placeholder"),
  ).toBe("黑名单内容");
  expect(w.findAll(".resource-filter .pa-label").map((x) => x.text())).toEqual([
    "搜索",
    "类型",
  ]);
  expect(w.findAll("thead th").map((x) => x.text())).toEqual([
    "",
    "ID",
    "黑名单类型",
    "黑名单内容",
    "添加时间",
    "过期时间",
    "备注",
    "操作",
  ]);
  const rows = w.findAll("tbody tr");
  expect(rows[0].text()).toContain("9007199254740993");
  expect(rows[0].text()).toContain("2026-11-01 09:02:03");
  expect(rows[0].text()).toContain("永久");
  expect(rows[1].text()).toContain("2026-11-02 08:00:00");
  expect(w.findAll("button").some((x) => x.text() === "添加")).toBe(true);
});

it("resets query, kind, page and stale row selection with one matching list read", async () => {
  const { w, calls } = setup();
  await flushPromises();
  await w
    .get('input[aria-label="搜索黑名单管理"]')
    .setValue("merchant.example");
  await w.get('.pa-select-trigger[aria-label="黑名单类型"]').trigger("click");
  await w
    .findAll('[role="option"]')
    .find((x) => x.text() === "域名")!
    .trigger("click");
  await flushPromises();
  await w
    .findAll(".pa-pager button")
    .find((x) => x.text() === "2")!
    .trigger("click");
  await flushPromises();
  await w.get('tbody input[type="checkbox"]').setValue(true);
  expect(w.text()).toContain("批量删除 (1)");
  const before = calls.length;
  await w
    .findAll(".resource-filter button")
    .find((x) => x.text() === "重置")!
    .trigger("click");
  await flushPromises();
  expect(calls.length).toBe(before + 1);
  const url = new URL(calls.at(-1)!, "https://local.example");
  expect(url.searchParams.get("page")).toBe("1");
  expect(url.searchParams.get("q")).toBe("");
  expect(url.searchParams.has("kind")).toBe(false);
  expect(
    (w.get('input[aria-label="搜索黑名单管理"]').element as HTMLInputElement)
      .value,
  ).toBe("");
  expect(
    (w.get('tbody input[type="checkbox"]').element as HTMLInputElement).checked,
  ).toBe(false);
  expect(
    w
      .findAll("button")
      .find((x) => x.text() === "批量删除")!
      .attributes("disabled"),
  ).toBeDefined();
});

it("keeps the existing generic plugin resource presentation", async () => {
  const { w } = setup("plugin-configs");
  await flushPromises();
  expect(w.findAll("thead th").map((x) => x.text())).toEqual([
    "",
    "编号",
    "黑名单类型",
    "黑名单内容",
    "备注",
    "启用",
    "到期时间（空为永久）",
    "添加时间",
    "操作",
  ]);
  expect(w.find('input[placeholder="搜索记录"]').exists()).toBe(true);
  expect(
    w.findAll(".resource-filter button").some((x) => x.text() === "重置"),
  ).toBe(false);
});

it("retains enabled in the existing editor even though it is not a default list column", async () => {
  const { w, client } = setup();
  await flushPromises();
  vi.mocked(client.request).mockResolvedValueOnce({
    id: "9007199254740993",
    kind: "domain",
    value: "merchant.example",
    reason: "synthetic",
    enabled: true,
    expires_at: null,
  });
  await w
    .findAll("tbody tr")[0]
    .findAll("button")
    .find((b) => b.text() === "编辑")!
    .trigger("click");
  await flushPromises();
  expect((w.get("#field-enabled").element as HTMLInputElement).checked).toBe(
    true,
  );
  await w.get("#field-enabled").setValue(false);
  await w.get(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(client.request).toHaveBeenCalledWith(
    "/resources/blacklist/9007199254740993",
    expect.objectContaining({
      method: "PATCH",
      body: expect.objectContaining({ enabled: false }),
    }),
  );
  expect(fields.find((f) => f.name === "expires_at")!.label).toBe(
    "到期时间（空为永久）",
  );
});
