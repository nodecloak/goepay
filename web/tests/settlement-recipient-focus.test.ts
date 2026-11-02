// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { ApiClient, ApiError } from "../src/api";
import Panel from "../src/components/SettlementRecipientPanel.vue";

const views: VueWrapper[] = [];
afterEach(() => {
  views.splice(0).forEach((view) => view.unmount());
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});
const recipient = (id = "42") => ({
  state: "configured",
  config_id: id,
  type: "alipay",
  app_id: "",
  account_masked: "***1234",
  name_masked: "***",
});
const button = (view: VueWrapper, name: string) =>
  view.findAll("button").find((item) => item.text() === name)!;
async function fixture(
  result:
    | "success"
    | "unknown"
    | "conflict"
    | "conflict-readback-failure" = "success",
) {
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic-focus-224";
  let posted = false;
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      if (options?.method === "POST") {
        posted = true;
        if (result === "unknown") throw Error("synthetic lost response");
        if (result === "conflict" || result === "conflict-readback-failure")
          throw new ApiError("synthetic conflict", 409, -1);
        return recipient("43") as never;
      }
      if (posted && result === "conflict-readback-failure")
        throw Error("synthetic readback failure");
      return (
        path.includes("allowed-types")
          ? { settlement: ["alipay", "wxpay"], can_write: true }
          : { ...recipient(posted ? "43" : "42"), can_write: true }
      ) as never;
    });
  const view = mount(Panel, {
    attachTo: document.body,
    props: { client, admin: false, disabled: false },
  });
  views.push(view);
  await flushPromises();
  return { view, request };
}
async function open(view: VueWrapper) {
  const opener = button(view, "修改收款资料");
  (opener.element as HTMLButtonElement).focus();
  await opener.trigger("click");
  await flushPromises();
}
async function save(view: VueWrapper) {
  await open(view);
  await view.get('[aria-label="收款账号"]').setValue("synthetic-account-224");
  await view.get('[aria-label="真实姓名"]').setValue("Synthetic Name");
  await button(view, "保存收款版本").trigger("click");
  await flushPromises();
}
it("focuses the first edit control and returns to the remounted edit button on cancel without writing", async () => {
  const { view, request } = await fixture();
  await open(view);
  expect(document.activeElement).toBe(
    view.get('[aria-label="收款方式"]').element,
  );
  await button(view, "取消修改收款资料").trigger("click");
  await flushPromises();
  expect(document.activeElement).toBe(button(view, "修改收款资料").element);
  expect(
    request.mock.calls.every(
      ([, options]) => !options?.method || options.method === "GET",
    ),
  ).toBe(true);
});
it("returns to edit after a mocked successful save", async () => {
  const { view, request } = await fixture();
  await save(view);
  expect(view.text()).toContain("版本 43");
  expect(document.activeElement).toBe(button(view, "修改收款资料").element);
  expect(
    request.mock.calls.filter(([, options]) => options?.method === "POST"),
  ).toHaveLength(1);
});
it("focuses the readback action after an unknown save without repeating the POST", async () => {
  const { view, request } = await fixture("unknown");
  await save(view);
  expect(view.text()).toContain("保存结果尚未确认");
  expect(button(view, "修改收款资料").attributes("disabled")).toBeDefined();
  expect(document.activeElement).toBe(button(view, "刷新收款版本").element);
  expect(
    request.mock.calls.filter(([, options]) => options?.method === "POST"),
  ).toHaveLength(1);
});
it("returns to edit after a conflict's authoritative readback, without retrying the write", async () => {
  const { view, request } = await fixture("conflict");
  await save(view);
  expect(view.text()).toContain("收款版本已变化");
  expect(document.activeElement).toBe(button(view, "修改收款资料").element);
  expect(
    request.mock.calls.filter(([, options]) => options?.method === "POST"),
  ).toHaveLength(1);
});
it("focuses the read-only refresh action when a conflict's readback fails", async () => {
  const { view, request } = await fixture("conflict-readback-failure");
  await save(view);
  expect(view.text()).toContain("synthetic readback failure");
  expect(button(view, "修改收款资料").attributes("disabled")).toBeDefined();
  expect(document.activeElement).toBe(button(view, "刷新收款版本").element);
  expect(
    request.mock.calls.filter(([, options]) => options?.method === "POST"),
  ).toHaveLength(1);
});
it("does not move focus into a replacement merchant context after cancel", async () => {
  const { view } = await fixture();
  await open(view);
  const outside = document.createElement("button");
  document.body.append(outside);
  const next = new ApiClient("merchant");
  next.csrfToken = "replacement-focus-224";
  vi.spyOn(next, "request").mockImplementation(
    async (path) =>
      (path.includes("allowed-types")
        ? { settlement: ["alipay"], can_write: true }
        : { ...recipient("77"), can_write: true }) as never,
  );
  (button(view, "取消修改收款资料").element as HTMLButtonElement).click();
  const changed = view.setProps({ client: next });
  outside.focus();
  await changed;
  await flushPromises();
  expect(document.activeElement).toBe(outside);
});
it("does not steal focus from another dialog when a save finishes later", async () => {
  const { view, request } = await fixture();
  let finish!: (value: unknown) => void;
  request.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }) as never,
  );
  await save(view);
  const dialog = document.createElement("div"),
    control = document.createElement("button");
  dialog.setAttribute("role", "dialog");
  dialog.append(control);
  document.body.append(dialog);
  control.focus();
  finish(recipient("43"));
  await flushPromises();
  expect(document.activeElement).toBe(control);
  expect(
    request.mock.calls.filter(([, options]) => options?.method === "POST"),
  ).toHaveLength(1);
});
