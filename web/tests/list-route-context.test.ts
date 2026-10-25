import { it, expect } from "vitest";
import { readListRouteQuery } from "../src/list-filters";
it("binds original merchant links to exact string IDs, pagination and search fields", () => {
  const result = readListRouteQuery(
    {
      uid: "9223372036854775807",
      page: "2",
      limit: "1",
      column: "out_trade_no",
      value: "original order",
    },
    "orders",
    "admin",
    "1",
  );
  expect(result.error).toBe("");
  expect(result.filters.merchant_id).toBe("9223372036854775807");
  expect(result.filters.search_field).toBe("out_trade_no");
  expect(result.search).toBe("original order");
  expect(result.page).toBe(2);
  expect(result.limit).toBe(1);
});
it("never degrades ambiguous route context into an unfiltered platform list", () => {
  for (const query of [
    { uid: ["1", "2"] },
    { uid: "01" },
    { uid: "9223372036854775808" },
    { uid: "1", merchant_id: "2" },
    { q: "a", value: "b" },
    { from: "2026-02-30" },
    { page: "0" },
    { limit: "101" },
    { status: "0" },
    { unexpected: "ignored" },
  ])
    expect(
      readListRouteQuery(query, "orders", "admin", "1").error,
      JSON.stringify(query),
    ).not.toBe("");
});
it("keeps merchant ownership and endpoint-specific filters independent of URL input", () => {
  expect(
    readListRouteQuery({ uid: "2" }, "orders", "merchant", "1").error,
  ).not.toBe("");
  expect(
    readListRouteQuery({ merchant_id: "1" }, "orders", "merchant", "1").error,
  ).toBe("");
  expect(
    readListRouteQuery({ status: "paid" }, "ledger", "admin", "1").error,
  ).not.toBe("");
  expect(
    readListRouteQuery({ include_archived: "true" }, "orders", "merchant", "1")
      .error,
  ).not.toBe("");
  expect(
    readListRouteQuery(
      { uid: "1", from: "2026-10-25", to: "2026-10-25" },
      "ledger",
      "admin",
      "1",
    ).error,
  ).toBe("");
});
