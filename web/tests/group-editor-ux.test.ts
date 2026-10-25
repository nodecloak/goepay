// @vitest-environment jsdom
import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import { resourceListFields } from "../src/pro-console-fields";
import GroupEditorFields from "../src/components/GroupEditorFields.vue";
import type { ResourceField } from "../src/resources";
const fields: ResourceField[] = [
  { name: "name", label: "名称", type: "string", required: true },
  { name: "rate_bps", label: "统一支付费率（万分比）", type: "integer" },
  {
    name: "settle_open",
    label: "结算开关",
    type: "select",
    options: [
      { value: "0", label: "继承" },
      { value: "1", label: "自动" },
      { value: "2", label: "手动" },
      { value: "3", label: "自动与手动" },
    ],
  },
  { name: "price_cents", label: "会员价格（分）", type: "integer" },
  {
    name: "user_transfer",
    label: "代付功能",
    type: "select",
    options: ["0", "1"],
  },
];
it("original per-payment retained rates use exact strings and preserve the other payment type", async () => {
  const w = mount(GroupEditorFields, {
    props: {
      fields: [
        ...fields,
        { name: "info", label: "支付方式分成比例", type: "json" },
      ],
      values: {
        name: "本地组",
        info: '{"alipay":{"rate":"99.50"},"wxpay":{"rate":"98.75"}}',
      },
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  await w.get('[role="tab"][data-group="payment"]').trigger("click");
  expect(w.text()).toContain("分成比例");
  const input = w.get('input[aria-label="支付宝分成比例（%）"]');
  expect((input.element as HTMLInputElement).value).toBe("99.50");
  await input.setValue("100");
  const event = w.emitted("change")!.at(-1)!;
  expect(event[0]).toBe("info");
  expect(JSON.parse(String(event[1]))).toEqual({
    alipay: { rate: "100" },
    wxpay: { rate: "98.75" },
  });
  expect(w.find("textarea").exists()).toBe(false);
  w.unmount();
});
it("group lists retain only the original group name while other schemas retain their columns", () => {
  expect(
    resourceListFields("groups", fields).map((f) => [f.name, f.label]),
  ).toEqual([["name", "用户组名称"]]);
  expect(resourceListFields("channels", fields).map((f) => f.name)).toEqual(
    fields.map((f) => f.name),
  );
});
it("group editor exposes original transfer permission, preserving legacy zero as inheritance", async () => {
  const w = mount(GroupEditorFields, {
    props: {
      fields,
      values: {
        name: "本地组",
        rate_bps: "38",
        settle_open: "3",
        price_cents: "100",
        user_transfer: "0",
      },
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  expect(w.get("input").element.value).toBe("本地组");
  await w.get('[role="tab"][data-group="settlement"]').trigger("click");
  expect(w.get("select").element.value).toBe("3");
  await w.get("select").setValue("2");
  expect(w.emitted("change")).toEqual([["settle_open", "2"]]);
  await w.get('[role="tab"][data-group="transfer"]').trigger("click");
  expect(w.text()).toContain("代付功能");
  expect(w.text()).toContain("商户自身权限");
  expect(w.get("select").element.value).toBe("0");
  expect(w.get("select").text()).toContain("缺省");
  await w.get("select").setValue("1");
  expect(w.emitted("change")?.at(-1)).toEqual(["user_transfer", "1"]);
  w.unmount();
});
it("hidden validation errors reveal their real group and keyboard navigation preserves the field values", async () => {
  const w = mount(GroupEditorFields, {
    attachTo: document.body,
    props: {
      fields,
      values: { name: "本地组", rate_bps: "38" },
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  await w.setProps({ errors: { rate_bps: "无效费率" } });
  expect(
    w.get('[role="tab"][aria-selected="true"]').attributes("data-group"),
  ).toBe("payment");
  expect(w.get("input").element.value).toBe("38");
  await w.get('[role="tablist"]').trigger("keydown", { key: "Home" });
  expect(
    w.get('[role="tab"][aria-selected="true"]').attributes("data-group"),
  ).toBe("basic");
  expect(w.get("input").element.value).toBe("本地组");
  w.unmount();
});
