// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import OrderSummaryDialog from "../src/components/OrderSummaryDialog.vue";
const overview = {
  totalCount: "286",
  paidCount: "12",
  unpaidCount: "274",
  refundedCount: "3",
  successRate: "4.20%",
  orderMoney: "14014.00",
  paidMoney: "588.00",
  merchantMoney: "585.60",
  refundedMoney: "147.00",
};
it("order 订单统计概况 shows Pro's amounts, counts and success rate over the current filter", async () => {
  const client = new ApiClient("admin"),
    calls = vi.spyOn(client, "request").mockResolvedValue(overview as never),
    w = mount(OrderSummaryDialog, {
      props: { client, query: "type=alipay&status=paid" },
      global: { stubs: { teleport: true } },
    });
  await flushPromises();
  // reads the bounded summary endpoint with the current filter
  expect(calls.mock.calls[0]?.[0]).toBe(
    "/orders/summary?type=alipay&status=paid",
  );
  const text = w.text();
  // Pro's 金额统计: ¥-prefixed money
  expect(text).toContain("订单总金额");
  expect(text).toContain("¥14014.00");
  expect(text).toContain("商户分成金额");
  expect(text).toContain("¥585.60");
  expect(text).toContain("已退款金额");
  expect(text).toContain("¥147.00");
  // Pro's 数量统计 + success rate
  expect(text).toContain("订单总数");
  expect(text).toContain("286");
  expect(text).toContain("已支付订单");
  expect(text).toContain("订单成功率");
  expect(text).toContain("4.20%");
  // read-only overview: a single 关闭, never a 保存, and no write method
  const buttons = w.findAll("button").map((b) => b.text());
  expect(buttons).toContain("关闭");
  expect(buttons).not.toContain("保存");
  expect(calls.mock.calls.every(([, o]) => !o?.method)).toBe(true);
  // kit modal look
  expect(w.find(".pa-modal").exists()).toBe(true);
  w.unmount();
});
it("rejects a malformed overview and clears any previous result", async () => {
  const client = new ApiClient("admin"),
    calls = vi.spyOn(client, "request").mockResolvedValue(overview as never),
    w = mount(OrderSummaryDialog, {
      props: { client, query: "" },
      global: { stubs: { teleport: true } },
    });
  await flushPromises();
  expect(w.text()).toContain("286");
  calls.mockResolvedValueOnce({ ...overview, orderMoney: "1e3" } as never);
  await w.setProps({ query: "status=paid" });
  await flushPromises();
  expect(w.text()).not.toContain("¥14014.00");
  expect(w.get('[role="alert"]').text()).toContain("格式无效");
  w.unmount();
});
