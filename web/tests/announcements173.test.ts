import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import AnnouncementsPage from "../src/components/AnnouncementsPage.vue";
import { ApiClient } from "../src/api";
const row = {
  id: "9007199254740993",
  content: "原文",
  title: "旧标题",
  sort: 50,
  color: "",
  content_format: "text",
  audience: "all",
  enabled: true,
};
const schema = {
  title: "公告管理",
  fields: [],
  permissions: { create: true, update: true, delete: true },
};
it("a late list from a previous client cannot restore the old announcement", async () => {
  let resolve!: (value: unknown) => void;
  const pending = new Promise((r) => (resolve = r)),
    first = new ApiClient("admin"),
    second = new ApiClient("admin");
  first.csrfToken = "first";
  second.csrfToken = "second";
  first.request = vi
    .fn()
    .mockResolvedValueOnce(schema)
    .mockReturnValueOnce(pending) as ApiClient["request"];
  second.request = vi
    .fn()
    .mockResolvedValueOnce(schema)
    .mockResolvedValueOnce({
      items: [{ ...row, id: "9007199254740995", content: "当前公告" }],
      total: 1,
    }) as ApiClient["request"];
  const w = mount(AnnouncementsPage, {
    props: { client: first, ownSession: true },
  });
  await flushPromises();
  await w.setProps({ client: second });
  await flushPromises();
  resolve({ items: [{ ...row, content: "过期公告" }], total: 1 });
  await flushPromises();
  expect(w.text()).toContain("当前公告");
  expect(w.text()).not.toContain("过期公告");
  w.unmount();
});
it("a late sent result cannot announce success after a same-client session replacement", async () => {
  let resolve!: (value: unknown) => void;
  const pending = new Promise((r) => (resolve = r)),
    { c, request } = setup(() => pending),
    w = mount(AnnouncementsPage, { props: { client: c, ownSession: true } });
  await flushPromises();
  await openAdd(w);
  const input = w.get("[contenteditable]");
  input.element.textContent = "公告";
  await input.trigger("input");
  await w.get("form").trigger("submit");
  c.csrfToken = "replacement";
  resolve(row);
  await flushPromises();
  expect(request.mock.calls.filter((x) => x[1]?.method)).toHaveLength(1);
  expect(w.text()).not.toContain("添加公告成功");
  expect(w.text()).toContain("会话已变化");
  w.unmount();
});
function setup(write?: () => Promise<unknown>) {
  const c = new ApiClient("admin");
  c.csrfToken = "ann173";
  const request = vi.fn(async (path: string, options?: { method?: string }) => {
    if (options?.method) return write ? write() : row;
    if (path.endsWith("/schema")) return schema;
    if (path.endsWith("/" + row.id)) return row;
    return { items: [row], total: 1 };
  });
  c.request = request as ApiClient["request"];
  return { c, request };
}
async function openAdd(w: ReturnType<typeof mount>) {
  await w
    .findAll("button")
    .find((b) => b.text() === "添加公告")!
    .trigger("click");
  await flushPromises();
}
it("uses original two cards and content/sort/color without an extra required title", async () => {
  const { c, request } = setup();
  const w = mount(AnnouncementsPage, {
    props: { client: c, ownSession: true },
  });
  await flushPromises();
  expect(w.text()).toContain("添加公告");
  expect(w.text()).toContain("已发公告");
  expect(w.find('input[aria-label="标题"]').exists()).toBe(false);
  await openAdd(w);
  const editor = w.get("[contenteditable]");
  editor.element.innerHTML = "<p>新公告</p>";
  await editor.trigger("input");
  await w.get('input[aria-label="排序"]').setValue("20");
  await w.get('input[aria-label="公告文字颜色"]').setValue("#123456");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(request.mock.calls.find((x) => x[1]?.method === "POST")).toEqual([
    "/resources/announcements",
    {
      method: "POST",
      body: {
        content: "<p>新公告</p>",
        sort: 20,
        color: "#123456",
        content_format: "html",
        audience: "merchant",
        enabled: true,
      },
      signal: expect.any(AbortSignal),
    },
  ]);
  expect(w.text()).toContain("添加公告成功");
  w.unmount();
});
it("fresh-reads a full string ID and preserves legacy fields by changing only original edit fields", async () => {
  const { c, request } = setup();
  const w = mount(AnnouncementsPage, {
    props: { client: c, ownSession: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "编辑")!
    .trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/resources/announcements/" + row.id,
    expect.any(Object),
  );
  expect(w.find("[contenteditable]").text()).toBe("原文");
  await w.get("form").trigger("submit");
  await flushPromises();
  const call = request.mock.calls.find((x) => x[1]?.method === "PATCH");
  expect(call?.[0]).toBe("/resources/announcements/" + row.id);
  expect(call?.[1]).toMatchObject({
    body: { content: "原文", sort: 50, color: "", content_format: "html" },
  });
  expect(call?.[1]).not.toHaveProperty("body.title");
  expect(call?.[1]).not.toHaveProperty("body.audience");
  w.unmount();
});
it("requires delete confirmation and cancel makes no mutation", async () => {
  const { c, request } = setup();
  const w = mount(AnnouncementsPage, {
    props: { client: c, ownSession: true },
    attachTo: document.body,
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "删除")!
    .trigger("click");
  await flushPromises();
  expect(w.find('[role="dialog"]').text()).toContain("确定要删除此公告吗");
  expect(request.mock.calls.filter((x) => x[1]?.method)).toHaveLength(0);
  await w
    .findAll("button")
    .find((b) => b.text() === "取消")!
    .trigger("click");
  expect(request.mock.calls.filter((x) => x[1]?.method)).toHaveLength(0);
  w.unmount();
});
it("blocks duplicate and uncertain writes until an explicit refresh", async () => {
  let reject!: (e: Error) => void;
  const pending = new Promise((_, r) => (reject = r));
  const { c, request } = setup(() => pending);
  const w = mount(AnnouncementsPage, {
    props: { client: c, ownSession: true },
  });
  await flushPromises();
  await openAdd(w);
  const editor = w.get("[contenteditable]");
  editor.element.textContent = "公告";
  await editor.trigger("input");
  await w.get("form").trigger("submit");
  await w.get("form").trigger("submit");
  expect(request.mock.calls.filter((x) => x[1]?.method)).toHaveLength(1);
  reject(new Error("connection lost"));
  await flushPromises();
  await w.get("form").trigger("submit");
  expect(request.mock.calls.filter((x) => x[1]?.method)).toHaveLength(1);
  expect(w.text()).toContain("刷新核对");
  expect(w.get("[contenteditable]").text()).toBe("公告");
  w.unmount();
});
it("an in-place CSRF change prevents an old draft from being submitted", async () => {
  const { c, request } = setup();
  const w = mount(AnnouncementsPage, {
    props: { client: c, ownSession: true },
  });
  await flushPromises();
  await openAdd(w);
  const editor = w.get("[contenteditable]");
  editor.element.textContent = "公告";
  await editor.trigger("input");
  c.csrfToken = "other-session";
  await w.get("form").trigger("submit");
  expect(request.mock.calls.filter((x) => x[1]?.method)).toHaveLength(0);
  expect(w.text()).not.toContain("添加公告成功");
  w.unmount();
});
