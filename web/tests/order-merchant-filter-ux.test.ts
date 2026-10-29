// @vitest-environment jsdom
import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import ListFilters from "../src/components/ListFilters.vue";
import { freshListFilters } from "../src/list-filters";

// Pro shows 商户号 in the MAIN filter row of trade/order and trade/refund, so the
// App.vue search group owns it there (admin). ListFilters must NOT also render a
// 商户 ID field for orders/refunds — only for 资金明细 (ledger), where Pro has no
// separate search row and merchant_id lives among the advanced filters.
it("admin order/refund 展开 no longer carries 商户 ID (moved to the main row); ledger keeps it", async () => {
  for (const kind of ["orders", "refunds"] as const) {
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
  const ledger = mount(ListFilters, {
    props: { kind: "ledger", area: "admin", modelValue: freshListFilters() },
  });
  await ledger.get("button.pa-filter-toggle").trigger("click");
  expect(ledger.find('[aria-label="商户 ID 筛选"]').exists()).toBe(true);
  ledger.unmount();
  // A merchant never sees a merchant-id filter in either place.
  const merchant = mount(ListFilters, {
    props: { kind: "orders", area: "merchant", modelValue: freshListFilters() },
  });
  await merchant.get("button.pa-filter-toggle").trigger("click");
  expect(merchant.find('[aria-label="商户 ID 筛选"]').exists()).toBe(false);
  merchant.unmount();
});
