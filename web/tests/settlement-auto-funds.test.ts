import { it, expect } from "vitest";
import {
  isAdminFundsAction,
  needsFundsAuthorization,
} from "../src/funds-authorization";
it("automatic payout authority always needs its own funds confirmation even when global enforcement is off", () => {
  expect(
    isAdminFundsAction("/settlement-plans/7/auto-authorizations", "POST"),
  ).toBe(true);
  expect(
    needsFundsAuthorization(
      "/console/v1/admin/settlement-plans/7/auto-authorizations",
      false,
    ),
  ).toBe(true);
  for (const path of [
    "/settlement-plans/7/auto-authorizations?x=1",
    "/settlement-plans/7/auto-authorizations/2/revoke",
    "/settlement-plans/0/auto-authorizations",
  ])
    expect(isAdminFundsAction(path, "POST")).toBe(false);
  expect(
    isAdminFundsAction("/settlement-plans/7/auto-authorizations", "GET"),
  ).toBe(false);
});
