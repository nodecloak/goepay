// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { ApiClient } from "../src/api";
import TransferPage from "../src/components/TransferPage.vue";
import { readTransferCapabilities } from "../src/transfer-capabilities";
const common = {
  merchant_id: "1000",
  writes_enabled: false,
  platform_enabled: true,
  effective_enabled: false,
  permission_source: "group_disabled",
  effective_group_id: "3",
  merchant_enabled: true,
  channels: [{ id: "42", types: ["alipay"] }],
  minimum_money: "1.00",
  maximum_money: "200.00",
  daily_recipient_limit: 3,
  default_remark: "",
  default_title: "",
};
const readonly = {
  ...common,
  fee_policy_available: false,
  funds_policy_available: false,
  ledger_available_money: "123.45",
  reserved_money: "6.78",
};
const healthy = {
  ...common,
  writes_enabled: true,
  effective_enabled: true,
  permission_source: "group",
  transfer_rate: "3",
  available_money: "100.00",
  ledger_available_money: "123.45",
  today_blocked_money: "23.45",
  settle_type: 1,
};
const wrappers: VueWrapper[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});
async function setup(caps: unknown) {
  const client = new ApiClient("merchant");
  client.csrfToken = "caps-session";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path) => {
      if (path.startsWith("/transfers/capabilities")) return caps;
      if (path === "/transfers/7")
        return {
          transfer: {
            id: "7",
            merchant_id: "1000",
            amount: "100",
            status: "unknown",
            type: "alipay",
          },
          recipient: {},
        };
      return {
        items: [
          {
            id: "7",
            merchant_id: "1000",
            amount: "100",
            status: "unknown",
            type: "alipay",
          },
        ],
        total: 1,
      };
    });
  const post = vi.spyOn(client, "requestResult");
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:rest(.*)*", component: { render: () => null } }],
  });
  const w = mount(TransferPage, {
    props: { client, area: "merchant", selfID: "1000" },
    global: { plugins: [router] },
    attachTo: document.body,
  });
  wrappers.push(w);
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text().includes("申请付款"))!
    .trigger("click");
  await flushPromises();
  return {
    w,
    request,
    post,
    dialog: document.querySelector<HTMLElement>(
      '[role="dialog"][aria-label="转账付款"]',
    )!,
  };
}
it("preserves disabled-group cause and real ledger/reserved amounts without fabricating a quote, while history remains readable", async () => {
  const { w, dialog, request, post } = await setup(readonly);
  expect(dialog.textContent).toContain("当前用户组付款功能未启用");
  expect(dialog.textContent).toContain("账本可用余额 123.45 元");
  expect(dialog.textContent).toContain("预占余额 6.78 元");
  expect(dialog.textContent).toContain("手续费和可出账金额暂不可用");
  const amount = dialog.querySelector<HTMLInputElement>(
    'input[aria-label="转账金额"]',
  )!;
  amount.value = "10.00";
  amount.dispatchEvent(new Event("input"));
  await flushPromises();
  expect(dialog.querySelector('input[aria-label="需支付金额"]')).toBeNull();
  expect(dialog.textContent).not.toContain("可出账 0");
  expect(dialog.textContent).not.toContain("undefined");
  expect(
    dialog.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled,
  ).toBe(true);
  dialog
    .querySelector("form")!
    .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  await flushPromises();
  expect(post).not.toHaveBeenCalled();
  dialog.querySelector<HTMLButtonElement>('[aria-label="关闭"]')!.click();
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text().includes("付款详情"))!
    .trigger("click");
  await flushPromises();
  expect(request.mock.calls.some(([path]) => path === "/transfers/7")).toBe(
    true,
  );
});
it.each([
  { fee_policy_available: true },
  { fee_policy_available: "true", funds_policy_available: true },
  { fee_policy_available: true, funds_policy_available: false },
  {
    fee_policy_available: true,
    funds_policy_available: true,
    transfer_rate: undefined,
  },
  { writes_enabled: "true" },
])(
  "never falls back to a healthy legacy policy for malformed flags/rate/write capability: %j",
  async (overrides) => {
    const { dialog, post } = await setup({ ...healthy, ...overrides });
    expect(
      dialog.querySelector<HTMLButtonElement>('button[type="submit"]')!
        .disabled,
    ).toBe(true);
    expect(dialog.querySelector('input[aria-label="需支付金额"]')).toBeNull();
    expect(post).not.toHaveBeenCalled();
  },
);
it.each([true, false])(
  "retains a complete validated legacy policy with writes strictly %j",
  async (writes) => {
    const { dialog } = await setup({ ...healthy, writes_enabled: writes });
    expect(dialog.textContent).toContain("可出账 100.00 元");
    expect(
      dialog.querySelector<HTMLButtonElement>('button[type="submit"]')!
        .disabled,
    ).toBe(!writes);
  },
);
it("accepts complete explicit policy flags and a genuine zero readonly ledger without inventing derived values", () => {
  expect(
    readTransferCapabilities({
      ...healthy,
      fee_policy_available: true,
      funds_policy_available: true,
    }),
  ).toMatchObject({
    policy_available: true,
    transfer_rate: "3",
    available_money: "100.00",
  });
  const cap = readTransferCapabilities({
    ...readonly,
    ledger_available_money: "0.00",
    reserved_money: "0.00",
    transfer_rate: "0",
    available_money: "999.00",
    settle_type: 0,
  });
  expect(cap).toMatchObject({
    policy_available: false,
    writes_enabled: false,
    ledger_available_money: "0.00",
    reserved_money: "0.00",
  });
  expect(cap.transfer_rate).toBeUndefined();
  expect(cap.available_money).toBeUndefined();
  expect(cap.settle_type).toBeUndefined();
});
it.each([
  { ledger_available_money: undefined },
  { ledger_available_money: "-1.00" },
  { reserved_money: "0" },
  { reserved_money: "92233720368547758.08" },
  { funds_policy_available: undefined },
  { fee_policy_available: 0 },
  { permission_source: "group" },
  { effective_enabled: true },
  { writes_enabled: true },
])(
  "rejects incomplete or contradictory readonly capability %j",
  (overrides) => {
    expect(() =>
      readTransferCapabilities({ ...readonly, ...overrides }),
    ).toThrow();
  },
);
it.each([undefined, "", "100.01", "-1", null])(
  "rejects an absent or invalid fee even in a complete legacy DTO: %j",
  (rate) => {
    expect(() =>
      readTransferCapabilities({ ...healthy, transfer_rate: rate }),
    ).toThrow();
  },
);
