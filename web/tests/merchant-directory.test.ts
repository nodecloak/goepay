import { it, expect } from "vitest";
import {
  readMerchantDirectoryQuery,
  merchantDirectoryError,
  newMerchantDirectory,
} from "../src/merchant-directory";

it("preserves validated route pagination rather than silently loading another page", () => {
  const result = readMerchantDirectoryQuery({
    uid: "9223372036854775807",
    page: "3",
    limit: "1",
    order: "id_desc",
  });
  expect(result.error).toBe("");
  expect(result.search).toBe("9223372036854775807");
  expect(result.draft.column).toBe("uid");
  expect(result.page).toBe(3);
  expect(result.limit).toBe(1);
  expect(result.draft.order).toBe("");
});
it("rejects ambiguous or out-of-range route pagination and merchant IDs", () => {
  for (const q of [
    { page: "0" },
    { page: "100001" },
    { page: "01" },
    { limit: "101" },
    { limit: "NaN" },
    { uid: "9223372036854775808" },
    { uid: ["1", "2"] },
    { uid: "1", column: "name" },
    { pay: "2", dstatus: "pay_0" },
  ])
    expect(readMerchantDirectoryQuery(q).error, JSON.stringify(q)).not.toBe("");
});
it("uses the server UTF-8 byte budget for multilingual search", () => {
  expect(merchantDirectoryError(newMerchantDirectory(), "中".repeat(85))).toBe(
    "",
  );
  expect(
    merchantDirectoryError(newMerchantDirectory(), "中".repeat(86)),
  ).not.toBe("");
});
it("rejects calendar normalization, year zero and reversed registration ranges", () => {
  for (const q of [
    { from: "2026-02-30" },
    { from: "0000-01-01" },
    { from: "2026-10-26", to: "2026-10-25" },
  ])
    expect(readMerchantDirectoryQuery(q).error).not.toBe("");
  expect(
    readMerchantDirectoryQuery({
      from: "2024-02-29",
      to: "2024-02-29",
      gid: "0",
    }).error,
  ).toBe("");
});
it("keeps original QQ and website search separate from approved payment domains", () => {
  for (const [column, q] of [
    ["qq", "1000011901"],
    ["url", "shop.example.invalid"],
    ["domain", "approved.example.invalid"],
  ]) {
    const parsed = readMerchantDirectoryQuery({ column, q });
    expect(parsed.error).toBe("");
    expect(parsed.draft.column).toBe(column);
    expect(parsed.search).toBe(q);
  }
});
