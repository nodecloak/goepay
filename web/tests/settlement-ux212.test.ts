import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettlementListTable from "../src/components/SettlementListTable.vue";
import SettlementRecipientPanel from "../src/components/SettlementRecipientPanel.vue";
import { ApiClient } from "../src/api";
import {
  settlementListFixture,
  paidSettlementListFixture,
} from "./fixtures/settlement-list";

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});
const missing = {
  state: "missing",
  config_id: "0",
  type: "",
  app_id: "",
  account_masked: "",
  name_masked: "",
};
function recipientClient(current = missing) {
  const client = new ApiClient("merchant");
  vi.spyOn(client, "request").mockImplementation(async (path, init) => {
    if (path.endsWith("/allowed-types"))
      return {
        settlement: ["alipay", "wxpay", "bank"],
        can_write: true,
      } as never;
    if (path === "/workflows/settlement-recipient")
      return {
        ...current,
        ...(!init?.method || init.method === "GET" ? { can_write: true } : {}),
      } as never;
    throw Error("Unexpected endpoint " + path);
  });
  return client;
}
it("confirmation receipt action is separate from payout inspection and requires explicit server capability", async () => {
  const waiting = settlementListFixture();
  const paid = paidSettlementListFixture();
  paid.id = "2";
  (paid.payout as any).request_id = "2";
  const wx = paidSettlementListFixture();
  wx.id = "3";
  wx.settlement_recipient = {
    ...wx.settlement_recipient,
    type: "wxpay",
    app_id: "configured-app",
  };
  wx.settlement_funding = {
    ...wx.settlement_funding,
    state: "reserved",
    reserved_amount: wx.amount_cents,
  };
  wx.payout = {
    ...wx.payout,
    request_id: "3",
    state: "pending",
    funding: { ...wx.settlement_funding },
    recipient: { ...wx.settlement_recipient },
    transfer: {
      ...(wx.payout!.transfer as object),
      status: "pending",
      type: "wxpay",
    },
  };
  const completedWx = {
    ...paid,
    id: "4",
    can_confirm_receipt: true,
    settlement_recipient: {
      ...paid.settlement_recipient,
      type: "wxpay",
      app_id: "configured-app",
    },
    payout: {
      ...paid.payout,
      request_id: "4",
      recipient: {
        ...paid.settlement_recipient,
        type: "wxpay",
        app_id: "configured-app",
      },
      transfer: { ...(paid.payout!.transfer as object), type: "wxpay" },
    },
  };
  const rows = [
    { ...waiting, can_confirm_receipt: true },
    { ...paid, can_confirm_receipt: true },
    { ...wx, can_confirm_receipt: true },
    completedWx,
  ];
  for (const area of ["admin", "merchant"] as const) {
    const w = mount(SettlementListTable, {
      props: {
        items: rows,
        area,
        checked: [],
        busy: false,
        client: new ApiClient(area),
      },
    });
    expect(
      w.findAll("button").filter((b) => b.text() === "核对结算出款"),
    ).toHaveLength(4);
    const confirm = w.findAll("button").filter((b) => b.text() === "确认收款");
    expect(confirm).toHaveLength(1);
    expect(
      confirm[0].element.closest("tr")?.getAttribute("data-settlement-id"),
    ).toBe("3");
    await w.setProps({
      items: [waiting, paid, { ...wx, can_confirm_receipt: false }],
    });
    expect(
      w.findAll("button").filter((b) => b.text() === "确认收款"),
    ).toHaveLength(0);
    w.unmount();
  }
});
it("recipient errors identify the invalid fields and focus the first without sending credentials", async () => {
  const client = recipientClient();
  const w = mount(SettlementRecipientPanel, {
    attachTo: document.body,
    props: { client, admin: false, disabled: false },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "配置收款资料")!
    .trigger("click");
  await w
    .findAll("button")
    .find((b) => b.text() === "保存收款版本")!
    .trigger("click");
  await flushPromises();
  expect(w.get('input[aria-label="收款账号"]').attributes("aria-invalid")).toBe(
    "true",
  );
  expect(w.get('input[aria-label="真实姓名"]').attributes("aria-invalid")).toBe(
    "true",
  );
  expect(document.activeElement?.getAttribute("aria-label")).toBe("收款账号");
  expect(w.text()).toContain("请填写收款账号");
  expect(w.text()).toContain("请填写真实姓名");
  expect(
    vi
      .mocked(client.request)
      .mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(0);
  w.unmount();
});
it("editing an existing WeChat recipient reuses its known app identity and explains its source", async () => {
  const client = recipientClient({
    state: "configured",
    config_id: "42",
    type: "wxpay",
    app_id: "wx-existing-config",
    account_masked: "***payee",
    name_masked: "***",
  });
  const w = mount(SettlementRecipientPanel, {
    props: { client, admin: false, disabled: false },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "修改收款资料")!
    .trigger("click");
  expect(
    (w.get('input[aria-label="收款所属 AppID"]').element as HTMLInputElement)
      .value,
  ).toBe("wx-existing-config");
  expect(w.text()).toContain("OpenID");
  expect(w.text()).toContain("平台管理员");
  w.unmount();
});
it("missing WeChat app identity focuses its field and bank selection uses the existing real code catalog", async () => {
  const client = recipientClient();
  const w = mount(SettlementRecipientPanel, {
    attachTo: document.body,
    props: { client, admin: false, disabled: false },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "配置收款资料")!
    .trigger("click");
  await w.get('[aria-label="收款方式"]').trigger("click");
  await w
    .findAll('[role="option"]')
    .find((b) => b.text() === "微信")!
    .trigger("click");
  await w.get('input[aria-label="收款账号"]').setValue("test-openid");
  await w.get('input[aria-label="真实姓名"]').setValue("Synthetic Name");
  await w
    .findAll("button")
    .find((b) => b.text() === "保存收款版本")!
    .trigger("click");
  await flushPromises();
  expect(document.activeElement?.getAttribute("aria-label")).toBe(
    "收款所属 AppID",
  );
  expect(
    w.get('input[aria-label="收款所属 AppID"]').attributes("aria-invalid"),
  ).toBe("true");
  await w.get('[aria-label="收款方式"]').trigger("click");
  await w
    .findAll('[role="option"]')
    .find((b) => b.text() === "银行卡")!
    .trigger("click");
  await w.get('[aria-label="选择开户银行"]').trigger("click");
  await w
    .findAll('[role="option"]')
    .find((b) => b.text().includes("ICBC · 1002"))!
    .trigger("click");
  await w.get('input[aria-label="收款账号"]').setValue("not-a-bank-card");
  await w
    .findAll("button")
    .find((b) => b.text() === "保存收款版本")!
    .trigger("click");
  await flushPromises();
  expect(document.activeElement?.getAttribute("aria-label")).toBe("收款账号");
  expect(w.text()).toContain("银行卡号应为8至32位数字");
  expect(
    vi
      .mocked(client.request)
      .mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(0);
  await w.get('input[aria-label="收款账号"]').setValue("6222021234567890");
  vi.mocked(client.request).mockResolvedValueOnce({
    state: "configured",
    config_id: "43",
    type: "bank",
    app_id: "",
    account_masked: "***7890",
    name_masked: "***",
  } as never);
  await w
    .findAll("button")
    .find((b) => b.text() === "保存收款版本")!
    .trigger("click");
  await flushPromises();
  const post = vi
    .mocked(client.request)
    .mock.calls.find(([, o]) => o?.method === "POST");
  expect(post?.[1]?.body).toEqual({
    type: "bank",
    account: "6222021234567890",
    name: "Synthetic Name",
    bank_code: "1002",
    app_id: "",
    expected_config_id: "0",
  });
  expect(
    w.get('[data-testid="settlement-recipient-current"]').text(),
  ).toContain("版本 43");
  w.unmount();
});
