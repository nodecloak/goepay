// @vitest-environment jsdom
import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import DataTable from "../src/components/DataTable.vue";
import { proListColumns } from "../src/pro-console-fields";
const columns = [
  { key: "refund_no", label: "退款单号" },
  { key: "trade_no", label: "平台订单号" },
  { key: "amount", label: "退款金额（分）" },
  { key: "created_at", label: "申请时间" },
  { key: "status", label: "状态" },
];
it("refund columns display separate original amounts and immutable terminal time and emit only the associated order", async () => {
  const row = {
    id: "9223372036854775807",
    trade_no: "ORDER-130",
    amount: "1234",
    merchant_amount: "1100",
    created_at: "2026-10-25T00:00:00Z",
    completed_at: "2026-10-25T00:01:00Z",
    updated_at: "2026-10-27T00:00:00Z",
    status: "succeeded",
  };
  const w = mount(DataTable, {
    props: { refunds: true, columns, items: [row] },
  });
  expect(w.text()).toContain("扣减金额");
  expect(w.text()).toContain("完成时间");
  expect(w.text()).toContain("¥12.34");
  expect(w.text()).toContain("¥11.00");
  expect(w.text()).toContain("2026-10-25 08:01:00");
  expect(w.text()).not.toContain("2026-10-27");
  expect(w.text()).toContain("退款成功");
  await w.get("button").trigger("click");
  expect(w.emitted("detail")).toEqual([[row]]);
  expect(w.emitted("refund")).toBeUndefined();
  w.unmount();
});
it("refund column plans match the original administrator six columns and hide merchant number for merchants", () => {
  expect(
    proListColumns("refunds", "admin", [], columns).map((c) => c.key),
  ).toEqual([
    "refund_no",
    "trade_no",
    "merchant_id",
    "amount",
    "created_at",
    "status",
  ]);
  expect(
    proListColumns("refunds", "merchant", [], columns).map((c) => c.key),
  ).toEqual(["refund_no", "trade_no", "amount", "created_at", "status"]);
});
it("unrecorded refund debit and completion remain unknown and malformed order references never become links", () => {
  const w = mount(DataTable, {
    props: {
      refunds: true,
      columns,
      items: [
        {
          trade_no: "../account/profile",
          amount: "0",
          status: "pending",
          created_at: "2026-10-25T00:00:00Z",
          updated_at: "2026-10-27T00:00:00Z",
        },
      ],
    },
  });
  expect(w.text()).toContain("未记录");
  expect(w.text()).toContain("退款中");
  expect(w.text()).not.toContain("2026-10-27");
  expect(w.findAll("button")).toHaveLength(0);
  w.unmount();
});
