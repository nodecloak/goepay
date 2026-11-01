// @vitest-environment jsdom
import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import ListFilters from "../src/components/ListFilters.vue";
import { freshListFilters } from "../src/list-filters";

// Pro shows 商户号 in the MAIN filter row of trade/order and trade/refund, so the
// App.vue search group owns it there (admin). ListFilters must NOT also render a
// 商户 ID field: App.vue owns the main-row input for all three lists.
it("admin order/refund/ledger advanced filters do not duplicate the main merchant filter", async () => {
  for (const kind of ["orders", "refunds", "ledger"] as const) {
    const w = mount(ListFilters, {
      props: { kind, area: "admin", modelValue: freshListFilters() },
    });
    await w.get("button.pa-filter-toggle").trigger("click");
    expect(
      w.find('button.pa-select-trigger[aria-label="商户 ID 筛选"]').exists(),
    ).toBe(false);
    expect(w.find('[aria-label="商户 ID 筛选"]').exists()).toBe(false);
    w.unmount();
  }
  // A merchant never sees a merchant-id filter in either place.
  const merchant = mount(ListFilters, {
    props: { kind: "orders", area: "merchant", modelValue: freshListFilters() },
  });
  await merchant.get("button.pa-filter-toggle").trigger("click");
  expect(merchant.find('[aria-label="商户 ID 筛选"]').exists()).toBe(false);
  merchant.unmount();
});
