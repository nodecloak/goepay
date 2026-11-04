import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import PrintingSettingsPage from "../src/components/PrintingSettingsPage.vue";

function adminClient() {
  return {
    area: "admin",
    csrfToken: "t",
    request: vi.fn().mockResolvedValue({
      revision: "1",
      enabled: false,
      provider: "local_fake",
      app_id_masked: "",
      secret_present: false,
      external_execution_available: false,
    }),
  } as unknown as import("../src/api").ApiClient;
}

it("admin printing page uses a service selector and ordinary credential buttons", async () => {
  const wrapper = mount(PrintingSettingsPage, {
    props: { client: adminClient(), ownSession: true, adminClass: "super" },
  });
  await flushPromises();
  expect(wrapper.find(".pa-switch").exists()).toBe(true);
  expect(wrapper.findAll(".pa-select-trigger")).toHaveLength(1);
  expect(wrapper.text()).toContain("允许参与订单打印");
  expect(wrapper.text()).toContain("打印服务");
  expect(wrapper.get('[aria-label="设置打印服务凭据"]').text()).toBe("设置");
  expect(wrapper.text()).not.toContain("保留原凭据");
});
