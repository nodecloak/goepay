// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import MerchantCreateFields from "../src/components/MerchantCreateFields.vue";
import { ApiClient, ApiError } from "../src/api";

async function fixture(failure: Error, lookup?: () => Promise<unknown>) {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-recovery122";
  const calls: { path: string; method?: string }[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    calls.push({ path, method: options?.method });
    if (options?.method === "POST") throw failure;
    if (path.endsWith("/schema"))
      return {
        title: "商户管理",
        permissions: { create: true, update: true, delete: false },
        fields: [
          { name: "creation_mode", type: "select", label: "新增方式" },
          { name: "phone", type: "string", label: "手机号" },
          {
            name: "login_password",
            type: "password",
            label: "密码",
            write_only: true,
          },
        ],
      } as never;
    if (path.includes("column=login_username") && lookup)
      return (await lookup()) as never;
    if (path.includes("column=login_username"))
      return {
        items: [
          { id: "12240", login_username: "13000012240", name: "13000012240" },
        ],
        total: 1,
      } as never;
    return { items: [], total: 0 } as never;
  });
  const w = mount(ResourcePage, {
    props: { client, resource: "merchants" },
    attachTo: document.body,
  });
  await flushPromises();
  const opener = w.findAll("button").find((b) => b.text() === "新增商户")!;
  (opener.element as HTMLElement).focus();
  await opener.trigger("click");
  await flushPromises();
  await w.get("#field-phone").setValue("13000012240");
  await w.get("#field-login_password").setValue("synthetic-recovery-password");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  return { w, calls, opener };
}

it("blocks a repeat create after a lost response and reconciles the original login with GET only", async () => {
  const { w, calls, opener } = await fixture(
    new TypeError("network unavailable"),
  );
  const dialog = w.get('[role="dialog"]');
  expect(dialog.text()).toContain("添加结果尚未确认");
  expect(
    (dialog.get("#field-login_password").element as HTMLInputElement).value,
  ).toBe("");
  await dialog.get("form").trigger("submit");
  await flushPromises();
  expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
  const query = dialog
    .findAll("button")
    .find((b) => b.text() === "核对商户账号")!;
  expect(query).toBeDefined();
  await query.trigger("click");
  await flushPromises();
  expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
  expect(
    calls.some(
      (c) =>
        c.path.includes("column=login_username") &&
        c.path.includes("13000012240") &&
        !c.method,
    ),
  ).toBe(true);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).toContain("12240");
  expect(document.activeElement).toBe(opener.element);
  w.unmount();
});

it("keeps a rejected validation draft editable without treating it as an unknown write", async () => {
  const { w, calls } = await fixture(new ApiError("手机号格式不正确", 422, -1));
  expect(w.get('[role="dialog"]').text()).toContain("手机号格式不正确");
  expect(w.get('[role="dialog"]').text()).not.toContain("核对商户账号");
  expect(w.get("#field-phone").attributes("disabled")).toBeUndefined();
  expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
  w.unmount();
});

it.each(["empty", "multiple", "failure"])(
  "keeps unknown creation locked when reconciliation is %s",
  async (kind) => {
    const lookup = async () => {
      if (kind === "failure") throw new Error("lookup unavailable");
      return {
        items:
          kind === "empty"
            ? []
            : [
                { id: "12240", login_username: "13000012240" },
                { id: "12241", login_username: "13000012240" },
              ],
        total: kind === "empty" ? 0 : 2,
      };
    };
    const { w, calls } = await fixture(new TypeError("lost response"), lookup);
    await w
      .findAll('[role="dialog"] button')
      .find((b) => b.text() === "核对商户账号")!
      .trigger("click");
    await flushPromises();
    expect(w.find('[role="dialog"]').exists()).toBe(true);
    expect(w.get('[role="dialog"]').text()).toContain("不会重复添加");
    await w.get('[role="dialog"] form').trigger("submit");
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
    w.unmount();
  },
);

it("erases the original confidential draft object when the resource context changes", async () => {
  const { w } = await fixture(new ApiError("validation rejected", 422, -1));
  const draft = w.findComponent(MerchantCreateFields).props("values");
  expect(draft.login_password).toBe("synthetic-recovery-password");
  await w.setProps({ resource: "groups" });
  await flushPromises();
  expect(draft.login_password).toBeUndefined();
  w.unmount();
});

it.each(["success", "failure"])(
  "discards late reconciliation %s after changing context",
  async (result) => {
    let finish!: (value: unknown) => void, fail!: (error: Error) => void;
    const pending = new Promise((resolve, reject) => {
      finish = resolve;
      fail = reject;
    });
    const { w, calls } = await fixture(
      new TypeError("lost response"),
      () => pending,
    );
    await w
      .findAll('[role="dialog"] button')
      .find((b) => b.text() === "核对商户账号")!
      .trigger("click");
    await w.setProps({ resource: "groups" });
    if (result === "success")
      finish({
        items: [{ id: "12240", login_username: "13000012240" }],
        total: 1,
      });
    else fail(new Error("late-private-error"));
    await flushPromises();
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(w.text()).not.toContain("12240");
    expect(w.text()).not.toContain("late-private-error");
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
    await w
      .findAll("button")
      .find((b) => b.text() === "新增用户组")!
      .trigger("click");
    await flushPromises();
    await w.get('[role="dialog"] form').trigger("submit");
    await flushPromises();
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(2);
    w.unmount();
  },
);
