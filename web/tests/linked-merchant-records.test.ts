import { it, expect } from "vitest";
import * as list from "../src/list-filters";
const read = (
  raw: Record<string, unknown>,
  area: "admin" | "merchant" = "admin",
  self = "1",
  search = false,
) => {
  const fn = list.readLinkedMerchantRoute;
  expect(typeof fn).toBe("function");
  return fn(raw, area, self, search);
};
it("original linked records retain int64 UID and pagination without creating a funds draft", () => {
  expect(read({ uid: "9223372036854775807", page: "2", limit: "1" })).toEqual({
    merchantID: "9223372036854775807",
    page: 2,
    limit: 1,
    search: "",
    error: "",
  });
  expect(read({})).toEqual({
    merchantID: "",
    page: 1,
    limit: 20,
    search: "",
    error: "",
  });
});
it("duplicate conflicting unknown and noncanonical linked filters fail closed", () => {
  for (const raw of [
    { uid: ["1", "2"] },
    { uid: "1", merchant_id: "2" },
    { uid: "01" },
    { uid: "0" },
    { uid: "9223372036854775808" },
    { uid: "1\n" },
    { page: "01" },
    { page: "100001" },
    { limit: "101" },
    { limit: "" },
    { other: "1" },
    { q: "ignored" },
  ])
    expect(read(raw).error).not.toBe("");
});
it("only the authenticated merchant can accept an original UID link", () => {
  expect(read({ uid: "2" }, "merchant", "1").error).not.toBe("");
  expect(read({ uid: "1" }, "merchant", "1").merchantID).toBe("1");
  expect(read({ uid: "1" }, "merchant", "").error).not.toBe("");
});
it("only transfer links accept the existing bounded payment-number search", () => {
  expect(
    read({ uid: "1", q: "original-transfer" }, "admin", "1", true).search,
  ).toBe("original-transfer");
  for (const q of ["a\n", "x".repeat(257)])
    expect(read({ q }, "admin", "1", true).error).not.toBe("");
  expect(read({ uid: "1", merchant_id: "1" }).error).toBe("");
});
