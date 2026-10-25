import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import SettlementAutoAuthorization from "../src/components/SettlementAutoAuthorization.vue";
const plan = {
  id: "4",
  revision: "3",
  last_authorization_id: "8",
  merchant_ids: ["2"],
};
const view = {
  id: "0",
  plan_id: "4",
  available: true,
  wallet_channels: [
    {
      id: "7",
      provider: "alipay",
      product: "alipay.fund.trans.toaccount.transfer",
      app_id: "local-app",
      payer_masked: "***0073",
      merchant_ids: ["2"],
    },
  ],
};
const button = (w: ReturnType<typeof mount>, text: string) =>
  w.findAll("button").find((b) => b.text() === text)!;
it("reads the exact parent plan fields when the real page also supplies a reactive schedule", async () => {
  const request = vi.fn().mockResolvedValue(view);
  const pagePlan = reactive({ ...plan, schedule: { frequency: "day" } });
  const client = Object.assign(new ApiClient("admin"), { request });
  const w = mount(SettlementAutoAuthorization, { props: { client, plan: pagePlan, ownSession: true, adminClass: "super" } });
  await button(w, "读取自动付款授权").trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(1);
  expect(w.text()).toContain("原自动授权 0");
  w.unmount();
});
it("does not read or authorize an automatic policy from delegated or operator context", async () => {
  const client = new ApiClient("admin"),
    request = vi.spyOn(client, "request");
  for (const access of [
    { ownSession: false, adminClass: "super" },
    { ownSession: true, adminClass: "operator" },
  ]) {
    const w = mount(SettlementAutoAuthorization, {
      props: { client, plan, ...access },
    });
    await flushPromises();
    expect(w.find("button").exists()).toBe(false);
    expect(request).not.toHaveBeenCalled();
    w.unmount();
  }
});
it("submits one immutable independent scope and does not retry an uncertain authorization", async () => {
  const request = vi.fn((_: string, options?: { method?: string }) =>
    options?.method === "POST"
      ? Promise.reject(new Error("连接中断"))
      : Promise.resolve(view),
  );
  const client = Object.assign(new ApiClient("admin"), { request });
  const w = mount(SettlementAutoAuthorization, {
    props: { client, plan, ownSession: true, adminClass: "super" },
  });
  await button(w, "读取自动付款授权").trigger("click");
  await flushPromises();
  const inputs = w.findAll('input:not([type="checkbox"])');
  for (const [i, value] of [
    "2026-10-24T00:00:00Z",
    "2026-10-25T00:00:00Z",
    "100",
    "200",
    "300",
    "2",
  ].entries())
    await inputs[i]!.setValue(value);
  await w.find('input[type="checkbox"]').setValue(true);
  await button(w, "核对独立自动出款授权").trigger("click");
  await inputs[2]!.setValue("999");
  await button(w, "确认授权并验证本人资金密码").trigger("click");
  await flushPromises();
  expect(request.mock.calls[1]).toMatchObject([
    "/settlement-plans/4/auto-authorizations",
    {
      method: "POST",
      body: {
        expected_revision: "3",
        expected_authorization_id: "0",
        generation_authorization_id: "8",
        max_item_gross: "100",
        channels: [{ merchant_id: "2", channel_id: "7" }],
        cost_policy: "provider_managed_excluded",
      },
    },
  ]);
  expect(w.text()).toContain("请勿重复提交");
  await button(w, "核对独立自动出款授权").trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  w.unmount();
});
for (const change of [
  "area",
  "csrf",
  "role",
  "delegated",
  "plan",
  "unmount",
] as const)
  it(`discards a late original auto policy after ${change} changes`, async () => {
    let finish: (value: unknown) => void = () => {};
    const request = vi.fn(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const client = reactive(Object.assign(new ApiClient("admin"), { request }));
    const w = mount(SettlementAutoAuthorization, {
      props: { client, plan, ownSession: true, adminClass: "super" },
    });
    await button(w, "读取自动付款授权").trigger("click");
    if (change === "area") client.area = "merchant";
    if (change === "csrf") client.csrfToken = "new-session";
    if (change === "role") await w.setProps({ adminClass: "operator" });
    if (change === "delegated") await w.setProps({ ownSession: false });
    if (change === "plan") await w.setProps({ plan: { ...plan, id: "5" } });
    if (change === "unmount") w.unmount();
    finish(view);
    await flushPromises();
    if (change !== "unmount") {
      expect(w.text()).not.toContain("原自动授权 0");
      expect(w.find("fieldset").exists()).toBe(false);
      w.unmount();
    }
    expect(request).toHaveBeenCalledTimes(1);
  });
