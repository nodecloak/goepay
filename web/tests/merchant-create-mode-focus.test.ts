import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import Fields from "../src/components/MerchantCreateFields.vue";
import { ApiClient } from "../src/api";
import type { ResourceField } from "../src/resources";
const schema: ResourceField[] = [
  { name: "id", label: "商户ID", type: "string", required: true },
  { name: "name", label: "名称", type: "string", required: true },
  { name: "phone", label: "手机号", type: "string" },
  { name: "email", label: "邮箱", type: "email", max_length: 254 },
  { name: "login_password", label: "登录密码", type: "password" },
];
const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
});
async function fixture() {
  const values = reactive<Record<string, unknown>>({
    creation_mode: "account",
    phone: "13000012288",
    email: "bad email",
    login_password: "synthetic-mode-password",
  });
  const w = mount(Fields, {
    props: {
      client: new ApiClient("admin"),
      fields: schema,
      values,
      errors: {},
      disabled: false,
      onChange: (name, value) => {
        values[name] = value;
      },
    },
    attachTo: document.body,
  });
  mounted.push(w);
  await flushPromises();
  return { w, values };
}
it("keeps editable contact fields in API-only mode with schema email length and contact labels", async () => {
  const { w, values } = await fixture();
  expect(w.get("#field-email").attributes("maxlength")).toBe("100");
  await w.get('details input[type="checkbox"]').setValue(true);
  await flushPromises();
  expect(values.creation_mode).toBe("api");
  expect(w.get("#field-phone").element).toHaveProperty("value", "13000012288");
  expect(w.get("#field-email").attributes("maxlength")).toBe("254");
  expect(w.text()).toContain("联系手机");
  expect(w.text()).toContain("联系邮箱");
  await w.get("#field-email").setValue("api-merchant@example.invalid");
  expect(values.email).toBe("api-merchant@example.invalid");
  expect(values.login_password).toBe("");
  await w.get('details input[type="checkbox"]').setValue(false);
  await flushPromises();
  expect(w.get("#field-email").attributes("maxlength")).toBe("100");
  expect(w.text()).toContain("邮箱(登录账号)");
  expect(values.email).toBe("api-merchant@example.invalid");
});
it("focuses the first initial field", async () => {
  const { w } = await fixture();
  expect(document.activeElement).toBe(w.get("#field-phone").element);
});
it("focuses the first visible invalid field on validation", async () => {
  const { w } = await fixture();
  (w.get("#field-login_password").element as HTMLInputElement).focus();
  await w.setProps({
    errors: { phone: "请填写手机号或邮箱", login_password: "请填写登录密码" },
  });
  await flushPromises();
  expect(document.activeElement).toBe(w.get("#field-phone").element);
});
