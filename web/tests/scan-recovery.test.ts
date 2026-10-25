// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import ScanRecoveryPanel from "../src/components/ScanRecoveryPanel.vue";

const recovery = (state: string) => ({
  state,
  queries: 6,
  maximum_queries: 6,
  last_provider_state: "WAIT_BUYER_PAY",
  started_at: "2026-10-24T10:00:00Z",
  expires_at: "2026-10-24T10:02:00Z",
  requires_reconciliation: state === "cancel_pending",
});

describe("original barcode recovery status", () => {
  it("keeps an unknown cancellation distinct from paid and cancelled without exposing another financial action", () => {
    const panel = mount(ScanRecoveryPanel, {
      props: { recovery: recovery("cancel_pending") },
    });
    expect(panel.text()).toContain("撤销结果待核实");
    expect(panel.text()).toContain("确认前不会入账");
    expect(panel.text()).toContain("6 / 6");
    expect(panel.text()).toContain("WAIT_BUYER_PAY");
    expect(panel.text()).not.toContain("已撤销，商户余额未增加");
    expect(panel.findAll("button, form, input, a")).toHaveLength(0);
  });

  it("renders a verified cancellation without implying merchant income or a new refund", () => {
    const panel = mount(ScanRecoveryPanel, {
      props: { recovery: recovery("cancelled") },
    });
    expect(panel.text()).toContain("已撤销");
    expect(panel.text()).toContain("商户余额未增加");
    expect(panel.text()).not.toContain("已支付");
    expect(panel.findAll("button")).toHaveLength(0);
  });

  it("hides absent historical recovery and treats future unknown states as unverified", () => {
    const historical = mount(ScanRecoveryPanel, { props: { recovery: null } });
    expect(historical.find("section").exists()).toBe(false);
    const panel = mount(ScanRecoveryPanel, {
      props: { recovery: recovery("future-state") },
    });
    expect(panel.text()).toContain("处理状态待核实");
    expect(panel.text()).not.toContain("已支付");
    expect(panel.findAll("button")).toHaveLength(0);
  });
});
