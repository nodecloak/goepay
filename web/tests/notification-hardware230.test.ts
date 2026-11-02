import { mount, flushPromises } from "@vue/test-utils";
import { describe, it, expect, vi } from "vitest";
import Hardware from "../src/components/NotificationHardwareSettings230.vue";
import KitSelect from "../src/components/KitSelect.vue";
import KitSwitch from "../src/components/KitSwitch.vue";
import KitModal from "../src/components/KitModal.vue";
const state = () => ({
  revision: "0",
  enabled: false,
  app_id_masked: "",
  secret_present: false,
  provider: "excelsecu",
});
function setup(kind: "voice" | "printing" = "voice") {
  const request = vi.fn().mockResolvedValue(state());
  const client = { area: "admin", csrfToken: "synthetic-csrf230", request };
  const wrapper = mount(Hardware, {
    props: {
      client: client as never,
      kind,
      ownSession: true,
      adminClass: "super",
    },
    global: { stubs: { teleport: true } },
  });
  return { wrapper, request, client };
}
describe("notification hardware configuration", () => {
  it("shows voice agent_id and agent_secret with credential state beside the switch", async () => {
    const { wrapper } = setup();
    await flushPromises();
    expect(wrapper.text()).toContain("云喇叭agent_id");
    expect(wrapper.text()).toContain("云喇叭agent_secret");
    expect(wrapper.find('input[type="password"]').exists()).toBe(true);
    expect(wrapper.text()).toContain("未配置");
  });
  it("does not submit enable without the complete agent pair", async () => {
    const { wrapper, request } = setup();
    await flushPromises();
    wrapper.findComponent(KitSwitch).vm.$emit("update:modelValue", true);
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(wrapper.text()).toContain("请先填写");
    expect(request).toHaveBeenCalledTimes(1);
  });
  it("submits one atomic voice switch and credential change without retrying an unknown result", async () => {
    const { wrapper, request } = setup();
    await flushPromises();
    wrapper.findComponent(KitSelect).vm.$emit("update:modelValue", "replace");
    await flushPromises();
    await wrapper.find('input[type="text"]').setValue("local-agent230");
    await wrapper
      .find('input[type="password"]')
      .setValue("synthetic-secret230");
    wrapper.findComponent(KitSwitch).vm.$emit("update:modelValue", true);
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    request.mockRejectedValueOnce(new Error("network lost"));
    await wrapper.findComponent(KitModal).find("form").trigger("submit");
    await flushPromises();
    expect(request).toHaveBeenLastCalledWith(
      "/settings/voice",
      expect.objectContaining({
        method: "PUT",
        body: expect.objectContaining({
          enabled: true,
          app_id: "local-agent230",
          secret: "synthetic-secret230",
        }),
      }),
    );
    expect(request).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("重新读取");
    expect(
      (wrapper.find('input[type="password"]').element as HTMLInputElement)
        .value,
    ).toBe("");
  });
  it("renders receipt APPID and APPSECRET and uses the existing printing API", async () => {
    const { wrapper, request } = setup("printing");
    await flushPromises();
    expect(wrapper.text()).toContain("APPID");
    expect(wrapper.text()).toContain("APPSECRET");
    expect(request).toHaveBeenCalledWith(
      "/printing/settings",
      expect.anything(),
    );
    expect(wrapper.text()).toContain("打印机序列号");
  });
  it("discards credentials and rejects late replies when the session changes", async () => {
    const { wrapper, request } = setup();
    await flushPromises();
    wrapper.findComponent(KitSelect).vm.$emit("update:modelValue", "replace");
    await flushPromises();
    await wrapper.find('input[type="password"]').setValue("private230");
    await wrapper.setProps({ ownSession: false });
    await flushPromises();
    expect(wrapper.find('input[type="password"]').exists()).toBe(false);
    expect(request).toHaveBeenCalledTimes(1);
  });
});
