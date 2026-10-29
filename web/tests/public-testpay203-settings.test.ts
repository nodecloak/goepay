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
    // test_open / captcha_open_login render as kit switches (开启/关闭 toggle);
    // locate each by its field label and read the switch checkbox state.
    const field = (label: string) =>
      wrapper
        .findAll(".setting-field")
        .find((f) => f.find("label").text() === label)!;
    const switchOf = (label: string) =>
      field(label).get('input[role="switch"]').element as HTMLInputElement;
    expect(switchOf("测试支付").checked).toBe(false);
    expect(switchOf("登录开启验证码").checked).toBe(false);
    // 极验版本 is a genuine two-way choice (V3.0/V4.0) → kit select, default V3.0.
    expect(
      wrapper.get('.pa-select-trigger[aria-label="极验版本"]').text(),
    ).toContain("V3.0");
    // Dependent fields are hidden while test payment is off.
    expect(
      wrapper.find('input[aria-label="测试支付收款商户ID"]').exists(),
    ).toBe(false);
    expect(
      wrapper
        .findAll(".setting-field")
        .some((f) => f.find("label").text() === "测试支付开启验证码"),
    ).toBe(false);
    // Turning the test-payment switch on reveals the dependent fields.
    await field("测试支付").get('input[role="switch"]').setValue(true);
    expect(
      (
        wrapper.get('input[aria-label="测试支付收款商户ID"]')
          .element as HTMLInputElement
      ).value,
    ).toBe("0");
    expect(switchOf("测试支付开启验证码").checked).toBe(false);
    // Turning it back off hides them again.
    await field("测试支付").get('input[role="switch"]').setValue(false);
    expect(
      wrapper.find('input[aria-label="测试支付收款商户ID"]').exists(),
    ).toBe(false);
  } finally {
    wrapper.unmount();
  }
});
