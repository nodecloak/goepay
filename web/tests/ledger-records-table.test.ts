import { afterEach, expect, it } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import LedgerRecordsTable from "../src/components/LedgerRecordsTable.vue";
import ListFilters from "../src/components/ListFilters.vue";
import { freshListFilters } from "../src/list-filters";

const mounted: VueWrapper[] = [];
afterEach(() => mounted.splice(0).forEach((w) => w.unmount()));
const entry = {
  id: "9007199254740993",
  merchant_id: "1000",
  ledger_type: "order_refund",
  amount: "-1",
  balance_before: "9223372036854775807",
  balance_after: "9223372036854775806",
  kind: "available",
  account_id: "9007199254740993123",
  created_at: "2026-11-01T01:02:03Z",
  event_key: "refund:INTERNAL-123:hold-cycle-456",
  description: "order refund",
  private_key: "do-not-show-private-value",
  unrelated: { password: "do-not-show-password" },
};
function setup(
  area: "admin" | "merchant" = "admin",
  items: Record<string, unknown>[] = [entry],
) {
  const w = mount(LedgerRecordsTable, {
    props: { area, items },
    global: { stubs: { teleport: true } },
  });
  mounted.push(w);
  return w;
}
it("shows operational account-level columns with exact balances and the complete event key", () => {
  const w = setup();
  expect(w.findAll("thead th").map((x) => x.text())).toEqual([
    "商户号",
    "操作类型",
    "变更金额",
    "账户变更前",
    "账户变更后",
    "时间",
    "关联订单/业务号",
    "操作",
  ]);
  expect(w.text()).toContain("账户");
  expect(w.text()).toContain("不代表商户总资产");
  expect(w.text()).toContain("92233720368547758.07");
  expect(w.text()).toContain("92233720368547758.06");
  expect(w.text()).toContain("¥-0.01");
  expect(w.text()).toContain("订单退款");
  expect(w.text()).toContain("2026-11-01 09:02:03");
  expect(w.find("tbody").text()).toContain(entry.event_key);
  expect(w.find("tbody").text()).not.toContain(entry.account_id);
  expect(w.find("tbody a").exists()).toBe(false);
});
it("uses a real trade_no before event_key and never derives an order link", () => {
  const w = setup("admin", [{ ...entry, trade_no: "ACTUAL.ORDER|123" }]);
  expect(w.find("tbody").text()).toContain("ACTUAL.ORDER|123");
  expect(w.find("tbody").text()).not.toContain(entry.event_key);
  expect(w.find("tbody a").exists()).toBe(false);
});
it("opens only local safe metadata, keeps account identities out of the default columns, and closes on context replacement", async () => {
  const w = setup();
  await w.find("tbody button").trigger("click");
  await flushPromises();
  const dialog = w.get('[role="dialog"]');
  expect(dialog.text()).toContain(entry.id);
  expect(dialog.text()).toContain(entry.account_id);
  expect(dialog.text()).toContain("可用余额");
  expect(dialog.text()).toContain(entry.event_key);
  expect(dialog.text()).not.toContain(entry.private_key);
  expect(dialog.text()).not.toContain("do-not-show-password");
  expect(dialog.find("input,textarea,select").exists()).toBe(false);
  expect(dialog.text()).not.toContain("删除");
  await w.setProps({ items: [{ ...entry, id: "123" }] });
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
});
it("omits the admin merchant column from the merchant view and keeps an empty state", () => {
  const merchant = setup("merchant");
  expect(merchant.findAll("thead th").map((x) => x.text())).not.toContain(
    "商户号",
  );
  const empty = setup("admin", []);
  expect(empty.text()).toContain("暂无资金记录");
});
it("does not invent missing balances or render unsafe rounded number identifiers", () => {
  const w = setup("admin", [
    {
      id: Number.MAX_SAFE_INTEGER + 1,
      ledger_type: "order_fee",
      amount: "0",
      balance_before: null,
      balance_after: null,
      event_key: "request-only",
    },
  ]);
  expect(w.text()).not.toContain(String(Number.MAX_SAFE_INTEGER + 1));
  expect(w.findAll("tbody td").map((x) => x.text())).toContain("—");
  expect(w.text()).not.toContain("0.00 → 0.00");
});
it("does not duplicate the admin merchant filter in the expandable ledger filter area", async () => {
  const w = mount(ListFilters, {
    props: { kind: "ledger", area: "admin", modelValue: freshListFilters() },
  });
  mounted.push(w);
  await w
    .findAll("button")
    .find((b) => b.text().includes("展开"))!
    .trigger("click");
  expect(w.find('input[aria-label="商户 ID 筛选"]').exists()).toBe(false);
});
