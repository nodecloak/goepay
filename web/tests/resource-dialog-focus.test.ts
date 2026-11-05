// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import { ApiClient } from "../src/api";
import type { ResourceEntry } from "../src/resources";

const views: VueWrapper[] = [];
afterEach(() => {
  views.splice(0).forEach((view) => view.unmount());
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});
function setup(entry?: ResourceEntry) {
  vi.spyOn(HTMLElement.prototype, "getClientRects").mockImplementation(
    function (this: HTMLElement) {
      return (this.isConnected && !this.hidden
        ? [new DOMRect(0, 0, 100, 30)]
        : []) as unknown as DOMRectList;
    },
  );
  const client = new ApiClient("admin"),
    writes: string[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    if (options?.method && options.method !== "GET") {
      writes.push(path);
      throw Error("No mutation belongs to a focus interaction");
    }
    if (path.endsWith("/schema"))
      return {
        title: "授权域名",
        permissions: { create: true, update: true, delete: true },
        fields: [
          {
            name: "merchant_id",
            label: "商户 ID",
            type: "string",
            required: true,
          },
          { name: "domain", label: "域名", type: "string", required: true },
          { name: "enabled", label: "启用", type: "boolean" },
        ],
      } as never;
    return { items: [], total: 0 } as never;
  });
  const view = mount(ResourcePage, {
    attachTo: document.body,
    props: { client, resource: "domains", entry },
  });
  views.push(view);
  return { view, writes };
}
async function opened() {
  const state = setup();
  await flushPromises();
  const opener = state.view
    .findAll("button")
    .find((b) => b.text() === "新增授权域名")!;
  (opener.element as HTMLButtonElement).focus();
  await opener.trigger("click");
  await flushPromises();
  return { ...state, opener, dialog: state.view.get('[role="dialog"]') };
}
it("moves focus inside a generic create drawer before keyboard interaction", async () => {
  const { dialog, writes } = await opened();
  expect(document.activeElement).toBe(dialog.get("#field-merchant_id").element);
  expect(writes).toEqual([]);
});
it("wraps Tab and Shift+Tab inside a generic drawer including its header close", async () => {
  const { dialog, writes } = await opened();
  const first = dialog.get('[aria-label="关闭"]').element as HTMLButtonElement;
  const last = dialog.findAll("button").find((b) => b.text() === "保存")!
    .element as HTMLButtonElement;
  last.focus();
  const forward = new KeyboardEvent("keydown", {
    key: "Tab",
    bubbles: true,
    cancelable: true,
  });
  last.dispatchEvent(forward);
  expect(forward.defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(first);
  const back = new KeyboardEvent("keydown", {
    key: "Tab",
    shiftKey: true,
    bubbles: true,
    cancelable: true,
  });
  first.dispatchEvent(back);
  expect(back.defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(last);
  expect(writes).toEqual([]);
});
it("returns focus to the generic create opener after cancel", async () => {
  const { dialog, opener, writes } = await opened();
  const cancel = dialog.findAll("button").find((b) => b.text() === "取消")!;
  (cancel.element as HTMLButtonElement).focus();
  await cancel.trigger("click");
  await flushPromises();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(document.activeElement).toBe(opener.element);
  expect(writes).toEqual([]);
});
it("closes a resource drawer on its scrim and returns focus without writing", async () => {
  const { view, opener, writes } = await opened();
  await view.get(".modal-backdrop").trigger("click");
  await flushPromises();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(document.activeElement).toBe(opener.element);
  expect(writes).toEqual([]);
});
it("keeps a resource drawer open when a field inside it is clicked", async () => {
  const { dialog, view, writes } = await opened();
  await dialog.get("#field-domain").trigger("click");
  await flushPromises();
  expect(view.find('[role="dialog"]').exists()).toBe(true);
  expect(writes).toEqual([]);
});
it("keeps keyboard focus in a temporarily disabled drawer", async () => {
  const { dialog, writes } = await opened();
  for (const element of dialog.findAll("button,input,select,textarea"))
    (element.element as HTMLInputElement).disabled = true;
  const panel = dialog.element as HTMLElement;
  panel.focus();
  const key = new KeyboardEvent("keydown", {
    key: "Tab",
    bubbles: true,
    cancelable: true,
  });
  panel.dispatchEvent(key);
  expect(key.defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(panel);
  expect(writes).toEqual([]);
});
it("uses a current toolbar control if a refreshed list replaced the opener", async () => {
  const { dialog, opener, view, writes } = await opened();
  opener.element.remove();
  const cancel = dialog
    .findAll("button")
    .find((button) => button.text() === "取消")!;
  (cancel.element as HTMLButtonElement).focus();
  await cancel.trigger("click");
  await flushPromises();
  expect(document.activeElement).toBe(
    view.get("[data-resource-refresh]").element,
  );
  expect(writes).toEqual([]);
});
it("does not treat an inline resource entry as a modal or steal outside focus", async () => {
  const outside = document.createElement("button");
  document.body.append(outside);
  outside.focus();
  const { view, writes } = setup({
    mode: "create",
    returnPath: "/admin/merchant/domain",
  });
  await flushPromises();
  expect(view.find('[role="dialog"]').exists()).toBe(false);
  expect(document.activeElement).toBe(outside);
  expect(writes).toEqual([]);
});
