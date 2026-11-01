// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import WorkflowPage from "../src/components/WorkflowPage.vue";
import { ApiClient } from "../src/api";

const views: VueWrapper[] = [];
afterEach(() => {
  views.splice(0).forEach((view) => view.unmount());
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});
async function setup(application = false) {
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
      throw Error("No funds mutation belongs to a focus interaction");
    }
    return { items: [], total: 0, state: "not_configured" } as never;
  });
  const view = mount(WorkflowPage, {
    attachTo: document.body,
    props: {
      client,
      area: "admin",
      workflow: application ? "settlements" : "internal-transfers",
      application,
    },
  });
  views.push(view);
  await flushPromises();
  return { view, writes };
}
async function opened() {
  const state = await setup();
  const opener = state.view
    .findAll("button")
    .find((b) => b.text() === "发起站内转账")!;
  (opener.element as HTMLButtonElement).focus();
  await opener.trigger("click");
  await flushPromises();
  return { ...state, opener, dialog: state.view.get('[role="dialog"]') };
}
it("focuses the first input of the funds request without loading or submitting funds", async () => {
  const { dialog, writes } = await opened();
  expect(document.activeElement).toBe(dialog.find("input").element);
  expect(writes).toEqual([]);
});
it("wraps keyboard focus within the funds dialog and returns it on cancel", async () => {
  const { dialog, opener, writes } = await opened();
  const first = dialog.get('[aria-label="关闭"]').element as HTMLButtonElement;
  const last = dialog
    .findAll("button")
    .filter((b) => b.attributes("disabled") === undefined)
    .at(-1)!.element as HTMLButtonElement;
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
  await dialog
    .findAll("button")
    .find((b) => b.text() === "取消")!
    .trigger("click");
  await flushPromises();
  expect(document.activeElement).toBe(opener.element);
  expect(writes).toEqual([]);
});
it("closes an idle funds request on Escape without submitting", async () => {
  const { dialog, opener, writes } = await opened();
  await dialog.trigger("keydown", { key: "Escape" });
  await flushPromises();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(document.activeElement).toBe(opener.element);
  expect(writes).toEqual([]);
});
it("does not steal or trap focus for the inline withdrawal application", async () => {
  const outside = document.createElement("button");
  document.body.append(outside);
  outside.focus();
  const { view, writes } = await setup(true);
  expect(document.activeElement).toBe(outside);
  expect(view.find('[role="dialog"]').exists()).toBe(false);
  expect(writes).toEqual([]);
});
it("returns to refresh after a mocked sharing decision removes its original row action", async () => {
  vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue([
    new DOMRect(0, 0, 100, 30),
  ] as unknown as DOMRectList);
  const client = new ApiClient("admin");
  let decided = false;
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      if (options?.method === "POST") {
        expect(path).toBe("/workflows/profitsharing/123/reject");
        decided = true;
        return {} as never;
      }
      return {
        items: [{ id: "123", status: decided ? "rejected" : "pending" }],
        total: 1,
      } as never;
    });
  const view = mount(WorkflowPage, {
    attachTo: document.body,
    props: { client, area: "admin", workflow: "profitsharing" },
  });
  views.push(view);
  await flushPromises();
  const opener = view.findAll("button").find((b) => b.text() === "拒绝")!;
  (opener.element as HTMLButtonElement).focus();
  await opener.trigger("click");
  await flushPromises();
  await view
    .get('[role="dialog"]')
    .findAll("button")
    .find((b) => b.text() === "确认拒绝")!
    .trigger("click");
  await flushPromises();
  expect(opener.element.isConnected).toBe(false);
  expect(document.activeElement).toBe(
    view.findAll("button").find((b) => b.text() === "刷新")!.element,
  );
  expect(
    request.mock.calls.filter(([, options]) => options?.method === "POST"),
  ).toHaveLength(1);
});
