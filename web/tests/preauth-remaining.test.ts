import { it, expect } from "vitest";
import { canReleaseAuthorization } from "../src/authorization-release";
import {
  isAdminFundsAction,
  needsFundsAuthorization,
} from "../src/funds-authorization";
it("remaining release has its own mandatory confirmation and cannot enter the legacy sender", () => {
  expect(
    isAdminFundsAction("/orders/T75/release-remaining-authorization", "POST"),
  ).toBe(true);
  expect(
    needsFundsAuthorization(
      "/console/v1/admin/orders/T75/release-remaining-authorization",
      false,
    ),
  ).toBe(true);
  expect(
    canReleaseAuthorization(true, "pending", {
      request_no: "UA75",
      sent: false,
      state: "reversal_pending",
      source_revision: "1",
    }),
  ).toBe(false);
  expect(
    isAdminFundsAction(
      "/orders/T75/release-remaining-authorization?x=1",
      "POST",
    ),
  ).toBe(false);
});
