import { it, expect } from "vitest";
import {
  systemLogRouteQuery,
  systemLogMerchantHref,
  systemLogIPHref,
} from "../src/system-log";
it("original system-log routes preserve UID zero and int64 strings and reject ambiguous claims", () => {
  expect(systemLogRouteQuery({ uid: "9007199254740993" })).toEqual({
    column: "uid",
    value: "9007199254740993",
  });
  expect(systemLogRouteQuery({ column: "uid", value: "0" })).toEqual({
    column: "uid",
    value: "0",
  });
  expect(systemLogRouteQuery({ column: "ip", value: " 192.0.2.1 " })).toEqual({
    column: "ip",
    value: " 192.0.2.1 ",
  });
  for (const q of [
    { uid: "1", column: "uid", value: "1" },
    { uid: ["1", "2"] },
    { uid: "01" },
    { uid: "9223372036854775808" },
    { uid: 1 },
    { column: "other", value: "x" },
  ])
    expect(systemLogRouteQuery(q).error).toBeTruthy();
});
it("merchant subject links use explicit ownership rather than original PHP numeric thresholds or claimed values", () => {
  expect(systemLogMerchantHref({ uid: "1", subject_kind: "merchant" })).toBe(
    "/admin/merchant/merchant-list?uid=1",
  );
  for (const row of [
    { uid: "0", subject_kind: "admin" },
    { uid: "1", subject_kind: "unknown" },
    { uid: null },
    { uid: "../1", subject_kind: "merchant" },
    { uid: 9007199254740992, subject_kind: "merchant" },
  ])
    expect(systemLogMerchantHref(row)).toBeUndefined();
});
it("original optional IP lookup links have a fixed public destination and reject non-address URL text", () => {
  expect(systemLogIPHref("192.0.2.1")).toBe(
    "https://m.ip138.com/iplookup.asp?ip=192.0.2.1",
  );
  expect(systemLogIPHref("2001:db8::1")).toContain("ip=2001%3Adb8%3A%3A1");
  for (const ip of [
    "unknown",
    "https://example.test",
    "192.0.2.1&token=x",
    "999.1.1.1",
    "192.0.2.01",
    "",
  ])
    expect(systemLogIPHref(ip)).toBeUndefined();
});
