// @vitest-environment jsdom
import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import DataTable from "../src/components/DataTable.vue";

const columns = [
  { key: "trade_no", label: "系统订单号" },
  { key: "merchant_id", label: "商户号" },
  { key: "name", label: "商品名称" },
  { key: "realmoney", label: "实际支付" },
  { key: "type", label: "支付方式" },
  { key: "created_at", label: "创建时间" },
  { key: "status", label: "支付状态" },
];
const base = {
  out_trade_no: "ux103-alipay-1239bc6f-26f5-4b73-9fcb-901b13cc29a0",
  api_trade_no: "sim-1",
  merchant_id: "1000",
  domain: "merchant.example.test",
  name: "UX103 synthetic alipay",
  amount: "123",
  realmoney: "123",
  merchant_amount: "120",
  type: "alipay",
  plugin: "UX103 synthetic alipay",
  created_at: "2026-10-25T08:13:01Z",
  paid_at: null,
  status: "pending",
  admin_status: "",
  notification_state: "none",
  income_status: "none",
  refunded_amount: "0",
};
function statusCells(items: Record<string, unknown>[]) {
  const w = mount(DataTable, {
    props: { orders: true, adminOrders: true, columns, items },
    global: { stubs: { OrderActionMenu: true, MerchantOrderActionMenu: true } },
  });
  const cells = w.findAll("td.col-status");
  return { w, cells };
}

it("order rows are two short lines: number over merchant number, name over amount", () => {
  const { w } = statusCells([
    { ...base, id: "1", trade_no: "1000000000000000003" },
  ]);
  const numbers = w.get("td.col-trade_no");
  expect(numbers.get("button").text()).toBe("1000000000000000003");
  const merchantNo = numbers.get("small");
  expect(merchantNo.classes()).toContain("order-ellipsis");
  expect(merchantNo.text()).toBe(base.out_trade_no);
  const name = w.get("td.col-name");
  expect(name.get("span").classes()).toContain("order-ellipsis");
  expect(name.get("span").attributes("title")).toBe(base.name);
  expect(name.get("small.order-amount").text()).toBe("¥1.23");
  const money = w.get("td.col-realmoney");
  expect(money.findAll(".order-money").map((e) => e.text())).toEqual([
    "¥1.23",
    "¥1.20",
  ]);
  expect(w.get("td.col-type small").attributes("title")).toBe(base.plugin);
  w.unmount();
});

it("the status cell names the notice state, and says nothing extra for unpaid orders", () => {
  const { w, cells } = statusCells([
    {
      ...base,
      id: "1",
      trade_no: "1",
      status: "paid",
      notification_state: "pending",
    },
    { ...base, id: "2", trade_no: "2" },
    {
      ...base,
      id: "3",
      trade_no: "3",
      status: "paid",
      notification_state: "dead",
    },
    {
      ...base,
      id: "4",
      trade_no: "4",
      status: "paid",
      notification_state: "succeeded",
    },
  ]);
  const [paidPending, unpaid, paidDead, paidDone] = cells;
  expect(paidPending!.text()).toBe("已支付待通知");
  expect(paidPending!.get(".order-flag").classes()).toContain(
    "order-flag--warning",
  );
  // An unpaid order shows its payment state only (no notice, no freeze word).
  expect(unpaid!.text()).toBe("待支付");
  expect(unpaid!.find(".order-flag").exists()).toBe(false);
  expect(paidDead!.get(".order-flag").text()).toBe("通知失败");
  expect(paidDead!.get(".order-flag").classes()).toContain(
    "order-flag--danger",
  );
  expect(paidDone!.get(".order-flag").text()).toBe("已通知");
  for (const c of cells) {
    expect(c.text()).not.toContain("待处理");
    expect(c.text()).not.toContain("未冻结");
  }
  w.unmount();
});

it("the status cell adds the real payment state, refunds and frozen income only when they apply", () => {
  const { w, cells } = statusCells([
    { ...base, id: "1", trade_no: "1", admin_status: "paid" },
    { ...base, id: "2", trade_no: "2", status: "paid", admin_status: "paid" },
    { ...base, id: "3", trade_no: "3", status: "paid", refunded_amount: "23" },
    { ...base, id: "4", trade_no: "4", status: "paid", income_status: "held" },
  ]);
  const [override, same, refunded, held] = cells;
  expect(override!.text()).toContain("已支付");
  expect(override!.text()).toContain("实际状态：待支付");
  expect(same!.text()).not.toContain("实际状态");
  expect(refunded!.text()).toContain("已退款 ¥0.23");
  expect(held!.text()).toContain("收入冻结中");
  expect(same!.text()).not.toContain("已退款");
  expect(same!.text()).not.toContain("收入冻结中");
  w.unmount();
});
