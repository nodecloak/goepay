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
it("new merchant validation opens the hidden required key tab before any request", async () => {
  const { w, writes } = await fixture();
  await w
    .findAll("button")
    .find((b) => b.text() === "新增商户")!
    .trigger("click");
  await flushPromises();
  await w.find("#field-id").setValue("11902");
  await w.find("#field-name").setValue("本地新增商户");
  await w.find(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(
    w
      .findAll('[role="tab"]')
      .find((b) => b.attributes("aria-selected") === "true")
      ?.text(),
  ).toBe("密钥信息");
  expect(w.find("#field-api_key").isVisible()).toBe(true);
  expect(writes).toHaveLength(0);
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
