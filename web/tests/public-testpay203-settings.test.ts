import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient, ApiError } from "../src/api";
import { settingDefinition, settingStorage } from "../src/settings";

it("keeps the original public test payment and CAPTCHA fields together in website settings", () => {
  for (const [key, label] of [
    ["test_open", "测试支付"],
    ["test_pay_uid", "测试支付收款商户ID"],
    ["captcha_open_test", "测试支付开启验证码"],
    ["captcha_id", "极验滑动验证码ID"],
    ["captcha_version", "极验版本"],
    ["captcha_open_login", "登录开启验证码"],
  ]) {
    const definition = settingDefinition(key!);
    expect(definition.tab).toBe("site");
    expect(definition.label).toBe(label);
    if (key !== "captcha_id" && key !== "test_pay_uid") {
      expect(definition.options?.map((item) => item.value).sort()).toEqual([
        "0",
        "1",
      ]);
      expect(() => settingStorage(definition, "true")).toThrow();
    }
  }
  const version = settingDefinition("captcha_version");
  expect(version.options).toEqual([
    { value: "0", label: "V3.0" },
    { value: "1", label: "V4.0" },
  ]);
  expect(
    settingStorage(settingDefinition("test_pay_uid"), "9007199254740993"),
  ).toBe("9007199254740993");
});

it("defaults an unconfigured public entry to closed and reveals original dependent fields only when enabled", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "public-settings203";
  client.request = vi.fn(async (path: string) => {
    if (path === "/resources/settings/schema")
      return {
        title: "设置",
        fields: [
          {
            name: "key",
            type: "select",
            options: [
              "test_open",
              "test_pay_uid",
              "captcha_open_test",
              "captcha_id",
              "captcha_version",
              "captcha_open_login",
            ],
          },
        ],
        permissions: { create: true, update: true, delete: false },
      };
    if (path === "/resources/settings?page=1&limit=100")
      return { items: [], total: 0 };
    if (
      [
        "/settings/branding",
        "/settings/captcha",
        "/settings/merchant-access",
      ].includes(path)
    )
      throw new ApiError("independent setting unavailable", 503, -1);
    throw Error("unexpected settings API " + path);
  }) as ApiClient["request"];
  const wrapper = mount(SettingsPage, {
    props: { client, adminClass: "super", ownSession: true },
  });
  try {
    await flushPromises();
    expect(wrapper.get('select[aria-label="测试支付"]').element).toHaveProperty(
      "value",
      "0",
    );
    expect(wrapper.get('select[aria-label="极验版本"]').element).toHaveProperty(
      "value",
      "0",
    );
    expect(
      wrapper.get('select[aria-label="登录开启验证码"]').element,
    ).toHaveProperty("value", "0");
    expect(
      wrapper.find('input[aria-label="测试支付收款商户ID"]').exists(),
    ).toBe(false);
    expect(
      wrapper.find('select[aria-label="测试支付开启验证码"]').exists(),
    ).toBe(false);
    await wrapper.get('select[aria-label="测试支付"]').setValue("1");
    expect(
      wrapper.get('input[aria-label="测试支付收款商户ID"]').element,
    ).toHaveProperty("value", "0");
    expect(
      wrapper.get('select[aria-label="测试支付开启验证码"]').element,
    ).toHaveProperty("value", "0");
    await wrapper.get('select[aria-label="测试支付"]').setValue("0");
    expect(
      wrapper.find('input[aria-label="测试支付收款商户ID"]').exists(),
    ).toBe(false);
  } finally {
    wrapper.unmount();
  }
});
