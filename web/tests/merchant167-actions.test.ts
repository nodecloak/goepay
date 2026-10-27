import { it, expect } from "vitest";
import * as actions from "../src/order-actions";
import { mount } from "@vue/test-utils";
import DataTable from "../src/components/DataTable.vue";
import { readListRouteQuery, appendListFilters } from "../src/list-filters";
it("merchant167 ledger and refund links bind the same exact trade to API filters", () => {
  for (const kind of ["ledger", "refunds"]) {
    const r = readListRouteQuery(
      { trade_no: "202610260000000000167" },
      kind,
      "merchant",
      "9007199254740993",
    );
    expect(r.error).toBe("");
    expect(r.search).toBe("202610260000000000167");
    const q = new URLSearchParams();
    appendListFilters(q, r.filters, kind, "merchant");
    expect(q.get("search_field")).toBe("trade_no");
  }
});
const row = (patch: Record<string, unknown> = {}) => ({
  id: "9007199254740993",
  trade_no: "202610260000000000167",
  status: "paid",
  amount: "10000",
  refunded_amount: "0",
  merchant_actions: { query: false, notify: true, refund: true, print: true },
  ...patch,
});
it("merchant167 uses verified booleans, exact state/money and scoped original links without administrator actions", () => {
  expect(typeof actions.merchantOrderActions).toBe("function");
  const ids = (v: Record<string, unknown>) =>
    actions.merchantOrderActions(v).map((a: any) => a.id);
  expect(ids(row())).toEqual(["detail", "ledger", "notify", "refund", "print"]);
  expect(
    ids(row({ status: "pending", merchant_actions: { query: true } })),
  ).toEqual(["detail", "ledger", "query"]);
  expect(ids(row({ status: "pending", merchant_actions: undefined }))).toEqual([
    "detail",
    "ledger",
  ]);
  expect(
    ids(
      row({
        status: "paid",
        refunded_amount: "10000",
        merchant_actions: { refund: false },
      }),
    ),
  ).toEqual(["detail", "ledger", "refund-history"]);
  expect(
    ids(
      row({
        status: "unknown",
        merchant_actions: {
          query: true,
          notify: true,
          refund: true,
          print: true,
        },
      }),
    ),
  ).toEqual(["detail", "ledger"]);
  for (const a of actions.merchantOrderActions(row({ refunded_amount: "1" })))
    if (a.href) expect(a.href).toContain("trade_no=202610260000000000167");
  expect(
    ids(
      row({
        status: "paid",
        income_status: "held",
        merchant_actions: { refund: true },
      }),
    ),
  ).toContain("refund");
});
it("merchant167 actual table exposes the same ActionDropdown and no direct privileged operation", async () => {
  const view = mount(DataTable, {
    props: {
      items: [row()],
      columns: [{ key: "trade_no", label: "系统订单号" }],
      orders: true,
      merchantOrders: true,
    } as any,
    attachTo: document.body,
  });
  const button = view.get(
    'button[aria-label="订单操作 202610260000000000167"]',
  );
  await button.trigger("click");
  expect(document.querySelector('[role="menu"]')?.textContent).toContain(
    "资金明细",
  );
  expect(document.querySelector('[role="menu"]')?.textContent).not.toMatch(
    /冻结订单|手动退款|改已完成/,
  );
  view.unmount();
});
