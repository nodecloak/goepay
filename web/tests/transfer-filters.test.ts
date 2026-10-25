import { it, expect } from "vitest";
import * as api from "../src/transfer-filters";
it("original public transfer search fields preserve exact method-independent meanings", () => {
  expect(typeof api.transferSearchFields).toBe("object");
  expect(api.transferSearchFields.map((v: any) => [v.value, v.label])).toEqual([
    ["", "商户交易号（包含）"],
    ["biz_no", "交易号"],
    ["out_biz_no", "商户交易号"],
    ["pay_order_no", "接口交易号"],
    ["money", "付款金额"],
  ]);
});
it("transfer read links preserve original aliases long IDs and exact amount text", () => {
  expect(typeof api.readTransferRoute).toBe("function");
  const r = api.readTransferRoute(
    {
      uid: "9223372036854775807",
      column: "money",
      value: "90071992547409.93",
      status: "unknown",
      channel_id: "9007199254740993",
      from: "2026-10-01",
      to: "2026-10-26",
      page: "2",
      limit: "10",
    },
    "admin",
    "1",
  );
  expect(r.error).toBe("");
  expect(r.page).toBe(2);
  expect(r.filters).toEqual({
    merchant_id: "9223372036854775807",
    search_field: "money",
    q: "90071992547409.93",
    status: "unknown",
    channel_id: "9007199254740993",
    from: "2026-10-01",
    to: "2026-10-26",
  });
});
it("invalid transfer read links never become an unrestricted list", () => {
  expect(typeof api.readTransferRoute).toBe("function");
  for (const q of [
    { uid: "0" },
    { uid: "1", merchant_id: "2" },
    { uid: ["1", "2"] },
    { q: ["x", "y"] },
    { column: "account", value: "private" },
    { column: "money", value: "1.001" },
    { column: "biz_no", search_field: "money" },
    { q: "x", value: "y" },
    { status: "anything" },
    { channel_id: "01" },
    { from: "2026-02-30" },
    { from: "2026-10-26", to: "2026-10-01" },
    { from: "2020-01-01", to: "2026-10-26" },
    { unknown: "x" },
  ])
    expect(api.readTransferRoute(q, "admin", "1").error).not.toBe("");
  expect(api.readTransferRoute({ uid: "2" }, "merchant", "1").error).not.toBe(
    "",
  );
});
it("applied transfer query serialization is detached from draft mutations", () => {
  expect(typeof api.freshTransferFilters).toBe("function");
  expect(typeof api.transferFiltersQuery).toBe("function");
  const draft = api.freshTransferFilters();
  draft.search_field = "money";
  draft.q = "90071992547409.93";
  draft.merchant_id = "9007199254740993";
  const applied = { ...draft };
  draft.q = "1.00";
  const q = api.transferFiltersQuery(applied, 2, 10);
  expect(q.get("q")).toBe("90071992547409.93");
  expect(q.get("merchant_id")).toBe("9007199254740993");
  expect(q.get("page")).toBe("2");
  expect(q.get("search_field")).toBe("money");
  expect(q.has("status")).toBe(false);
});
