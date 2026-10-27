import { it, expect } from "vitest";
import { welcomeOnce } from "../src/announcement-layout";
it("scopes the daily preference to the exact own merchant and server UTC8 date", () => {
  const entries = new Map<string, string>(),
    storage = {
      getItem: (k: string) => entries.get(k) || null,
      setItem: (k: string, v: string) => {
        entries.set(k, v);
      },
    };
  const a = {
    merchantId: "9007199254740993",
    dataMerchantId: "9007199254740993",
    businessDate: "2026-10-26",
    content: "<p>欢迎</p>",
    ownSession: true,
  };
  expect(welcomeOnce(a, storage)).toBe(true);
  expect(welcomeOnce(a, storage)).toBe(false);
  expect(welcomeOnce({ ...a, businessDate: "2026-10-27" }, storage)).toBe(true);
  expect(welcomeOnce({ ...a, merchantId: "9007199254740995" }, storage)).toBe(
    false,
  );
  expect(
    welcomeOnce(
      {
        ...a,
        merchantId: "9007199254740995",
        dataMerchantId: "9007199254740995",
      },
      storage,
    ),
  ).toBe(true);
  for (const value of entries.values())
    expect(value).toMatch(/^2026-10-2[67]$/);
});
it("does not consume a day for delegated, blank, invalid or mismatched context", () => {
  let writes = 0;
  const storage = {
      getItem: () => null,
      setItem: () => {
        writes++;
      },
    },
    a = {
      merchantId: "35",
      dataMerchantId: "35",
      businessDate: "2026-10-28",
      content: "<p>欢迎</p>",
      ownSession: true,
    };
  for (const v of [
    { ...a, ownSession: false },
    { ...a, content: "" },
    { ...a, dataMerchantId: "36" },
    { ...a, businessDate: "2026-02-30" },
    { ...a, merchantId: "035" },
    { ...a, merchantId: "9223372036854775808" },
  ])
    expect(welcomeOnce(v, storage)).toBe(false);
  expect(writes).toBe(0);
  expect(welcomeOnce(a, storage)).toBe(true);
});
it("keeps a bounded in-memory preference when browser storage is blocked", () => {
  const storage = {
      getItem: () => {
        throw Error("blocked");
      },
      setItem: () => {
        throw Error("blocked");
      },
    },
    a = {
      merchantId: "890173",
      dataMerchantId: "890173",
      businessDate: "2026-10-29",
      content: "欢迎",
      ownSession: true,
    };
  expect(welcomeOnce(a, storage)).toBe(true);
  expect(welcomeOnce(a, storage)).toBe(false);
});
