import { expect, it } from "vitest";
import { documentBoundaryDecision202 as decide } from "../src/document-boundary202";

it("starts the original document without a redirect loop", () => {
  expect(
    decide(
      "/merchant/scanpay",
      "/",
      "https://pay.example.invalid/merchant/scanpay",
      true,
    ),
  ).toEqual({ kind: "stay" });
});
it("keeps ordinary console and same-cashier query transitions in the current document", () => {
  expect(
    decide(
      "/merchant/orders",
      "/merchant/onecode",
      "https://pay.example.invalid/merchant/onecode",
      false,
    ),
  ).toEqual({ kind: "stay" });
  expect(
    decide(
      "/merchant/scanpay?tab=2#code",
      "/merchant/scanpay?tab=1",
      "https://pay.example.invalid/merchant/scanpay?tab=1",
      false,
    ),
  ).toEqual({ kind: "stay" });
});
it("refreshes policy for every native cashier entry and return", () => {
  expect(
    decide(
      "/merchant/scanpay",
      "/merchant/onecode",
      "https://pay.example.invalid/merchant/onecode",
      false,
    ),
  ).toEqual({
    kind: "assign",
    href: "https://pay.example.invalid/merchant/scanpay",
  });
  expect(
    decide(
      "/merchant/onecode",
      "/merchant/scanpay",
      "https://pay.example.invalid/merchant/scanpay",
      false,
    ),
  ).toEqual({
    kind: "assign",
    href: "https://pay.example.invalid/merchant/onecode",
  });
});
it("replaces an already popped history destination instead of adding another entry", () => {
  expect(
    decide(
      "/merchant/onecode",
      "/merchant/scanpay",
      "https://pay.example.invalid/merchant/onecode",
      false,
    ),
  ).toEqual({
    kind: "replace",
    href: "https://pay.example.invalid/merchant/onecode",
  });
});
it("also isolates the public CAPTCHA document from console and native cashier policies", () => {
  for (const from of ["/merchant/orders", "/merchant/scanpay"])
    expect(
      decide("/test/?ok=1", from, "https://pay.example.invalid" + from, false),
    ).toEqual({
      kind: "assign",
      href: "https://pay.example.invalid/test/?ok=1",
    });
  expect(
    decide(
      "/merchant/dashboard",
      "/test/",
      "https://pay.example.invalid/test/",
      false,
    ),
  ).toEqual({
    kind: "assign",
    href: "https://pay.example.invalid/merchant/dashboard",
  });
});
it("never creates an external navigation or preserves SDK exceptions via malformed targets", () => {
  for (const target of [
    "//other.example.invalid",
    "https://other.example.invalid/merchant/scanpay",
    "/\\other.example.invalid",
    "/merchant/scanpay\n",
  ])
    expect(
      decide(
        target,
        "/merchant/scanpay",
        "https://pay.example.invalid/merchant/scanpay",
        false,
      ),
    ).toEqual({ kind: "block" });
});
