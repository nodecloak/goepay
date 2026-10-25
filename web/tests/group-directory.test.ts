import { it, expect } from "vitest";
import { groupDirectoryRates, groupUsersHref } from "../src/group-directory";
it("shows merchant retained rates and inheritance rather than mislabelling the platform fee", () => {
  const summary = groupDirectoryRates({
    rate_bps: "38",
    info: { alipay: { rate: "99.50" }, wxpay: { rate: "0" } },
  });
  expect(summary.error).toBe("");
  expect(summary.lines).toContainEqual({ label: "统一分成", value: "99.62%" });
  expect(summary.lines).toContainEqual({ label: "支付宝", value: "99.50%" });
  expect(summary.lines).toContainEqual({
    label: "微信支付",
    value: "继承统一分成99.62%",
  });
  expect(groupDirectoryRates({ rate_bps: "0" }).lines).toContainEqual({
    label: "统一分成",
    value: "100.00%",
  });
  expect(
    groupDirectoryRates({
      rate_bps: "38",
      info: { alipay: { rate: "99", channel: 1 } },
    }).error,
  ).not.toBe("");
  expect(groupDirectoryRates({ rate_bps: "38\n" }).error).not.toBe("");
});
it("retains the original zero and full int64 GIDs without navigating to a rounded or injected group", () => {
  for (const id of ["0", "9007199254740993", "9223372036854775807"])
    expect(groupUsersHref(id)).toBe("/admin/merchant/merchant-list?gid=" + id);
  for (const id of [
    "01",
    "-1",
    "1.0",
    "1\n",
    "9223372036854775808",
    "x&gid=1",
    ["1"],
    null,
    9007199254740992,
  ])
    expect(groupUsersHref(id)).toBeUndefined();
});
