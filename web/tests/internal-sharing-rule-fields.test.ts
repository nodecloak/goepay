import { expect, it } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import InternalSharingRuleFields from "../src/components/InternalSharingRuleFields.vue";
import type { ResourceField } from "../src/resources";

// 站内分账规则 kit dialog (a4 N3): receiver rows (收款商户ID + 分账比例%),
// add/remove up to 10, a running total that must stay ≤100%, and a canonical
// `receivers` body emitted as JSON (never raw JSON shown to the user).

const fields: ResourceField[] = [
  { name: "merchant_id", label: "付款商户ID", type: "string" } as ResourceField,
  {
    name: "name",
    label: "名称",
    type: "string",
    required: true,
  } as ResourceField,
  {
    name: "recipient_id",
    label: "收款商户ID",
    type: "string",
  } as ResourceField,
  { name: "rate_bps", label: "分账比例", type: "integer" } as ResourceField,
  { name: "receivers", label: "分账接收方列表", type: "json" } as ResourceField,
  { name: "enabled", label: "启用", type: "boolean" } as ResourceField,
];

function lastReceivers(w: ReturnType<typeof mount>): any[] | undefined {
  const calls = w.emitted("change") as Array<[string, unknown]> | undefined;
  if (!calls) return undefined;
  for (let i = calls.length - 1; i >= 0; i--) {
    if (calls[i]![0] === "receivers") {
      const v = calls[i]![1];
      return typeof v === "string" ? JSON.parse(v) : (v as any[]);
    }
  }
  return undefined;
}

it("renders one receiver row by default with 收款商户ID + 分账比例", async () => {
  const w = mount(InternalSharingRuleFields, {
    props: {
      fields,
      values: { name: "", enabled: true },
      errors: {},
      editing: false,
    },
  });
  expect(w.findAll('[aria-label="收款商户ID"]').length).toBe(1);
  expect(w.findAll('[aria-label="分账比例"]').length).toBe(1);
  // No raw JSON textarea for receivers.
  expect(w.html()).not.toContain('aria-label="分账接收方列表"');
  w.unmount();
});

it("adds and removes rows up to 10 and emits a canonical receivers body", async () => {
  const w = mount(InternalSharingRuleFields, {
    props: {
      fields,
      values: { name: "r", enabled: true },
      errors: {},
      editing: false,
    },
  });
  // Fill first row.
  await w.findAll('[aria-label="收款商户ID"]')[0]!.setValue("102");
  await w.findAll('[aria-label="分账比例"]')[0]!.setValue("30");
  // Add a second row and fill it.
  await w.get('[class*="pa-btn--success"]').trigger("click");
  await flushPromises();
  await w.findAll('[aria-label="收款商户ID"]')[1]!.setValue("103");
  await w.findAll('[aria-label="分账比例"]')[1]!.setValue("20.5");
  await flushPromises();
  const recv = lastReceivers(w)!;
  expect(recv).toEqual([
    { recipient_id: "102", rate_bps: "3000" },
    { recipient_id: "103", rate_bps: "2050" },
  ]);
  // Running total shown as percent.
  expect(w.get(".recv-total").text()).toContain("50.50%");
  w.unmount();
});

it("caps receiver rows at 10", async () => {
  const w = mount(InternalSharingRuleFields, {
    props: {
      fields,
      values: { name: "r", enabled: true },
      errors: {},
      editing: false,
    },
  });
  const addBtn = () => w.get('[class*="pa-btn--success"]');
  for (let i = 0; i < 15; i++) {
    if ((addBtn().element as HTMLButtonElement).disabled) break;
    await addBtn().trigger("click");
    await flushPromises();
  }
  expect(w.findAll('[aria-label="收款商户ID"]').length).toBe(10);
  expect((addBtn().element as HTMLButtonElement).disabled).toBe(true);
  w.unmount();
});

it("flags a running total over 100%", async () => {
  const w = mount(InternalSharingRuleFields, {
    props: {
      fields,
      values: { name: "r", enabled: true },
      errors: {},
      editing: false,
    },
  });
  await w.findAll('[aria-label="收款商户ID"]')[0]!.setValue("102");
  await w.findAll('[aria-label="分账比例"]')[0]!.setValue("60");
  await w.get('[class*="pa-btn--success"]').trigger("click");
  await flushPromises();
  await w.findAll('[aria-label="收款商户ID"]')[1]!.setValue("103");
  await w.findAll('[aria-label="分账比例"]')[1]!.setValue("50");
  await flushPromises();
  expect(w.text()).toContain("分账比例合计不能超过100%");
  w.unmount();
});

it("seeds rows from an existing multi-receiver rule in edit mode", async () => {
  const w = mount(InternalSharingRuleFields, {
    props: {
      fields,
      values: {
        id: "7",
        name: "edit",
        enabled: true,
        // json field arrives pretty-printed in edit mode.
        receivers: JSON.stringify([
          { recipient_id: "102", rate_bps: "3000" },
          { recipient_id: "103", rate_bps: "2000" },
        ]),
      },
      errors: {},
      editing: true,
    },
  });
  await flushPromises();
  const ids = w
    .findAll('[aria-label="收款商户ID"]')
    .map((i) => (i.element as HTMLInputElement).value);
  const rates = w
    .findAll('[aria-label="分账比例"]')
    .map((i) => (i.element as HTMLInputElement).value);
  expect(ids).toEqual(["102", "103"]);
  expect(rates).toEqual(["30", "20"]);
  w.unmount();
});
