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

it.each([
  ["merchant", "orders", true, false],
  ["merchant", "refunds", false, true],
  ["admin", "orders", false, false],
  ["admin", "refunds", false, false],
] as const)(
  "keeps the original primary filters for %s %s without duplicate advanced fields",
  async (area, kind, paymentVisible, datesVisible) => {
    const w = mount(ListFilters, {
      props: { kind, area, modelValue: freshListFilters() },
    });
    const payment = () => w.findAll('[aria-label="支付方式筛选"]');
    const from = () => w.findAll('[aria-label="创建开始日期"]');
    const to = () => w.findAll('[aria-label="创建结束日期"]');
    expect(payment()).toHaveLength(paymentVisible ? 1 : 0);
    expect(from()).toHaveLength(datesVisible ? 1 : 0);
    expect(to()).toHaveLength(datesVisible ? 1 : 0);
    expect(w.find('[aria-label="状态筛选"]').exists()).toBe(true);
    await w.get("button.pa-filter-toggle").trigger("click");
    expect(payment()).toHaveLength(1);
    expect(from()).toHaveLength(1);
    expect(to()).toHaveLength(1);
    await w.get("button.pa-filter-toggle").trigger("click");
    expect(payment()).toHaveLength(paymentVisible ? 1 : 0);
    expect(from()).toHaveLength(datesVisible ? 1 : 0);
    w.unmount();
  },
);
it("retains the merchant payment draft while advanced filters open and close", async () => {
  const filters = freshListFilters();
  const w = mount(ListFilters, {
    props: { kind: "orders", area: "merchant", modelValue: filters },
  });
  await w.get('[aria-label="支付方式筛选"]').trigger("click");
  await w
    .findAll('[role="option"]')
    .find((option) => option.text() === "支付宝")!
    .trigger("click");
  expect(filters.type).toBe("alipay");
  await w.get("button.pa-filter-toggle").trigger("click");
  await w.get("button.pa-filter-toggle").trigger("click");
  expect(filters.type).toBe("alipay");
  expect(w.get('[aria-label="支付方式筛选"]').text()).toContain("支付宝");
  w.unmount();
});
it("retains merchant refund date drafts while advanced filters open and close", async () => {
  const filters = freshListFilters();
  const w = mount(ListFilters, {
    props: { kind: "refunds", area: "merchant", modelValue: filters },
  });
  await w.get('[aria-label="创建开始日期"]').setValue("2026-10-01");
  await w.get('[aria-label="创建结束日期"]').setValue("2026-10-12");
  await w.get("button.pa-filter-toggle").trigger("click");
  await w.get("button.pa-filter-toggle").trigger("click");
  expect(filters.from).toBe("2026-10-01");
  expect(filters.to).toBe("2026-10-12");
  expect(w.findAll('[aria-label="创建开始日期"]')).toHaveLength(1);
  w.unmount();
});
