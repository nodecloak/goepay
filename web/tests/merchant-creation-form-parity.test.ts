// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import MerchantCreateFields from "../src/components/MerchantCreateFields.vue";
import { ApiClient } from "../src/api";

it("creates a merchant from reference contact/password fields without exposing internal identity controls", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-create122-session";
  const fields = [
    {
      name: "creation_mode",
      label: "新增方式",
      type: "select",
      options: [
        { value: "account", label: "商户账号" },
        { value: "api", label: "仅API" },
      ],
    },
    {
      name: "id",
      label: "商户 ID",
      type: "string",
      required: true,
      create_only: true,
    },
    { name: "name", label: "名称", type: "string", required: true },
    { name: "login_username", label: "后台登录账号（可选）", type: "string" },
    { name: "phone", label: "手机号码", type: "string" },
    { name: "email", label: "电子邮箱", type: "email" },
    {
      name: "login_password",
      label: "后台登录密码（首次设置或重置）",
      type: "password",
      write_only: true,
    },
    { name: "qq", label: "QQ", type: "string" },
    { name: "url", label: "网站域名", type: "string" },
    { name: "group_id", label: "基础用户组 ID", type: "string" },
    {
      name: "confirm_group_assignment",
      label: "明确确认基础组",
      type: "boolean",
      write_only: true,
    },
    { name: "enabled", label: "启用", type: "boolean", default: true },
    {
      name: "pay",
      label: "收款状态",
      type: "select",
      default: "1",
      options: [
        { value: "0", label: "禁止收款" },
        { value: "1", label: "正常" },
        { value: "2", label: "待审核" },
      ],
    },
  ];
  const writes: unknown[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    if (options?.method) {
      writes.push(options.body);
      return { id: "12201" } as never;
    }
    if (path.endsWith("/schema"))
      return {
        title: "商户管理",
        permissions: { create: true, update: true, delete: false },
        fields,
      } as never;
    if (path.includes("/resources/groups"))
      return { items: [{ id: "0", name: "默认用户组" }], total: 1 } as never;
    return { items: [], total: 0 } as never;
  });
  const w = mount(ResourcePage, {
    props: { client, resource: "merchants" },
    attachTo: document.body,
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "新增商户")!
    .trigger("click");
  await flushPromises();
  const dialog = w.get('[role="dialog"]');
  expect(dialog.attributes("aria-label")).toBe("添加商户");
  expect(dialog.text()).toContain("手机号(登录账号)");
  expect(dialog.text()).toContain("邮箱(登录账号)");
  expect(dialog.find("#field-id").exists()).toBe(false);
  expect(dialog.find("#field-name").exists()).toBe(false);
  expect(dialog.find("#field-confirm_group_assignment").exists()).toBe(false);
  await dialog.get("form").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(0);
  expect(dialog.text()).toContain("请填写手机号或邮箱");
  await dialog.get("#field-phone").setValue("13000012201");
  await dialog
    .get("#field-login_password")
    .setValue("synthetic-create122-password");
  const draft = w.findComponent(MerchantCreateFields).props("values");
  await dialog.get("form").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({
    creation_mode: "account",
    phone: "13000012201",
    login_password: "synthetic-create122-password",
  });
  expect(writes[0]).not.toHaveProperty("id");
  expect(writes[0]).not.toHaveProperty("name");
  expect(writes[0]).not.toHaveProperty("login_username");
  expect(draft.login_password).toBeUndefined();
  w.unmount();
});
