import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import AdminPermissionField from "../src/components/AdminPermissionField.vue";
import KitCheckbox from "../src/components/KitCheckbox.vue";

const catalog = [
  { key: "OrderList", title: "订单管理", group: "交易管理" },
  { key: "DailyStat", title: "每日交易统计", group: "交易管理" },
  { key: "SystemLog", title: "登录日志", group: "系统管理" },
];

it("renders the permission tree as kit checkbox groups and items", () => {
  const wrapper = mount(AdminPermissionField, {
    props: { catalog, value: [] },
  });
  // kit checkbox markup, not native bare <label><input>
  const checks = wrapper.findAll(".pa-check");
  // 2 group heads + 3 leaf items
  expect(checks.length).toBe(5);
  expect(wrapper.text()).toContain("交易管理");
  expect(wrapper.text()).toContain("系统管理");
  expect(wrapper.text()).toContain("每日交易统计");
});

it("group head is indeterminate when only some children are selected", () => {
  const wrapper = mount(AdminPermissionField, {
    props: { catalog, value: ["OrderList"] },
  });
  // First KitCheckbox is the 交易管理 group head: one of two children selected.
  const head = wrapper.findAllComponents(KitCheckbox)[0];
  expect(head.props("indeterminate")).toBe(true);
  expect(head.props("modelValue")).toBe(false);
});

it("checking a leaf emits the full granted key set", async () => {
  const wrapper = mount(AdminPermissionField, {
    props: { catalog, value: ["OrderList"] },
  });
  const leafInputs = wrapper.findAll(".admin-permission-item input");
  await leafInputs[1].setValue(true); // DailyStat
  const events = wrapper.emitted("change");
  expect(events).toBeTruthy();
  expect(events!.at(-1)![0]).toEqual(["OrderList", "DailyStat"]);
});

it("disables every checkbox when admin_class is super", () => {
  const wrapper = mount(AdminPermissionField, {
    props: { catalog, value: [], disabled: true },
  });
  const inputs = wrapper.findAll(".pa-check input");
  expect(inputs.every((i) => (i.element as HTMLInputElement).disabled)).toBe(
    true,
  );
});
