import { expect, it } from "vitest";
import { viewKind } from "../src/menu";
import { isAdminFundsAction } from "../src/funds-authorization";

it("routes each original sharing menu to its dedicated page and authorizes submit", () => {
  for (const area of ["admin", "merchant"] as const)
    for (const path of ["/profitsharing/receiver", "/profitsharing/order"])
      expect(viewKind(area, path)).toBe("channel-sharing");
  expect(isAdminFundsAction("/channel-sharing/orders", "POST")).toBe(true);
  expect(isAdminFundsAction("/channel-sharing/returns", "POST")).toBe(true);
  expect(isAdminFundsAction("/channel-sharing/finishes", "POST")).toBe(true);
  expect(isAdminFundsAction("/channel-sharing/orders/PS1/query", "POST")).toBe(
    false,
  );
});
