import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import Page from "../src/components/PayTypePage.vue";

const schema = {
  title: "支付方式",
  fields: [
    { name: "name", type: "string", label: "名称", required: true },
    {
      name: "type",
      type: "string",
      label: "类型",
      required: true,
      create_only: true,
    },
    { name: "device", type: "string", label: "支持设备", default: "0" },
    { name: "enabled", type: "boolean", label: "启用", default: true },
  ],
  permissions: { create: true, update: true, delete: true },
};
const initial = [
  { id: "1", type: "alipay", name: "支付宝", enabled: false, device: "0" },
  { id: "2", type: "wxpay", name: "微信支付", enabled: false, device: "0" },
];
const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
});
function button(w: VueWrapper, name: string) {
  return w.findAll("button").find((b) => b.text() === name)!;
}
function deferred() {
  let resolve!: (v: unknown) => void;
  return {
    promise: new Promise((r) => {
      resolve = r;
    }),
    resolve: (v: unknown) => resolve(v),
  };
}
async function fixture(client = new ApiClient("admin")) {
  client.csrfToken = "synthetic-pay-type-state";
  const rows = initial.map((r) => ({ ...r }));
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      if (path.endsWith("/schema")) return schema as never;
      const id = path.split("/").at(-1);
      if (options?.method === "POST")
        rows.push({ ...(options.body as (typeof initial)[number]), id: "3" });
      if (options?.method === "PATCH")
        Object.assign(
          rows.find((r) => r.id === id)!,
          options.body,
        );
      if (options?.method === "DELETE")
        rows.splice(
          rows.findIndex((r) => r.id === id),
          1,
        );
      if (/\/\d+$/.test(path)) return rows.find((r) => r.id === id) as never;
      return { items: rows.map((r) => ({ ...r })) } as never;
    });
  const w = mount(Page, {
    props: { client },
    global: {
      stubs: {
        KitModal: {
          props: ["title", "loading"],
          emits: ["close", "submit"],
          template:
            '<section role="dialog"><h2>{{ title }}</h2><form @submit.prevent="$emit(\'submit\')"><slot /><button type="submit" :disabled="loading">确认</button></form></section>',
        },
      },
    },
  });
  mounted.push(w);
  await flushPromises();
  return { w, request, client, rows };
}

it("releases switch busy state after refreshing saved rows so a second type can be changed", async () => {
  const { w, request } = await fixture();
  await w.findAll('input[role="switch"]')[0]!.setValue(true);
  await flushPromises();
  expect(
    w
      .findAll('input[role="switch"]')
      .every((s) => s.attributes("disabled") === undefined),
  ).toBe(true);
  await w.findAll('input[role="switch"]')[1]!.setValue(true);
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "PATCH"),
  ).toHaveLength(2);
  expect(button(w, "刷新").attributes("disabled")).toBeUndefined();
});

it.each(["create", "edit", "delete"] as const)(
  "releases busy state after %s and its list refresh",
  async (kind) => {
    const { w } = await fixture();
    await button(
      w,
      kind === "create" ? "新增" : kind === "edit" ? "编辑" : "删除",
    ).trigger("click");
    await flushPromises();
    if (kind === "create") await w.get("#field-type").setValue("qqpay");
    if (kind !== "delete") await w.get("#field-name").setValue("本地支付方式");
    await w.get('[role="dialog"] form').trigger("submit");
    await flushPromises();
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(button(w, "新增").attributes("disabled")).toBeUndefined();
    expect(button(w, "刷新").attributes("disabled")).toBeUndefined();
    await button(w, "新增").trigger("click");
    expect(w.find('[role="dialog"]').exists()).toBe(true);
  },
);

it("an older context response cannot unlock or overwrite a newer pending operation", async () => {
  const f = await fixture();
  const old = deferred();
  f.request.mockImplementation(async (path, options) =>
    options?.method
      ? (old.promise as never)
      : path.endsWith("/schema")
        ? (schema as never)
        : ({ items: initial } as never),
  );
  await f.w.findAll('input[role="switch"]')[0]!.setValue(true);
  await flushPromises();
  const current = new ApiClient("admin");
  current.csrfToken = "replacement-pay-type-state";
  const fresh = deferred();
  const calls = vi
    .spyOn(current, "request")
    .mockImplementation(async (path, options) =>
      options?.method
        ? (fresh.promise as never)
        : path.endsWith("/schema")
          ? (schema as never)
          : ({ items: initial } as never),
    );
  await f.w.setProps({ client: current });
  await flushPromises();
  expect(
    f.w.findAll('input[role="switch"]')[1]!.attributes("disabled"),
  ).toBeUndefined();
  await f.w.findAll('input[role="switch"]')[1]!.setValue(true);
  await flushPromises();
  old.resolve({});
  await flushPromises();
  expect(
    f.w.findAll('input[role="switch"]')[0]!.attributes("disabled"),
  ).toBeDefined();
  fresh.resolve({});
  await flushPromises();
  expect(calls.mock.calls.filter(([, o]) => o?.method)).toHaveLength(1);
  expect(
    f.w.findAll('input[role="switch"]')[0]!.attributes("disabled"),
  ).toBeUndefined();
});

it("a failed toggle refreshes once and releases the page without retrying the write", async () => {
  const f = await fixture();
  f.request.mockImplementation(async (path, options) => {
    if (options?.method) throw new Error("synthetic type update failed");
    return path.endsWith("/schema")
      ? (schema as never)
      : ({ items: initial } as never);
  });
  await f.w.findAll('input[role="switch"]')[0]!.setValue(true);
  await flushPromises();
  expect(f.w.text()).toContain("synthetic type update failed");
  expect(button(f.w, "刷新").attributes("disabled")).toBeUndefined();
  expect(f.request.mock.calls.filter(([, o]) => o?.method)).toHaveLength(1);
});

it("drops an old operation after a raw same-client CSRF change and reads the replacement context without a write replay", async () => {
  const f = await fixture();
  const pending = deferred();
  f.request.mockImplementation(async (path, options) =>
    options?.method
      ? (pending.promise as never)
      : path.endsWith("/schema")
        ? (schema as never)
        : ({ items: initial } as never),
  );
  await f.w.findAll('input[role="switch"]')[0]!.setValue(true);
  await flushPromises();
  f.client.csrfToken = "raw-replacement-pay-type";
  pending.resolve({});
  await flushPromises();
  expect(f.w.text()).not.toContain("已开启");
  expect(button(f.w, "刷新").attributes("disabled")).toBeUndefined();
  expect(f.request.mock.calls.filter(([, o]) => o?.method)).toHaveLength(1);
});
