import { mount } from "@vue/test-utils";
import { expect, it } from "vitest";
import Control from "../src/components/CredentialActionControl.vue";

it("shows saved status without emitting a credential mutation on mount", () => {
  const view = mount(Control, {
    props: { modelValue: "keep", configured: true, label: "应用密钥" },
  });
  expect(view.text()).toContain("已配置");
  expect(view.find("select").exists()).toBe(false);
  expect(view.find("input").exists()).toBe(false);
  expect(view.emitted()).toEqual({});
  expect(view.get('button[aria-label="修改应用密钥"]').attributes("type")).toBe(
    "button",
  );
});

it("starts editing explicitly and lets the user cancel before the form is saved", async () => {
  const view = mount(Control, {
    props: { modelValue: "keep", configured: true, label: "应用密钥" },
  });
  await view.get('button[aria-label="修改应用密钥"]').trigger("click");
  expect(view.emitted("update:modelValue")).toEqual([["replace"]]);
  await view.setProps({ modelValue: "replace" });
  expect(view.text()).toContain("已配置");
  await view.get('button[aria-label="取消修改应用密钥"]').trigger("click");
  expect(view.emitted("update:modelValue")).toEqual([["replace"], ["keep"]]);
});

it("stages clearing separately from stored status and supports undo", async () => {
  const view = mount(Control, {
    props: { modelValue: "keep", configured: true, label: "打印设备" },
  });
  await view.get('button[aria-label="清除打印设备"]').trigger("click");
  expect(view.emitted("update:modelValue")).toEqual([["clear"]]);
  await view.setProps({ modelValue: "clear" });
  expect(view.get('[role="status"]').text()).toBe("保存后清除");
  expect(view.text()).toContain("已配置");
  await view.get('button[aria-label="撤销清除打印设备"]').trigger("click");
  expect(view.emitted("update:modelValue")).toEqual([["clear"], ["keep"]]);
});

it("offers setup for an empty credential without a misleading clear action", async () => {
  const view = mount(Control, {
    props: { modelValue: "keep", configured: false, label: "云喇叭凭据" },
  });
  expect(view.text()).toContain("未配置");
  expect(view.find('button[aria-label="清除云喇叭凭据"]').exists()).toBe(false);
  await view.get('button[aria-label="设置云喇叭凭据"]').trigger("click");
  expect(view.emitted("update:modelValue")).toEqual([["replace"]]);
});

it("blocks clearing an active integration while allowing normal credential editing", async () => {
  const view = mount(Control, {
    props: {
      modelValue: "keep",
      configured: true,
      label: "机器人凭据",
      clearDisabled: true,
      clearHint: "停用后可清除",
    },
  });
  const clear = view.get('button[aria-label="清除机器人凭据"]');
  expect(clear.attributes("disabled")).toBeDefined();
  expect(view.text()).toContain("停用后可清除");
  await clear.trigger("click");
  expect(view.emitted("update:modelValue")).toBeUndefined();
  await view.get('button[aria-label="修改机器人凭据"]').trigger("click");
  expect(view.emitted("update:modelValue")).toEqual([["replace"]]);
});

it("does not emit changes while the owning form is blocked", async () => {
  const view = mount(Control, {
    props: {
      modelValue: "keep",
      configured: true,
      label: "AppSecret",
      disabled: true,
    },
  });
  for (const button of view.findAll("button")) await button.trigger("click");
  expect(view.emitted("update:modelValue")).toBeUndefined();
});
