import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import Panel from "../src/components/SettlementPayoutPanel.vue";
const recipient = {
  state: "recorded",
  config_id: "42",
  type: "alipay",
  app_id: "",
  account_masked: "***1234",
  name_masked: "***",
};
const funding = {
  state: "reserved",
  account_id: "90",
  gross_amount: "123",
  reserved_amount: "123",
};
const request = {
  id: "12",
  merchant_id: "1000",
  status: "approved",
  amount_cents: "123",
  settlement: { gross_amount: "123", fee_amount: "3", net_amount: "120" },
  funding,
  recipient,
};
const intent = (state = "unsent", send_state = "unsent") => ({
  request_id: "12",
  merchant_id: "1000",
  channel_id: "1",
  transfer_id: "90",
  state,
  send_state,
  transfer: { id: "90", transfer_no: "TF90", amount: "120", status: "pending" },
  funding,
  recipient,
  created_at: "2026-10-23T00:00:00Z",
});
const caps = {
  request_id: "12",
  can_prepare: true,
  writes_enabled: true,
  channels: [{ id: "1", type: "alipay", app_id: "" }],
  reason: "",
};
it("separates prepare confirmation from execution and leaves unknown query-only without a second send", async () => {
  let payout: unknown = null;
  const read = vi.fn(async (path: string) =>
    path.endsWith("/capabilities") ? caps : { request, payout },
  );
  const write = vi.fn(async (path: string) => {
    payout = path.endsWith("/prepare")
      ? intent()
      : intent("unknown", "claimed");
    return { status: path.endsWith("/execute") ? 202 : 200, data: payout };
  });
  const wrapper = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("admin"), {
        request: read,
        requestResult: write,
      }),
      area: "admin",
      requestId: "12",
    },
  });
  await flushPromises();
  expect(wrapper.text()).toContain("核准不代表已出款");
  await wrapper.get("select").setValue("1");
  await wrapper.get("button[data-action=prepare]").trigger("click");
  expect(write).not.toHaveBeenCalled();
  await wrapper.get("button[data-action=confirm]").trigger("click");
  await flushPromises();
  expect(write).toHaveBeenCalledWith(
    "/settlement-payouts/12/prepare",
    expect.objectContaining({ method: "POST", body: { channel_id: "1" } }),
  );
  await wrapper.get("button[data-action=execute]").trigger("click");
  expect(write).toHaveBeenCalledTimes(1);
  await wrapper.get("button[data-action=confirm]").trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("待核实");
  expect(wrapper.find("button[data-action=execute]").exists()).toBe(false);
  expect(wrapper.find("button[data-action=prepare]").exists()).toBe(false);
  expect(wrapper.find("button[data-action=query]").exists()).toBe(true);
  expect(write).toHaveBeenCalledTimes(2);
  wrapper.unmount();
});
it("merchant never fetches admin capabilities or sees sends; network uncertainty blocks writes until original request is re-read", async () => {
  const read = vi
    .fn()
    .mockResolvedValue({ request, payout: intent("unknown", "claimed") });
  const write = vi.fn().mockRejectedValue(new Error("network unavailable"));
  const wrapper = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("merchant"), {
        request: read,
        requestResult: write,
      }),
      area: "merchant",
      requestId: "12",
    },
  });
  await flushPromises();
  expect(read).toHaveBeenCalledTimes(1);
  expect(wrapper.find("select").exists()).toBe(false);
  await wrapper.get("button[data-action=query]").trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("network unavailable");
  expect(write).toHaveBeenCalledTimes(1);
  expect(wrapper.find("button[data-action=execute]").exists()).toBe(false);
  wrapper.unmount();
});
it("rejects stale request responses and does not retain a confirmation across request changes", async () => {
  let resolve!: (value: unknown) => void;
  const read = vi.fn((path: string) =>
    path.includes("/12")
      ? new Promise((r) => (resolve = r))
      : Promise.resolve(
          path.endsWith("/capabilities")
            ? { ...caps, request_id: "13" }
            : { request: { ...request, id: "13" }, payout: null },
        ),
  );
  const wrapper = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request: read }),
      area: "admin",
      requestId: "12",
    },
  });
  await wrapper.setProps({ requestId: "13" });
  await flushPromises();
  resolve({ request, payout: intent() });
  await flushPromises();
  expect(wrapper.text()).toContain("申请 13");
  expect(wrapper.text()).not.toContain("TF90");
  wrapper.unmount();
});
it("permits proof-unsent cancellation while provider writes are disabled and never labels released funds as paid", async () => {
  let payout = intent();
  const read = vi.fn(async (path: string) =>
    path.endsWith("/capabilities")
      ? {
          ...caps,
          can_prepare: false,
          writes_enabled: false,
          reason: "provider writes disabled",
        }
      : { request, payout },
  );
  const write = vi.fn(async () => {
    payout = {
      ...intent("cancelled", "cancelled"),
      funding: { ...funding, state: "released", reserved_amount: "0" },
    };
    return { status: 200, data: payout };
  });
  const wrapper = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("admin"), {
        request: read,
        requestResult: write,
      }),
      area: "admin",
      requestId: "12",
    },
  });
  await flushPromises();
  expect(wrapper.find("button[data-action=execute]").exists()).toBe(false);
  await wrapper.get("button[data-action=cancel]").trigger("click");
  expect(write).not.toHaveBeenCalled();
  await wrapper.get("button[data-action=confirm]").trigger("click");
  await flushPromises();
  expect(write).toHaveBeenCalledWith(
    "/settlement-payouts/12/cancel-unsent",
    expect.objectContaining({ method: "POST", body: {} }),
  );
  expect(wrapper.text()).toContain("本申请已释放");
  expect(wrapper.text()).not.toContain("已核实出款成功");
  expect(wrapper.find("button[data-action=cancel]").exists()).toBe(false);
  wrapper.unmount();
});
it("a failed administrator capabilities read cannot enable prepare or execute from stale data", async () => {
  const read = vi.fn(async (path: string) => {
    if (path.endsWith("/capabilities")) throw new Error("policy unavailable");
    return { request, payout: intent() };
  });
  const wrapper = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request: read }),
      area: "admin",
      requestId: "12",
    },
  });
  await flushPromises();
  expect(wrapper.text()).toContain("policy unavailable");
  expect(wrapper.find("button[data-action=prepare]").exists()).toBe(false);
  expect(wrapper.find("button[data-action=execute]").exists()).toBe(false);
  expect(wrapper.find("button[data-action=cancel]").exists()).toBe(false);
  wrapper.unmount();
});
