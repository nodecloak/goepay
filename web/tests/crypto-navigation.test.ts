import { expect, it } from "vitest";
import { navigationFor, pageAuthorizedFor, viewKind } from "../src/menu";

it("opens a merchant USDT funds page and reserves platform controls for super administrators", () => {
  expect(viewKind("merchant", "/account/crypto")).toBe("crypto");
  expect(viewKind("admin", "/system/crypto")).toBe("crypto");
  expect(
    pageAuthorizedFor(
      "merchant",
      "/account/crypto",
      "merchant",
      undefined,
      undefined,
    ),
  ).toBe(true);
  expect(
    pageAuthorizedFor(
      "admin",
      "/system/crypto",
      "admin",
      "operator",
      undefined,
    ),
  ).toBe(false);
  expect(
    pageAuthorizedFor("admin", "/system/crypto", "admin", "super", undefined),
  ).toBe(true);
  expect(
    JSON.stringify(navigationFor("merchant", "merchant", undefined, undefined)),
  ).toContain("USDT");
});
