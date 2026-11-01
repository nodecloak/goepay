// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import GroupEditorFields from "../src/components/GroupEditorFields.vue";
import { ApiClient } from "../src/api";

it("keeps settlement minimum in the basic settlement section and preserves exact yuan input", async () => {
  const wrapper = mount(GroupEditorFields, {
    props: {
      fields: [
        { name: "name", label: "名称", type: "string" },
        {
          name: "settle_minmoney",
          label: "最低结算金额（元）",
          type: "string",
        },
      ],
      values: { name: "Synthetic group", settle_minmoney: "1.23" },
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  expect(wrapper.find("#field-settle_minmoney").exists()).toBe(true);
  expect(
    wrapper
      .get("#field-settle_minmoney")
      .element.closest("section")
      ?.getAttribute("class"),
  ).toContain("settlement");
  await wrapper.get("#field-settle_minmoney").setValue("_");
  expect(wrapper.emitted("change")?.at(-1)).toEqual(["settle_minmoney", "_"]);
  wrapper.unmount();
});

it("shows actual catalog payment types and retained legacy types while preserving hidden sandbox configuration", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    if (path === "/resources/groups/routing-options")
      return {
        available: true,
        items: [{ id: "8", type: "wxpay", methods: ["native"] }],
      } as never;
    if (path.startsWith("/resources/pay-rolls?"))
      return { items: [], total: 0 } as never;
    throw Error("unexpected route");
  });
  const wrapper = mount(GroupEditorFields, {
    props: {
      client,
      fields: [{ name: "info", label: "支付方式分成比例", type: "json" }],
      values: {
        info: JSON.stringify({
          alipay: { rate: "99.50" },
          sandbox: { rate: "100" },
        }),
      },
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  await flushPromises();
  expect(wrapper.findAll("tbody th").map((row) => row.text())).toEqual([
    "支付宝",
    "微信支付",
  ]);
  expect(
    wrapper.find('input[aria-label="本地模拟支付分成比例（%）"]').exists(),
  ).toBe(false);
  await wrapper
    .get('input[aria-label="微信支付分成比例（%）"]')
    .setValue("98.75");
  expect(JSON.parse(String(wrapper.emitted("change")?.at(-1)?.[1]))).toEqual({
    alipay: { rate: "99.50" },
    wxpay: { rate: "98.75" },
    sandbox: { rate: "100" },
  });
  wrapper.unmount();
});

it("keeps normalized settlement cycle beside minimum in the basic settlement section", async () => {
  const wrapper = mount(GroupEditorFields, {
    props: {
      fields: [
        {
          name: "settle_type",
          label: "结算周期",
          type: "select",
          options: [
            { value: "_", label: "继承" },
            { value: "0", label: "D+0" },
            { value: "1", label: "D+1" },
          ],
        },
      ],
      values: { settle_type: "0" },
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  expect(wrapper.find("#field-settle_type").exists()).toBe(true);
  expect(
    wrapper
      .get("#field-settle_type")
      .element.closest("section")
      ?.getAttribute("class"),
  ).toContain("settlement");
  await wrapper.get("#field-settle_type").setValue("1");
  expect(wrapper.emitted("change")?.at(-1)).toEqual(["settle_type", "1"]);
  wrapper.unmount();
});

it("orders settlement fields as switch, cycle, minimum and fee independently of schema registration order", () => {
  const wrapper = mount(GroupEditorFields, {
    props: {
      fields: [
        { name: "settle_minmoney", label: "最低结算金额", type: "string" },
        {
          name: "settle_type",
          label: "结算周期",
          type: "select",
          options: ["_", "0", "1"],
        },
        {
          name: "settle_open",
          label: "结算开关",
          type: "select",
          options: ["_", "1", "2", "3"],
        },
        { name: "settle_rate", label: "结算手续费", type: "string" },
      ],
      values: {},
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  expect(
    wrapper
      .findAll(".group-editor-section.settlement label")
      .map((label) => label.attributes("for")),
  ).toEqual([
    "field-settle_open",
    "field-settle_type",
    "field-settle_minmoney",
    "field-settle_rate",
  ]);
  wrapper.unmount();
});
