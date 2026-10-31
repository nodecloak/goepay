// @vitest-environment jsdom
import { it, expect } from "vitest";
import { reactive } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient } from "../src/api";

it("clears retained settings drafts when the same client's session proof changes", async () => {
  let reads = 0;
  const client = reactive(new ApiClient("admin"));
  client.csrfToken = "proof-before";
  client.request = async (path: string) => {
    if (path.endsWith("/schema")) {
      reads++;
      return {
        fields: [{ name: "key", type: "select", options: ["site_name"] }],
        permissions: { create: true, update: true },
      } as any;
    }
    return {
      items: [{ id: "1", key: "site_name", value: "已核实网站" }],
      total: 1,
    } as any;
  };
  const wrapper = mount(SettingsPage, {
    props: { client, ownSession: true, adminClass: "super" },
    global: {
      stubs: { SiteBrandSettings193: true, CaptchaKeySettings203: true },
    },
  });
  try {
    await flushPromises();
    await wrapper.get('input[aria-label="网站名称"]').setValue("旧会话草稿");
    expect(wrapper.text()).toContain("本页有1项未保存");
    client.csrfToken = "proof-after";
    await flushPromises();
    expect(reads).toBe(2);
    expect(wrapper.get('input[aria-label="网站名称"]').element).toHaveProperty(
      "value",
      "已核实网站",
    );
    expect(wrapper.text()).toContain("本页没有未保存修改");
  } finally {
    wrapper.unmount();
  }
});
