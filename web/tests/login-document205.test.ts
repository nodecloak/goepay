import { expect, it } from "vitest";
import {
  merchantLoginEntry205,
  merchantLoginReturn205,
} from "../src/login-document205";

it("preserves a known merchant destination and ordinary filter or anchor without storage", () => {
  for (const route of [
    "/merchant/dashboard",
    "/merchant/trade/order?status=0#unpaid",
    "/merchant/scanpay",
    "/merchant/account/api-info",
  ]) {
    expect(merchantLoginReturn205(route)).toBe(route);
    expect(merchantLoginEntry205(route)).toBe(
      "/merchant/login?return=" + encodeURIComponent(route),
    );
  }
});
it("rejects external, recursive, unknown, malformed and bearer-bearing destinations", () => {
  for (const route of [
    undefined,
    ["/merchant/dashboard"],
    "//elsewhere.invalid/merchant/dashboard",
    "https://elsewhere.invalid/merchant/dashboard",
    "/admin/dashboard",
    "/merchant/login",
    "/merchant/login/?return=/merchant/dashboard",
    "/merchant/../admin/dashboard",
    "/merchant/%2e%2e/admin/dashboard",
    "/merchant/not-a-page",
    "/merchant/dashboard\n",
    "/merchant/\\elsewhere.invalid",
    "/merchant/dashboard?promotion_token=private",
    "/merchant/dashboard?api_key=private",
    "/merchant/dashboard?x=1&x=2",
    "/merchant/dashboard?x=%0a",
    "/merchant/dashboard#access_token=private",
  ])
    expect(merchantLoginReturn205(route)).toBe("/merchant/dashboard");
});
