import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import DashboardOverview from "../src/components/DashboardOverview.vue";
import fixture from "../e2e/subchannel168b-dashboard-fixture.json";
it("renders actual current parent fees per row, with unknown kept distinct from zero", () => {
  const data = {...structuredClone(fixture.parent50),payment_channels:structuredClone(fixture.parent50.payment_channels) as {channel_id:string,type:string,fee_rate_bps:number|null,fee_source:string}[]};
  data.payment_channels.push(
    {
      ...data.payment_channels[0],
      channel_id: "9007199254740993",
      fee_rate_bps: 1000,
      fee_source: "parent",
    },
    {
      ...data.payment_channels[0],
      channel_id: "9",
      fee_rate_bps: null,
      fee_source: "unknown",
    },
  );
  const w = mount(DashboardOverview, { props: { area: "merchant", data } });
  try {
    const table = w.find('table[aria-label="通道费率"]');
    const rows = table.findAll("tbody tr");
    expect(rows[0].text()).toContain("0.50%");
    expect(rows[1].text()).toContain("10.00%");
    expect(rows[1].text()).toContain("9007199254740993");
    expect(rows[2].text()).toContain("暂不可确认");
    expect(w.text()).not.toContain("统一商户费率");
  } finally {
    w.unmount();
  }
});
it("fee-disabled actual projection displays exact zero without repricing prior orders", () => {
  const w = mount(DashboardOverview, {
    props: { area: "merchant", data: fixture.off },
  });
  try {
    expect(w.find('table[aria-label="通道费率"]').text()).toContain("0.00%");
  } finally {
    w.unmount();
  }
});
