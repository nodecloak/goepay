import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient, ApiError } from "../src/api";
import { settingDefinition, settingStorage } from "../src/settings";
import actualPlatform from "../e2e/globalaccess179-http-fixture.json";
it("only the three real layout positions allow empty and longer HTML values", () => {
  for (const key of ["modal", "zhuce", "footer"]) {
    const d = settingDefinition(key, key);
    expect(d.kind).toBe("html");
    expect(settingStorage(d, "")).toBe("");
    expect(settingStorage(d, "a".repeat(1001))).toHaveLength(1001);
    expect(() => settingStorage(d, "中".repeat(7000))).toThrow(/20000/);
  }
  expect(() =>
    settingStorage(settingDefinition("site_name", "网站名称"), ""),
  ).toThrow();
  expect(() =>
    settingStorage(
      { ...settingDefinition("site_name", "网站名称"), kind: "html" },
      "",
    ),
  ).toThrow();
});
const schema = {
  title: "设置",
  fields: [{ name: "key", type: "select", options: ["site_name"] }],
  permissions: { create: true, update: true, delete: false },
};
function api(request: ReturnType<typeof vi.fn>) {
  const c = new ApiClient("admin");
  c.csrfToken = "settings173-session";
  c.request = ((path: string, options: unknown) => {
    if (
      ["/settings/branding", "/settings/captcha"].includes(path) &&
      ((options as { method?: string } | undefined)?.method ?? "GET") === "GET"
    )
      return Promise.reject(
        new ApiError(
          "synthetic independent image setting unavailable",
          503,
          -1,
        ),
      );
    return path === "/settings/merchant-access"
      ? Promise.resolve(actualPlatform.admin_default)
      : (request as (...args: unknown[]) => unknown)(path, options);
  }) as ApiClient["request"];
  return c;
}
it("does not write a settings draft after the same client CSRF is replaced", async () => {
  const request = vi
      .fn()
      .mockResolvedValueOnce(schema)
      .mockResolvedValueOnce({
        items: [{ id: "11", key: "site_name", value: "旧站点" }],
        total: 1,
      })
      .mockResolvedValue({ id: "11", key: "site_name", value: "旧站点" }),
    c = api(request);
  const w = mount(SettingsPage, {
    props: { client: c, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  expect(w.text()).toContain("品牌图片设置无法确认，请重新读取");
  await w.get('input[aria-label="网站名称"]').setValue("新站点");
  c.csrfToken = "other-session";
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  expect(w.text()).not.toContain("已保存");
  expect(w.find('input[aria-label="网站名称"]').exists()).toBe(false);
  w.unmount();
});
it("a late list response cannot restore settings from the previous client", async () => {
  let resolve!: (v: unknown) => void;
  const pending = new Promise((r) => (resolve = r)),
    first = api(
      vi.fn().mockResolvedValueOnce(schema).mockReturnValueOnce(pending),
    ),
    second = api(
      vi
        .fn()
        .mockResolvedValueOnce(schema)
        .mockResolvedValueOnce({
          items: [{ id: "12", key: "site_name", value: "当前站点" }],
          total: 1,
        }),
    );
  const w = mount(SettingsPage, {
    props: { client: first, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  await w.setProps({ client: second });
  await flushPromises();
  resolve({
    items: [{ id: "11", key: "site_name", value: "过期站点" }],
    total: 1,
  });
  await flushPromises();
  expect(w.get('input[aria-label="网站名称"]').element).toHaveProperty(
    "value",
    "当前站点",
  );
  w.unmount();
});
it("a CSRF replacement while the pre-save reread waits cannot send the old PUT", async () => {
  let resolve!: (v: unknown) => void;
  const pending = new Promise((r) => (resolve = r)),
    request = vi
      .fn()
      .mockResolvedValueOnce(schema)
      .mockResolvedValueOnce({
        items: [{ id: "11", key: "site_name", value: "旧站点" }],
        total: 1,
      })
      .mockReturnValueOnce(pending)
      .mockResolvedValue({ id: "11", key: "site_name", value: "新站点" }),
    c = api(request);
  const w = mount(SettingsPage, {
    props: { client: c, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  await w.get('input[aria-label="网站名称"]').setValue("新站点");
  await w.get("form").trigger("submit");
  c.csrfToken = "replacement";
  resolve({ id: "11", key: "site_name", value: "旧站点" });
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(3);
  expect(w.text()).not.toContain("已保存");
  w.unmount();
});
