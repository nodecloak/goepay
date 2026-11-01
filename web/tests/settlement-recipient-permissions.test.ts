// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import Panel from "../src/components/SettlementRecipientPanel.vue";
import Creator from "../src/components/MerchantCreateFields.vue";

const view = (id = "42") => ({
  state: "configured",
  config_id: id,
  type: "alipay",
  app_id: "",
  account_masked: "***1234",
  name_masked: "***",
});
const wrappers: VueWrapper[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
});
const button = (w: VueWrapper, text: string) =>
  w.findAll("button").find((b) => b.text() === text)!;
async function fixture(base: unknown, types: unknown) {
  const client = reactive(new ApiClient("admin"));
  client.csrfToken = "permission-session";
  let basePermission = base,
    typePermission = types,
    id = 42;
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, init) => {
      if (init?.method === "POST") return view(String(++id)); // Original POST DTO has no can_write.
      return path.includes("allowed-types")
        ? { settlement: ["alipay"], can_write: typePermission }
        : { ...view(String(id)), can_write: basePermission };
    });
  const w = mount(Panel, {
    props: { client, admin: true, merchantID: "101", disabled: false },
  });
  wrappers.push(w);
  await flushPromises();
  return {
    w,
    client,
    request,
    revoke: () => {
      basePermission = false;
      typePermission = false;
    },
  };
}
async function edit(w: VueWrapper) {
  await button(w, "修改收款资料").trigger("click");
  await w
    .get('input[aria-label="收款账号"]')
    .setValue("synthetic@example.invalid");
  await w.get('input[aria-label="真实姓名"]').setValue("Synthetic Name");
}
it.each([
  [false, false],
  [true, false],
  [false, true],
  [undefined, true],
  [true, undefined],
  ["true", true],
  [true, 1],
  [null, true],
  [true, {}],
])(
  "keeps masked read/current version but rejects edits without both strict grants (%j, %j)",
  async (base, types) => {
    const { w, request } = await fixture(base, types);
    expect(w.text()).toContain("***1234");
    expect(w.emitted("change")?.at(-1)?.[0]).toEqual(view());
    expect(button(w, "刷新收款版本").attributes("disabled")).toBeUndefined();
    expect(button(w, "修改收款资料").attributes("disabled")).toBeDefined();
    await button(w, "修改收款资料").trigger("click");
    expect(w.find('input[aria-label="收款账号"]').exists()).toBe(false);
    expect(
      request.mock.calls.filter(([, init]) => init?.method === "POST"),
    ).toHaveLength(0);
  },
);
it("preserves verified GET grants over two pure-view POSTs, then revokes on refresh", async () => {
  const { w, request, revoke } = await fixture(true, true);
  for (const id of ["43", "44"]) {
    await edit(w);
    await button(w, "保存收款版本").trigger("click");
    await flushPromises();
    expect(w.text()).toContain("版本 " + id);
    expect(button(w, "修改收款资料").attributes("disabled")).toBeUndefined();
  }
  const posts = request.mock.calls.filter(
    ([, init]) => init?.method === "POST",
  );
  expect(
    posts.map(
      ([, init]) => (init?.body as Record<string, unknown>).expected_config_id,
    ),
  ).toEqual(["42", "43"]);
  revoke();
  await button(w, "刷新收款版本").trigger("click");
  await flushPromises();
  expect(w.emitted("change")?.at(-1)?.[0]).toEqual(view("44"));
  expect(button(w, "修改收款资料").attributes("disabled")).toBeDefined();
  expect(posts).toHaveLength(2);
});
it("does not restore old writable state after a CSRF context replacement", async () => {
  const { w, client, request } = await fixture(true, true);
  await edit(w);
  let resolve!: (value: unknown) => void;
  request.mockImplementation(async (path) =>
    path.includes("allowed-types")
      ? { settlement: ["alipay"], can_write: false }
      : new Promise((r) => {
          resolve = r;
        }),
  );
  client.csrfToken = "pending";
  await flushPromises();
  const oldResolve = resolve;
  client.csrfToken = "current";
  await flushPromises();
  resolve({ ...view("52"), can_write: false });
  await flushPromises();
  oldResolve({ ...view("43"), can_write: true });
  await flushPromises();
  expect(w.text()).toContain("版本 52");
  expect(w.text()).not.toContain("版本 43");
  expect(w.find('input[aria-label="收款账号"]').exists()).toBe(false);
  expect(button(w, "修改收款资料").attributes("disabled")).toBeDefined();
});

it.each([false, undefined, "true", null])(
  "blocks a preexisting creator recipient draft for unverified grant %j until explicit clear",
  async (permission) => {
    const client = new ApiClient("admin");
    client.csrfToken = "create-permission";
    vi.spyOn(client, "request").mockResolvedValue({
      settlement: ["alipay"],
      can_write: permission,
    });
    const values = reactive<Record<string, unknown>>({
      creation_mode: "account",
      settlement_type: "alipay",
      settlement_account: "synthetic@example.invalid",
      settlement_name: "Synthetic",
      phone: "13800138000",
    });
    const w = mount(Creator, {
      props: {
        client,
        values,
        errors: {},
        disabled: false,
        fields: [
          { name: "phone", label: "Phone", type: "string" },
          {
            name: "settlement_type",
            label: "结算方式",
            type: "select",
            options: ["alipay"],
          },
          { name: "settlement_account", label: "结算账号", type: "string" },
          { name: "settlement_name", label: "姓名", type: "string" },
        ],
        onChange: (name, value) => (values[name] = value),
      },
    });
    wrappers.push(w);
    await flushPromises();
    expect(w.emitted("settlement-validity")?.at(-1)?.[0]).toContain("权限");
    expect(
      w
        .get<HTMLInputElement>("#field-settlement_account")
        .element.matches(":disabled"),
    ).toBe(true);
    await button(w, "清空初始结算资料").trigger("click");
    await flushPromises();
    expect(values.phone).toBe("13800138000");
    expect(values.settlement_account).toBe("");
    expect(w.emitted("settlement-validity")?.at(-1)?.[0]).toBe("");
  },
);
it("a late writable creator preview cannot restore permission after a newer read-only context", async () => {
  const client = reactive(new ApiClient("admin"));
  client.csrfToken = "old-create";
  let resolveOld!: (value: unknown) => void;
  vi.spyOn(client, "request").mockImplementation(async () =>
    client.csrfToken === "old-create"
      ? new Promise((r) => {
          resolveOld = r;
        })
      : { settlement: ["alipay"], can_write: false },
  );
  const values = reactive<Record<string, unknown>>({
    creation_mode: "account",
    settlement_type: "alipay",
    settlement_account: "retained-private-draft",
    settlement_name: "Synthetic",
    phone: "13800138000",
  });
  const w = mount(Creator, {
    props: {
      client,
      fields: [
        {
          name: "settlement_type",
          label: "方式",
          type: "select",
          options: ["alipay"],
        },
        { name: "settlement_account", label: "账号", type: "string" },
        { name: "settlement_name", label: "姓名", type: "string" },
      ],
      values,
      disabled: false,
      errors: {},
      onChange: (name, value) => (values[name] = value),
    },
  });
  wrappers.push(w);
  await flushPromises();
  client.csrfToken = "current-create";
  await flushPromises();
  resolveOld({ settlement: ["alipay"], can_write: true });
  await flushPromises();
  expect(w.emitted("settlement-validity")?.at(-1)?.[0]).toContain("权限");
  expect(
    w
      .get<HTMLInputElement>("#field-settlement_account")
      .element.matches(":disabled"),
  ).toBe(true);
  expect(values.settlement_account).toBe("retained-private-draft");
  await button(w, "清空初始结算资料").trigger("click");
  await flushPromises();
  expect(w.emitted("settlement-validity")?.at(-1)?.[0]).toBe("");
  expect(values.phone).toBe("13800138000");
});
