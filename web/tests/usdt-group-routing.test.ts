import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import GroupEditorFields from "../src/components/GroupEditorFields.vue";
import {
  CATALOG_PAY_TYPES,
  FILTER_PAY_TYPES,
  payTypeLabel,
} from "../src/pay-types";
import { readGroupPaymentRates } from "../src/group-payment-rates";
import {
  readGroupRoutingCatalog,
  setGroupPaymentRoute,
} from "../src/group-routing";
it("includes USDT in payment methods and accepts explicit group channel and retained rate", () => {
  expect(CATALOG_PAY_TYPES).toContain("usdt");
  expect(payTypeLabel("usdt")).toBe("USDT");
  expect(FILTER_PAY_TYPES).not.toContain("usdt");
  expect(
    readGroupRoutingCatalog({
      available: true,
      items: [{ id: "99", type: "usdt", methods: ["native"] }],
    }),
  ).toHaveLength(1);
  expect(
    readGroupPaymentRates({
      usdt: { rate: "99.25", channel: "99", type: "channel" },
    }),
  ).toMatchObject({ usdt: { rate: "99.25" } });
});
it("rejects USDT group routing modes the chain admission does not support", () => {
  for (const channel of ["-1", "-2", "-3", "-4"])
    expect(() =>
      readGroupPaymentRates({
        usdt: { channel, type: channel === "-3" ? "roll" : "channel" },
      }),
    ).toThrow();
  expect(() =>
    readGroupPaymentRates({ usdt: { channel: "12", type: "roll" } }),
  ).toThrow();
  expect(
    readGroupPaymentRates({ usdt: { channel: "-5", type: "channel" } }).usdt
      ?.channel,
  ).toBe("-5");
});
it("assigns USDT through the existing group editor without auto-selecting a new channel", async () => {
  const client = new ApiClient("admin");
  const calls = vi
    .spyOn(client, "request")
    .mockImplementation(async (path) =>
      path === "/resources/groups/routing-options"
        ? {
            available: true,
            items: [{ id: "99", type: "usdt", methods: ["native"] }],
          }
        : { items: [], total: 0 },
    );
  const info = { alipay: { rate: "99.5", channel: "7", type: "channel" } };
  const w = mount(GroupEditorFields, {
    props: {
      client,
      fields: [{ name: "info", label: "通道费率", type: "json" }],
      values: { info },
      errors: {},
      editing: true,
      disabled: false,
    },
  });
  await flushPromises();
  const select = w.get<HTMLSelectElement>('select[aria-label="USDT通道"]');
  expect(select.element.value).toBe("default");
  expect(
    select
      .findAll("option")
      .map((option) => [option.attributes("value"), option.text()]),
  ).toEqual([
    ["disabled", "关闭"],
    ["first", "首个可用通道"],
    ["default", "继承默认用户组"],
    ["channel:99", "USDT #99"],
  ]);
  expect(w.text()).toContain("默认用户组未分配时不可收款");
  expect(w.emitted("change")).toBeUndefined();
  await select.setValue("channel:99");
  expect(JSON.parse(String(w.emitted("change")?.at(-1)?.[1]))).toEqual({
    ...info,
    usdt: { channel: "99", type: "channel" },
  });
  await select.setValue("disabled");
  expect(JSON.parse(String(w.emitted("change")?.at(-1)?.[1]))).toEqual({
    ...info,
    usdt: { channel: "0", type: "" },
  });
  expect(calls.mock.calls.every(([, options]) => !options?.method)).toBe(true);
  w.unmount();
  vi.restoreAllMocks();
});
