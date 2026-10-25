import { expect, it } from "vitest";
import {
  isAdminFundsAction,
  needsFundsAuthorization,
} from "../src/funds-authorization";

it("requires separate funds confirmation for the exact channel restoration even when global enforcement is off", () => {
  expect(isAdminFundsAction("/channel-stops/7/resume", "POST")).toBe(true);
  expect(
    needsFundsAuthorization("/console/v1/admin/channel-stops/7/resume", false),
  ).toBe(true);
  for (const p of [
    "/channel-stops/7",
    "/channel-stops/07/resume",
    "/channel-stops/7/resume?enabled=true",
  ])
    expect(isAdminFundsAction(p, "POST")).toBe(false);
  expect(isAdminFundsAction("/channel-stops/7/resume", "GET")).toBe(false);
});
