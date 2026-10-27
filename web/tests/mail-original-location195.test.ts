import { it, expect } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient } from "../src/api";

it("original administrator email belongs to message notifications and the old alias never adds a second field", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "mail195-original-location";
  c.request = (async (path: string) => {
    if (path === "/resources/settings/schema")
      return {
        fields: [{ name: "key", options: ["mail_recv", "mail_name"] }],
        permissions: { create: true, update: true },
      };
    if (path.startsWith("/resources/settings?"))
      return {
        items: [
          { id: "1", key: "mail_recv", value: "admin195@example.test" },
          { id: "2", key: "mail_name", value: "legacy195@example.test" },
        ],
        total: 2,
      };
    throw Error("independent account unavailable");
  }) as ApiClient["request"];
  const w = mount(SettingsPage, {
    props: { client: c, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  await w.get("#settings-tab-mail").trigger("click");
  await flushPromises();
  expect(w.find("#setting-mail_recv").exists()).toBe(false);
  expect(w.find("#setting-mail_name").exists()).toBe(false);
  await w.get("#settings-tab-notice").trigger("click");
  await flushPromises();
  expect(w.get("#setting-mail_recv").attributes("aria-label")).toBe(
    "管理员邮箱",
  );
  expect(w.find("#setting-mail_name").exists()).toBe(false);
  w.unmount();
});
