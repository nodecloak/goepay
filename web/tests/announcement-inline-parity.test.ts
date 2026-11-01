import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import AnnouncementsPage from "../src/components/AnnouncementsPage.vue";
import { ApiClient } from "../src/api";
const item = {
  id: "9007199254740993",
  content: "Existing",
  content_format: "text",
  enabled: true,
  sort: 50,
  color: "",
};
function setup(write?: (path: string, options: any) => Promise<unknown>) {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      if (options?.method && options.method !== "GET")
        return (write ? await write(path, options) : item) as never;
      if (path.endsWith("/schema"))
        return {
          fields: [],
          permissions: { create: true, update: true, delete: true },
        } as never;
      if (path.endsWith("/" + item.id)) return item as never;
      return { items: [item], total: 1 } as never;
    });
  const view = mount(AnnouncementsPage, {
    props: { client, ownSession: true },
    attachTo: document.body,
  });
  return { view, request };
}
it("shows an inline editor by default and keeps the existing list available", async () => {
  const { view } = setup();
  try {
    await flushPromises();
    expect(view.find('[role="region"][aria-label="添加公告"]').exists()).toBe(
      true,
    );
    expect(view.find('[role="dialog"]').exists()).toBe(false);
    expect(view.get('[aria-label="已发公告"]').text()).toContain("Existing");
    await view.get('[aria-label="排序"]').setValue("8");
    await view
      .findAll("button")
      .find((b) => b.text() === "添加公告")!
      .trigger("click");
    expect(
      (view.get('[aria-label="排序"]').element as HTMLInputElement).value,
    ).toBe("8");
  } finally {
    view.unmount();
  }
});
it("keeps an inline draft after an unknown save and blocks duplicate submission", async () => {
  let reject!: (e: Error) => void;
  const pending = new Promise((_r, re) => {
    reject = re;
  });
  const { view, request } = setup(() => pending);
  try {
    await flushPromises();
    const editor = view.get('[contenteditable="true"]');
    editor.element.innerHTML = "<p>Synthetic draft</p>";
    await editor.trigger("input");
    await view.get(".announcement-editor form").trigger("submit");
    await flushPromises();
    await view.get(".announcement-editor form").trigger("submit");
    reject(Error("response lost"));
    await flushPromises();
    expect(
      request.mock.calls.filter(([, o]) => o?.method === "POST"),
    ).toHaveLength(1);
    expect(view.get("[contenteditable]").element.textContent).toContain(
      "Synthetic draft",
    );
    expect(view.get('[role="alert"]').text()).toContain("刷新核对");
    expect(
      view
        .get('.announcement-editor button[type="submit"]')
        .attributes("disabled"),
    ).toBeDefined();
  } finally {
    view.unmount();
  }
});
