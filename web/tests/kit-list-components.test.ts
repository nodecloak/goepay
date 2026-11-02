// @vitest-environment jsdom
import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import { money } from "../src/pro-console-fields";
import KitStatusTag from "../src/components/KitStatusTag.vue";
import KitPayMethod from "../src/components/KitPayMethod.vue";
import KitPagination from "../src/components/KitPagination.vue";
import KitSwitch from "../src/components/KitSwitch.vue";
import KitEmpty from "../src/components/KitEmpty.vue";

it("money() groups thousands and keeps two decimals", () => {
  expect(money("123456789")).toBe("1,234,567.89");
  expect(money("1234")).toBe("12.34");
  expect(money("0")).toBe("0.00");
  expect(money("-100000")).toBe("-1,000.00");
  expect(money("1.23")).toBe("—");
});

it("KitStatusTag shows the fixed word and semantic tone", () => {
  const paid = mount(KitStatusTag, { props: { status: "paid", orders: true } });
  expect(paid.text()).toBe("已支付");
  expect(paid.get("span").classes()).toContain("pa-tag--success");
  const failed = mount(KitStatusTag, { props: { status: "failed" } });
  expect(failed.get("span").classes()).toContain("pa-tag--danger");
  const explicit = mount(KitStatusTag, {
    props: { text: "待处理", tone: "warning" },
  });
  expect(explicit.text()).toBe("待处理");
  expect(explicit.get("span").classes()).toContain("pa-tag--warning");
});

it("KitPayMethod shows the Alipay and WeChat icons with the Chinese name", () => {
  for (const [type, label] of [
    ["alipay", "支付宝"],
    ["wxpay", "微信支付"],
  ]) {
    const w = mount(KitPayMethod, { props: { type } });
    const icon = w.get("img.pa-method-icon");
    expect(icon.classes()).toContain(`pa-method-icon--${type}`);
    expect(icon.attributes("src")).toContain(`${type}.png`);
    expect(icon.attributes("aria-hidden")).toBe("true");
    expect(w.find(".pa-method-mark").exists()).toBe(false);
    expect(w.text()).toBe(label);
  }
});

it("KitPayMethod keeps the letter mark for methods without an icon", () => {
  const w = mount(KitPayMethod, { props: { type: "qqpay" } });
  expect(w.find("img").exists()).toBe(false);
  expect(w.get(".pa-method-mark").classes()).toContain("pa-method-mark--qqpay");
  expect(w.text()).toContain("Q");
});

it("KitPagination emits page changes and page-size changes", async () => {
  const w = mount(KitPagination, {
    props: { page: 1, pageSize: 20, total: 85 },
  });
  expect(w.text()).toContain("共 85 条");
  const pages = w.findAll(".pa-page");
  const next = pages.find((b) => b.attributes("aria-label") === "下一页")!;
  await next.trigger("click");
  expect(w.emitted("update:page")).toEqual([[2]]);
  expect(w.emitted("change")).toBeTruthy();
});

it("KitSwitch emits boolean change", async () => {
  const w = mount(KitSwitch, { props: { modelValue: false, label: "启用" } });
  await w.get("input").setValue(true);
  expect(w.emitted("update:modelValue")).toEqual([[true]]);
  expect(w.emitted("change")).toEqual([[true]]);
});
it("KitSwitch keeps the accepted state while confirmation is pending or cancelled", async () => {
  const w = mount(KitSwitch, { props: { modelValue: false } });
  await w.get("input").setValue(true);
  expect((w.get("input").element as HTMLInputElement).checked).toBe(false);
  await w.setProps({ modelValue: true });
  expect((w.get("input").element as HTMLInputElement).checked).toBe(true);
  await w.get("input").setValue(false);
  expect((w.get("input").element as HTMLInputElement).checked).toBe(true);
});
it("KitSwitch names the feature independently from its changing state text", async () => {
  const w = mount(KitSwitch, {
    props: { modelValue: false, label: "关闭", ariaLabel: "开放商户注册" },
  });
  expect(w.get('input[role="switch"]').attributes("aria-label")).toBe(
    "开放商户注册",
  );
  expect(w.text()).toBe("关闭");
  await w.get("input").setValue(true);
  expect(w.emitted("update:modelValue")).toEqual([[true]]);
  await w.setProps({ modelValue: true, label: "开启" });
  expect(w.get('input[role="switch"]').attributes("aria-label")).toBe(
    "开放商户注册",
  );
  expect(w.text()).toBe("开启");
});

it("KitEmpty shows the default empty text", () => {
  expect(mount(KitEmpty).text()).toContain("暂无数据");
});
