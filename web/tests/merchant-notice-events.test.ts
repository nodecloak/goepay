import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import MerchantNoticeSettings from "../src/components/MerchantNoticeSettings.vue";
const settings = {
  id: "0",
  merchant_id: "1000",
  revision: "0",
  voice_order: false,
  email_masked: "",
  configured: {},
  target_masks: {},
  msgconfig: {
    order: 0,
    settle: 0,
    login: 0,
    complain: 0,
    mchrisk: 0,
    balance: 0,
    group: 0,
    domain_audit: 0,
    daily_report: 0,
    order_money: "0.00",
    balance_money: "0.00",
  },
};

it("shows read-only original notification outcomes and pages without retrying delivery", async () => {
  const c = new ApiClient("merchant");
  const calls: string[] = [];
  vi.spyOn(c, "request").mockImplementation(async (p, o) => {
    expect(o?.method ?? "GET").toBe("GET");
    calls.push(p);
    if (!p.includes("/events")) return settings as never;
    return (
      p.includes("before_id=9")
        ? {
            items: [
              {
                id: "8",
                scene: "login",
                at: "2026-10-24T10:00:00Z",
                state: "unknown",
                deliveries: [{ transport: "smtp", state: "unknown" }],
              },
            ],
            next_cursor: "",
          }
        : {
            items: [
              {
                id: "9",
                scene: "order",
                trade_no: "LOCAL80",
                at: "2026-10-24T10:00:00Z",
                state: "accepted",
                deliveries: [{ transport: "smtp", state: "accepted" }],
              },
            ],
            next_cursor: "9",
          }
    ) as never;
  });
  const w = mount(MerchantNoticeSettings, {
    props: { client: c, ownSession: true },
  });
  await flushPromises();
  expect(w.text()).toContain("通知记录");
  expect(w.text()).toContain("LOCAL80");
  expect(w.text()).toContain("已受理");
  const more = w.findAll("button").find((b) => b.text() === "加载更多记录");
  expect(more).toBeDefined();
  await more!.trigger("click");
  await flushPromises();
  expect(w.text()).toContain("待核实");
  expect(w.text()).toContain("LOCAL80");
  expect(calls.some((p) => p.endsWith("/events?limit=20&before_id=9"))).toBe(
    true,
  );
  expect(w.findAll("button").some((b) => /重发|重试发送/.test(b.text()))).toBe(
    false,
  );
  w.unmount();
});

it("discards another merchant's late notification history", async () => {
  const c = new ApiClient("admin");
  let old: (v: unknown) => void = () => {};
  vi.spyOn(c, "request").mockImplementation(async (p) => {
    if (!p.includes("/events")) return settings as never;
    if (p.includes("1000"))
      return (await new Promise<unknown>((resolve) => {
        old = resolve;
      })) as never;
    return { items: [], next_cursor: "" } as never;
  });
  const w = mount(MerchantNoticeSettings, {
    props: {
      client: c,
      ownSession: true,
      adminClass: "operator",
      merchantID: "1000",
    },
  });
  await flushPromises();
  await w.setProps({ merchantID: "1001" });
  await flushPromises();
  old({
    items: [
      {
        id: "9",
        scene: "order",
        trade_no: "OTHER-OWNER80",
        state: "accepted",
        deliveries: [],
      },
    ],
    next_cursor: "",
  });
  await flushPromises();
  expect(w.text()).not.toContain("OTHER-OWNER80");
  expect(w.text()).toContain("暂无通知记录");
  w.unmount();
});
