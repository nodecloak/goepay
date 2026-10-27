import { it, expect } from "vitest";
import {
  freshSettlementFilters,
  readSettlementRoute,
  settlementFilterQuery,
  readSettlementStatistics,
} from "../src/settlement-filters";
it("original settlement aliases preserve zero status and full merchant identifiers", () => {
  const r = readSettlementRoute(
    {
      uid: "9223372036854775807",
      type: "4",
      dstatus: "0",
      starttime: "2026-10-01",
      endtime: "2026-10-26",
      page: "2",
      limit: "10",
    },
    "admin",
    "1",
  );
  expect(r.error).toBe("");
  expect(r.page).toBe(2);
  expect(
    settlementFilterQuery(r.filters, "admin", r.page, r.limit).toString(),
  ).toBe(
    "merchant_id=9223372036854775807&type=bank&dstatus=0&from=2026-10-01&to=2026-10-26&page=2&limit=10",
  );
});
it("invalid aliases, unknown fields and dates never broaden a settlement query", () => {
  for (const r of [
    { uid: ["1", "2"] },
    { uid: "1", merchant_id: "1" },
    { uid: "01" },
    { type: "5" },
    { dstatus: "00" },
    { starttime: "2026-02-30", endtime: "2026-03-01" },
    { from: "2026-01-01" },
    { unknown: "1" },
    { from: "2020-01-01", to: "2026-01-01" },
  ])
    expect(readSettlementRoute(r, "admin", "1").error).not.toBe("");
  expect(readSettlementRoute({ uid: "2" }, "merchant", "1").error).not.toBe("");
  expect(readSettlementRoute({ uid: "1" }, "merchant", "1").error).toBe("");
  expect(
    settlementFilterQuery(
      { ...freshSettlementFilters(), merchant_id: "1" },
      "merchant",
    ).has("merchant_id"),
  ).toBe(false);
});
it("settlement statistics retain exact counts and reject inconsistent or untrusted values", () => {
  const row = {
    total_count: "9007199254740993",
    waiting_count: "9007199254740990",
    completed_count: "1",
    processing_count: "1",
    failed_count: "1",
    cancelled_count: "0",
    returning_count: "0",
    returned_count: "0",
    total_money: "90071992547409.93",
  };
  expect(readSettlementStatistics(row).total_count).toBe(row.total_count);
  for (const bad of [
    { ...row, total_count: 9007199254740992 },
    { ...row, waiting_count: "2" },
    { ...row, total_money: "1e12" },
    { ...row, total_money: "1.001" },
  ])
    expect(() => readSettlementStatistics(bad)).toThrow();
});
