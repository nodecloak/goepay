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

it("admin printing page uses kit switch + kit selects (打印服务 / 凭据操作)", async () => {
  const wrapper = mount(PrintingSettingsPage, {
    props: { client: adminClient(), ownSession: true, adminClass: "super" },
  });
  await flushPromises();
  expect(wrapper.find(".pa-switch").exists()).toBe(true);
  // provider + credentialAction are kit selects
  expect(wrapper.findAll(".pa-select-trigger").length).toBeGreaterThanOrEqual(
    2,
  );
  expect(wrapper.text()).toContain("允许参与订单打印");
  expect(wrapper.text()).toContain("打印服务");
  expect(wrapper.text()).toContain("凭据操作");
});
