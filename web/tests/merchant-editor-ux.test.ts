// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ResourcePage from "../src/components/ResourcePage.vue";
import type { ResourceSchema } from "../src/resources";

const schema: ResourceSchema = {
  title: "商户管理",
  permissions: { create: true, update: true, delete: false },
  fields: [
    {
      name: "id",
      label: "商户 ID",
      type: "string",
      required: true,
      create_only: true,
    },
    { name: "name", label: "名称", type: "string", required: true },
    { name: "phone", label: "手机号码", type: "string" },
    { name: "qq", label: "QQ", type: "string" },
    { name: "url", label: "网站域名", type: "string" },
    { name: "api_key", label: "API 密钥", type: "password", write_only: true },
    {
      name: "signature_algorithm",
      label: "签名算法",
      type: "select",
      options: [
        { value: "MD5", label: "MD5" },
        { value: "RSA", label: "RSA" },
      ],
      default: "MD5",
    },
    { name: "enabled", label: "启用", type: "boolean", default: true },
  ],
};
async function fixture() {
  const client = new ApiClient("admin"),
    writes: Record<string, unknown>[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    if (options?.method) {
      writes.push(options.body as Record<string, unknown>);
      return {} as never;
    }
    return (
      path.endsWith("/schema")
        ? schema
        : path.endsWith("/11901")
          ? {
              id: "11901",
              name: "本地联系人",
              phone: "13000011901",
              qq: "1000011901",
              url: "shop.example.invalid",
              signature_algorithm: "MD5",
              enabled: true,
            }
          : {
              items: [
                {
                  id: "11901",
                  name: "本地联系人",
                  phone: "13000011901",
                  qq: "1000011901",
                  url: "shop.example.invalid",
                },
              ],
              total: 1,
            }
    ) as never;
  });
  const w = mount(ResourcePage, {
    props: { client, resource: "merchants" },
    attachTo: document.body,
  });
  await flushPromises();
  return { w, writes };
}
it("opens the original basic and key tabs without exposing existing secrets", async () => {
  const { w, writes } = await fixture();
  await w
    .findAll("button")
    .find((b) => b.text() === "编辑")!
    .trigger("click");
  await flushPromises();
  const basic = w.find('[role="tab"][aria-selected="true"]');
  expect(basic.text()).toBe("基本信息");
  expect(w.find("#field-phone").isVisible()).toBe(true);
  expect(w.find("#field-api_key").isVisible()).toBe(false);
  await w
    .findAll('[role="tab"]')
    .find((b) => b.text() === "密钥信息")!
    .trigger("click");
  expect(w.find("#field-api_key").isVisible()).toBe(true);
  expect((w.find("#field-api_key").element as HTMLInputElement).value).toBe("");
  await w
    .findAll('[role="tab"]')
    .find((b) => b.text() === "基本信息")!
    .trigger("click");
  await w.find("#field-phone").setValue("");
  await w.find("#field-qq").setValue("1000011902");
  await w.find(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({
    phone: "",
    qq: "1000011902",
    url: "shop.example.invalid",
  });
  expect(writes[0]).not.toHaveProperty("api_key");
  w.unmount();
});
it("new merchant has no key tab and is saved without a key", async () => {
  const { w, writes } = await fixture();
  await w
    .findAll("button")
    .find((b) => b.text() === "新增商户")!
    .trigger("click");
  await flushPromises();
  expect(w.find('[role="tablist"]').exists()).toBe(false);
  expect(w.find("#field-api_key").exists()).toBe(false);
  expect(w.find("#field-signature_algorithm").exists()).toBe(false);
  expect(w.text()).toContain("保存后系统自动生成 MD5 API");
  await w.find("#field-id").setValue("11902");
  await w.find("#field-name").setValue("本地新增商户");
  await w.find(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({ id: "11902", name: "本地新增商户" });
  expect(writes[0]).not.toHaveProperty("api_key");
  expect(writes[0]).not.toHaveProperty("signature_algorithm");
  w.unmount();
});

const createSetupFields: ResourceSchema["fields"] = [
  {
    name: "settlement_type",
    label: "结算方式",
    type: "select",
    options: [
      { label: "支付宝", value: "alipay" },
      { label: "微信", value: "wxpay" },
      { label: "银行卡", value: "bank" },
    ],
    default: "alipay",
    create_only: true,
    write_only: true,
  },
  ...(
    [
      ["settlement_account", "结算账号"],
      ["settlement_name", "结算账号姓名"],
      ["settlement_app_id", "收款所属 AppID"],
    ] as const
  ).map(([name, label]) => ({
    name,
    label,
    type: "string" as const,
    create_only: true,
    write_only: true,
  })),
  {
    name: "fee_mode",
    label: "手续费扣除模式",
    type: "select",
    options: [
      { label: "余额扣费", value: "0" },
      { label: "订单加费", value: "1" },
    ],
    default: "0",
    create_only: true,
    write_only: true,
  },
];
async function createSetupFixture(allowed?: string[]) {
  const client = new ApiClient("admin"),
    writes: Record<string, unknown>[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    if (options?.method) {
      writes.push(options.body as Record<string, unknown>);
      return {} as never;
    }
    if (path.endsWith("/schema"))
      return {
        ...schema,
        fields: [...schema.fields, ...createSetupFields],
      } as never;
    if (path.endsWith("/allowed-types"))
      return {
        settlement: allowed ?? ["alipay", "wxpay", "bank"],
        can_write: true,
      } as never;
    if (path.endsWith("/11901"))
      return {
        id: "11901",
        name: "本地商户",
        signature_algorithm: "MD5",
        enabled: true,
      } as never;
    return { items: [{ id: "11901", name: "本地商户" }], total: 1 } as never;
  });
  const w = mount(ResourcePage, {
    props: { client, resource: "merchants" },
    attachTo: document.body,
  });
  await flushPromises();
  return { w, writes };
}
async function openCreate(
  w: Awaited<ReturnType<typeof createSetupFixture>>["w"],
) {
  await w
    .findAll("button")
    .find((b) => b.text() === "新增商户")!
    .trigger("click");
  await flushPromises();
  await w.find("#field-id").setValue("11903");
  await w.find("#field-name").setValue("本地结算商户");
}
const groupLabels = (w: Awaited<ReturnType<typeof createSetupFixture>>["w"]) =>
  w.findAll(".merchant-editor-group h3").map((h) => h.text());
it("new merchant asks for 结算信息 and 手续费扣除模式 with the Pro defaults", async () => {
  const { w, writes } = await createSetupFixture();
  await openCreate(w);
  expect(groupLabels(w)).toEqual(["基本资料", "结算信息", "功能开关"]);
  expect(
    w.findAll('section[aria-label="功能开关"] label').map((l) => l.text())[0],
  ).toContain("手续费扣除模式");
  expect((w.find("#field-fee_mode").element as HTMLSelectElement).value).toBe(
    "0",
  );
  expect(
    (w.find("#field-settlement_type").element as HTMLSelectElement).value,
  ).toBe("alipay");
  expect(w.find("#field-settlement_app_id").exists()).toBe(false);
  await w.find("#field-settlement_account").setValue("local@example.invalid");
  await w.find(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(0);
  expect(w.text()).toContain("请填写结算账号姓名");
  await w.find("#field-settlement_name").setValue("本地收款人");
  await w.find("#field-fee_mode").setValue("1");
  await w.find(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(writes).toEqual([
    expect.objectContaining({
      id: "11903",
      fee_mode: "1",
      settlement_type: "alipay",
      settlement_account: "local@example.invalid",
      settlement_name: "本地收款人",
    }),
  ]);
  expect(writes[0]).not.toHaveProperty("settlement_app_id");
  w.unmount();
});
it("new merchant 微信 settlement needs its AppID; blank 结算信息 sends no recipient", async () => {
  const { w, writes } = await createSetupFixture();
  await openCreate(w);
  await w.find("#field-settlement_type").setValue("wxpay");
  expect(w.find("#field-settlement_app_id").exists()).toBe(true);
  await w.find("#field-settlement_account").setValue("openid-local");
  await w.find("#field-settlement_name").setValue("本地微信");
  await w.find(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(0);
  expect(w.text()).toContain("微信结算请填写收款 OpenID 所属应用的 AppID");
  await w.find("#field-settlement_account").setValue("");
  await w.find("#field-settlement_name").setValue("");
  await w.find(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({ id: "11903", fee_mode: "0" });
  for (const name of [
    "settlement_type",
    "settlement_account",
    "settlement_name",
    "settlement_app_id",
  ])
    expect(writes[0]).not.toHaveProperty(name);
  w.unmount();
});
it("new merchant lists only the settlement types the platform allows", async () => {
  const { w } = await createSetupFixture(["wxpay", "bank"]);
  await openCreate(w);
  const select = w.find("#field-settlement_type");
  expect(select.findAll("option").map((o) => o.text())).toEqual([
    "请选择",
    "微信",
    "银行卡",
  ]);
  expect((select.element as HTMLSelectElement).value).toBe("wxpay");
  w.unmount();
  const none = await createSetupFixture([]);
  await openCreate(none.w);
  expect(groupLabels(none.w)).toEqual(["基本资料", "功能开关"]);
  none.w.unmount();
});
it("editing a merchant shows no create-only setup fields", async () => {
  const { w } = await createSetupFixture();
  await w
    .findAll("button")
    .find((b) => b.text() === "编辑")!
    .trigger("click");
  await flushPromises();
  for (const name of ["fee_mode", "settlement_type", "settlement_account"])
    expect(w.find("#field-" + name).exists()).toBe(false);
  expect(groupLabels(w)).not.toContain("结算信息");
  w.unmount();
});

it("a failed second merchant read cannot leave the first merchant draft writable under another ID", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    if (path.endsWith("/schema")) return schema as never;
    if (path.endsWith("/11901"))
      return {
        id: "11901",
        name: "原商户草稿",
        signature_algorithm: "MD5",
        enabled: true,
      } as never;
    if (path.endsWith("/11902")) throw Error("second merchant unavailable");
    return {
      items: [
        { id: "11901", name: "原商户" },
        { id: "11902", name: "另一商户" },
      ],
      total: 2,
    } as never;
  });
  const w = mount(ResourcePage, {
    props: { client, resource: "merchants" },
    attachTo: document.body,
  });
  await flushPromises();
  const edits = w.findAll("button").filter((b) => b.text() === "编辑");
  await edits[0]!.trigger("click");
  await flushPromises();
  await w.find("#field-phone").setValue("13000011901");
  await edits[1]!.trigger("click");
  await flushPromises();
  expect(w.text()).toContain("second merchant unavailable");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  w.unmount();
});

it("uses original group names in a real selector and keeps an out-of-page int64 group exact", async () => {
  const client = new ApiClient("admin"),
    big = "9223372036854775807",
    writes: Record<string, unknown>[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    if (options?.method) {
      writes.push(options.body as Record<string, unknown>);
      return {} as never;
    }
    if (path.endsWith("/merchants/schema"))
      return {
        ...schema,
        fields: [
          ...schema.fields,
          {
            name: "group_id",
            label: "基础用户组 ID（管理员指定）",
            type: "string",
          },
        ],
      } as never;
    if (path.startsWith("/resources/groups?"))
      return { items: [{ id: "1", name: "普通组" }], total: 101 } as never;
    if (path === "/resources/groups/" + big)
      return { id: big, name: "大ID当前组" } as never;
    if (path.endsWith("/merchants/11901"))
      return {
        id: "11901",
        name: "本地商户",
        group_id: big,
        signature_algorithm: "MD5",
        enabled: true,
      } as never;
    return { items: [{ id: "11901", name: "本地商户" }], total: 1 } as never;
  });
  const w = mount(ResourcePage, {
    props: { client, resource: "merchants" },
    attachTo: document.body,
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "编辑")!
    .trigger("click");
  await flushPromises();
  const select = w.find("#field-group_id");
  expect(select.element.tagName).toBe("SELECT");
  expect((select.element as HTMLSelectElement).value).toBe(big);
  expect(select.text()).toContain("大ID当前组");
  await select.setValue("1");
  await w.find(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(writes[0]!.group_id).toBe("1");
  w.unmount();
});
