// @vitest-environment jsdom
import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import GroupEditorFields from "../src/components/GroupEditorFields.vue";

it("keeps settlement rails and transfer fee/redpack beside their basic configuration", () => {
  const keys = [
    "settle_alipay",
    "settle_wxpay",
    "settle_bank",
    "user_transfer_red",
    "transfer_rate",
  ];
  const w = mount(GroupEditorFields, {
    props: {
      fields: keys.map((name) => ({
        name,
        type: "string" as const,
        label: name,
      })),
      values: Object.fromEntries(keys.map((key) => [key, "0"])),
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  for (const key of keys) {
    const input = w.get("#field-" + key);
    expect(input.element.closest(".group-editor-section")?.className).toContain(
      key.startsWith("settle_") ? "settlement" : "transfer",
    );
  }
  expect(w.get('[role="tab"][aria-selected="true"]').text()).toBe("基本信息");
  w.unmount();
});

it("uses the original settlement field names with explicit fee units", () => {
  const w = mount(GroupEditorFields, {
    props: {
      fields: [
        { name: "settle_open", type: "string", label: "结算开关 settle_open" },
        {
          name: "settle_rate",
          type: "string",
          label: "结算手续费率 settle_rate（%，空继承，0免手续费）",
        },
      ],
      values: { settle_open: "1", settle_rate: "0.30" },
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  expect(w.get('label[for="field-settle_open"]').text()).toBe("结算开关");
  expect(w.get('label[for="field-settle_rate"]').text()).toBe(
    "结算手续费（%）",
  );
  expect(w.get<HTMLInputElement>("#field-settle_rate").element.value).toBe(
    "0.30",
  );
  w.unmount();
});
