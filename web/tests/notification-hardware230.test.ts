import { mount, flushPromises } from "@vue/test-utils";
import { describe, it, expect, vi } from "vitest";
import Hardware from "../src/components/NotificationHardwareSettings230.vue";
import CredentialActionControl from "../src/components/CredentialActionControl.vue";
import KitSwitch from "../src/components/KitSwitch.vue";
import KitModal from "../src/components/KitModal.vue";
const state = () => ({
  revision: "0",
  enabled: false,
  app_id_masked: "",
  secret_present: false,
  provider: "excelsecu",
});
function setup(kind: "voice" | "printing" = "voice", initial = state()) {
  const request = vi.fn().mockResolvedValue(initial);
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
  it("cancels private credential drafts and stages clearing until explicit confirmation", async () => {
    const { wrapper, request } = setup("voice", {
      ...state(),
      revision: "3",
      app_id_masked: "age***230",
      secret_present: true,
    });
    await flushPromises();
    expect(wrapper.find("select").exists()).toBe(false);
    await wrapper.get('[aria-label="修改云喇叭语音播报凭据"]').trigger("click");
    await wrapper.get('[aria-label="云喇叭agent_id"]').setValue("new-agent230");
    await wrapper
      .get('[aria-label="云喇叭agent_secret"]')
      .setValue("private-new230");
    await wrapper
      .get('[aria-label="取消修改云喇叭语音播报凭据"]')
      .trigger("click");
    expect(
      wrapper.get('[aria-label="云喇叭agent_secret"]').element,
    ).toHaveProperty("value", "");
    expect(wrapper.get('[aria-label="云喇叭agent_id"]').element).toHaveProperty(
      "value",
      "",
    );
    expect(
      wrapper.findAll('button[type="submit"]')[0]!.attributes("disabled"),
    ).toBeDefined();
    await wrapper.get('[aria-label="清除云喇叭语音播报凭据"]').trigger("click");
    expect(wrapper.text()).toContain("保存后清除");
    expect(request).toHaveBeenCalledTimes(1);
    await wrapper
      .get('[aria-label="撤销清除云喇叭语音播报凭据"]')
      .trigger("click");
    expect(wrapper.text()).not.toContain("保存后清除");
    await wrapper.get('[aria-label="清除云喇叭语音播报凭据"]').trigger("click");
    await wrapper.find("form").trigger("submit");
    expect(request).toHaveBeenCalledTimes(1);
    request.mockResolvedValueOnce({ ...state(), revision: "4" });
    await wrapper.findComponent(KitModal).find("form").trigger("submit");
    await flushPromises();
    expect(request).toHaveBeenLastCalledWith(
      "/settings/voice",
      expect.objectContaining({
        method: "PUT",
        body: expect.objectContaining({
          expected_revision: "3",
          credential_action: "clear",
          app_id: "",
          secret: "",
        }),
      }),
    );
    wrapper.unmount();
  });
  it("requires disabling voice before a configured credential can be cleared", async () => {
    const { wrapper, request } = setup("voice", {
      ...state(),
      enabled: true,
      secret_present: true,
    });
    await flushPromises();
    expect(
      wrapper
        .get('[aria-label="清除云喇叭语音播报凭据"]')
        .attributes("disabled"),
    ).toBeDefined();
    expect(wrapper.text()).toContain("请先关闭开关");
    wrapper.findComponent(KitSwitch).vm.$emit("update:modelValue", false);
    await flushPromises();
    expect(
      wrapper
        .get('[aria-label="清除云喇叭语音播报凭据"]')
        .attributes("disabled"),
    ).toBeUndefined();
    expect(request).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });
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
    await wrapper
      .findComponent(CredentialActionControl)
      .get("button")
      .trigger("click");
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
    expect(wrapper.text()).toContain("刷新页面");
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
    await wrapper
      .findComponent(CredentialActionControl)
      .get("button")
      .trigger("click");
    await flushPromises();
    await wrapper.find('input[type="password"]').setValue("private230");
    await wrapper.setProps({ ownSession: false });
    await flushPromises();
    expect(wrapper.find('input[type="password"]').exists()).toBe(false);
    expect(request).toHaveBeenCalledTimes(1);
  });
});
