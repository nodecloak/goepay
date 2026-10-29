import { flushPromises, mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";
import AnnouncementsPage from "../src/components/AnnouncementsPage.vue";
import CollectionStylesPage from "../src/components/CollectionStylesPage.vue";
import GroupSalesPage from "../src/components/GroupSalesPage.vue";
import LocalPluginsPage from "../src/components/LocalPluginsPage.vue";
import PayTypePage from "../src/components/PayTypePage.vue";
import ResourcePage from "../src/components/ResourcePage.vue";
import SystemUpdatePage from "../src/components/SystemUpdatePage.vue";
import TaskSchedulerPage from "../src/components/TaskSchedulerPage.vue";
import actual from "../e2e/systemupdate183-http-fixture.json";
import { scheduler171Task } from "./scheduler171-fixtures";

// The top of a page (filter card, toolbar, card titles) must look the same
// while it loads as after, so nothing jumps when the data arrives.

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

it("a loading button keeps its width: the spinner takes its room from the side padding", () => {
  const css = readFileSync(resolve(process.cwd(), "src/style.css"), "utf8");
  const rule = (selector: string) => {
    const at = css.indexOf("\n" + selector + " {");
    expect(at, selector).toBeGreaterThan(-1);
    return css.slice(at, css.indexOf("}", at));
  };
  const value = (body: string, prop: string) => {
    const m = body.match(new RegExp("\\s" + prop + ":\\s*([^;]+);"));
    expect(m, prop).toBeTruthy();
    return m![1].trim();
  };
  const base = rule(".pa-btn");
  const loading = rule(".pa-btn--loading");
  const spinner = rule(".pa-btn--loading::before");
  const side = parseFloat(value(base, "padding").split(/\s+/)[1]!);
  expect(value(spinner, "box-sizing")).toBe("border-box");
  expect(
    2 * parseFloat(value(loading, "padding-inline")) +
      parseFloat(value(spinner, "width")) +
      parseFloat(value(loading, "gap")),
  ).toBe(2 * side);
});

describe("ResourcePage first load", () => {
  const schema = {
    title: "黑名单管理",
    description: "黑名单说明",
    fields: [{ name: "value", label: "黑名单内容", type: "string" }],
    permissions: { create: true, update: true, delete: true },
    exportable: true,
    batch_delete: true,
    filters: [
      {
        param: "kind",
        label: "黑名单类型",
        options: [
          { label: "全部类型", value: "" },
          { label: "IP", value: "ip" },
        ],
      },
    ],
  };

  it("shows the toolbar and schema filters with the first page, then keeps them while reloading", async () => {
    const client = new ApiClient("admin");
    let list = deferred<unknown>();
    vi.spyOn(client, "request").mockImplementation(async (path: string) =>
      path.endsWith("/schema")
        ? (schema as never)
        : ((await list.promise) as never),
    );
    const w = mount(ResourcePage, {
      props: { client, resource: "blacklist" },
    });
    await flushPromises();
    const filter = '.pa-select-trigger[aria-label="黑名单类型"]';
    expect(w.find(".loading-row").exists()).toBe(true);
    expect(w.find(".table-tools").exists()).toBe(false);
    expect(w.find(filter).exists()).toBe(false);

    list.resolve({ items: [{ id: "1", value: "10.0.0.1" }], total: 1 });
    await flushPromises();
    const labels = () =>
      w
        .get(".table-tools")
        .findAll("button")
        .map((b) => b.text());
    const loaded = labels();
    expect(loaded.some((t) => t.includes("新增"))).toBe(true);
    expect(loaded.some((t) => t.includes("刷新"))).toBe(true);
    expect(w.find(".csv-export").exists()).toBe(true);
    expect(w.find(filter).exists()).toBe(true);

    list = deferred<unknown>();
    await w
      .get(".table-tools")
      .findAll("button")
      .find((b) => b.text().includes("刷新"))!
      .trigger("click");
    expect(w.find(".loading-row").exists()).toBe(true);
    expect(labels()).toEqual(loaded);
    expect(w.find(filter).exists()).toBe(true);
    list.resolve({ items: [], total: 0 });
    await flushPromises();
    w.unmount();
  });

  it("still offers 刷新 after the first read fails", async () => {
    const client = new ApiClient("admin");
    vi.spyOn(client, "request").mockRejectedValue(new Error("读取失败"));
    const w = mount(ResourcePage, {
      props: { client, resource: "blacklist" },
    });
    await flushPromises();
    expect(w.get(".table-tools").text()).toContain("刷新");
    w.unmount();
  });
});

it("PayTypePage shows its toolbar with the rows, not half-built while they load", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "t";
  const list = deferred<unknown>();
  vi.spyOn(client, "request").mockImplementation(async (path: string) =>
    path.endsWith("/schema")
      ? ({
          title: "支付方式",
          description: "支付方式说明",
          fields: [{ name: "name", label: "展示名称", type: "string" }],
          permissions: { create: true, update: true, delete: true },
          actions: [],
        } as never)
      : ((await list.promise) as never),
  );
  const w = mount(PayTypePage, { props: { client } });
  await flushPromises();
  expect(w.find(".loading-row").exists()).toBe(true);
  expect(w.find(".table-tools").exists()).toBe(false);
  list.resolve({ items: [{ id: "0", type: "alipay", enabled: true }] });
  await flushPromises();
  const tools = w.get(".table-tools").text();
  expect(tools).toContain("新增");
  expect(tools).toContain("刷新");
  expect(tools).toContain("说明");
  w.unmount();
});

it("AnnouncementsPage keeps its refresh label and shows the loading line where the list goes", async () => {
  const list = deferred<unknown>();
  const client = {
    csrfToken: "t",
    area: "admin",
    request: vi.fn().mockImplementation((path: string) =>
      path.endsWith("/schema")
        ? Promise.resolve({
            permissions: { create: true, update: true, delete: true },
          })
        : list.promise,
    ),
  } as unknown as ApiClient;
  const w = mount(AnnouncementsPage, {
    props: { client, ownSession: true },
    global: { stubs: { RichTextEditor: true, RichContent: true } },
  });
  await flushPromises();
  const refresh = () =>
    w.findAll("button").find((b) => b.text() === "刷新核对公告");
  expect(refresh()?.attributes("disabled")).toBeDefined();
  expect(w.get(".announcement-list .empty").text()).toBe("正在加载公告…");
  list.resolve({ items: [], total: 0 });
  await flushPromises();
  expect(refresh()?.attributes("disabled")).toBeUndefined();
  expect(w.get(".announcement-list .empty").text()).toBe("暂无公告");
  w.unmount();
});

it("GroupSalesPage shows its first-load line inside the card, under the title", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockImplementation(
    () => new Promise<never>(() => {}),
  );
  const w = mount(GroupSalesPage, {
    props: { client, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  expect(w.get(".sale-policy-card [role=status]").text()).toBe(
    "正在读取销售配置…",
  );
  expect(w.find(".group-sales > [role=status]").exists()).toBe(false);
  w.unmount();
});

it("CollectionStylesPage keeps its toolbar first and shows the loading line where the cards go", async () => {
  const client = new ApiClient("admin");
  const list = deferred<unknown>();
  vi.spyOn(client, "request").mockImplementation(() => list.promise as never);
  const w = mount(CollectionStylesPage, {
    props: { client, ownSession: true, editing: false },
    global: { stubs: { RouterLink: true } },
  });
  await flushPromises();
  const root = w.get(".collection-styles").element;
  expect(root.firstElementChild?.classList.contains("toolbar")).toBe(true);
  expect(w.get(".loading-row").text()).toBe("正在读取收款码样式…");
  expect(w.find(".pa-pager").exists()).toBe(false);
  list.resolve({ items: [] });
  await flushPromises();
  expect(w.find(".loading-row").exists()).toBe(false);
  expect(w.text()).toContain("暂无收款码样式");
  w.unmount();
});

it("LocalPluginsPage reports when its list has loaded, so the block below can wait for it", async () => {
  const w = mount(LocalPluginsPage);
  expect(w.emitted("loaded")).toBeUndefined();
  await vi.waitFor(() => expect(w.emitted("loaded")).toHaveLength(1));
  await flushPromises();
  expect(w.find(".plugin-page tbody tr").exists()).toBe(true);
  w.unmount();
});

it("TaskSchedulerPage shows its diagnostics row only once the task cards are in", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "t";
  const list = deferred<unknown>();
  c.request = vi
    .fn()
    .mockImplementation(() => list.promise) as unknown as ApiClient["request"];
  const w = mount(TaskSchedulerPage, { props: { client: c, allowed: true } });
  await flushPromises();
  expect(w.text()).toContain("正在读取计划任务…");
  expect(w.find(".task-diagnostics").exists()).toBe(false);
  list.resolve({ items: [scheduler171Task()], total: 1 });
  await flushPromises();
  expect(w.find(".job-card").exists()).toBe(true);
  expect(w.find(".task-diagnostics").exists()).toBe(true);
  w.unmount();
});

it("SystemUpdatePage keeps 重新检测 on the button and shows 正在检测… in the page", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "t";
  const check = deferred<unknown>();
  vi.spyOn(client, "request").mockImplementation(() => check.promise as never);
  const w = mount(SystemUpdatePage, {
    props: { client, ownSession: true, adminClass: "super" },
    global: { stubs: { Teleport: true } },
  });
  await flushPromises();
  const button = () => w.get(".update-heading button");
  expect(button().text()).toBe("重新检测");
  expect(button().attributes("disabled")).toBeDefined();
  expect(w.get("[role=status]").text()).toBe("正在检测…");
  check.resolve(actual.available);
  await flushPromises();
  expect(button().text()).toBe("重新检测");
  expect(w.text()).not.toContain("正在检测…");
  w.unmount();
});
