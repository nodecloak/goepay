import { expect, it } from "vitest";
import {
  isAdminFundsAction,
  needsFundsAuthorization,
} from "../src/funds-authorization";
import { resolvePage, viewKind } from "../src/menu";
import { planBody, authorizationBody } from "../src/settlement-plans";

it("authorizes only the exact positive plan authorization POST, never plan edits, scans or emergency revokes", () => {
  expect(
    needsFundsAuthorization(
      "/console/v1/admin/settlement-plans/12/authorizations",
      false,
    ),
  ).toBe(true);
  expect(
    needsFundsAuthorization("/console/v1/admin/orders/abc/refund", false),
  ).toBe(false);
  expect(
    isAdminFundsAction("/settlement-plans/12/authorizations", "POST"),
  ).toBe(true);
  for (const path of [
    "/settlement-plans",
    "/settlement-plans/12",
    "/settlement-plans/12/runs",
    "/settlement-plan-authorizations/12/revoke",
    "/settlement-plans/0/authorizations",
    "/settlement-plans/01/authorizations",
    "/settlement-plans/12/authorizations?x=1",
    "/settlement-plans/12/authorizations/",
    "/settlement-plans/-1/authorizations",
  ])
    expect(isAdminFundsAction(path, "POST")).toBe(false);
  expect(
    isAdminFundsAction("/settlement-plans/12/authorizations", "PATCH"),
  ).toBe(false);
});

it("keeps a bounded unique merchant scope and explicit schedule without auto-payment fields", () => {
  const body = planBody(
    {
      name: "每日申请",
      frequency: "day",
      interval: "1",
      anchor_at: "2026-10-24T00:00:00+08:00",
      execute_hour: "9",
      execute_minute: "30",
      execute_second: "0",
      merchant_ids: "2, 3",
    },
    "7",
  );
  expect(body).toEqual({
    name: "每日申请",
    mode: "generate_only",
    schedule: {
      frequency: "day",
      interval: "1",
      anchor_at: "2026-10-24T00:00:00+08:00",
      execute_hour: 9,
      execute_minute: 30,
      execute_second: 0,
    },
    merchant_ids: ["2", "3"],
    expected_revision: "7",
  });
  expect(() =>
    planBody({
      name: "x",
      frequency: "day",
      interval: "1",
      anchor_at: "2026-10-24T00:00:00Z",
      execute_hour: "9",
      execute_minute: "0",
      execute_second: "0",
      merchant_ids: "2,2",
    }),
  ).toThrow();
});

it("freezes exact authorization caps as cents strings and refuses impossible windows", () => {
  const b = authorizationBody("4", "0", {
    valid_from: "2026-10-24T00:00:00Z",
    expires_at: "2026-10-25T00:00:00Z",
    max_item_gross: "100",
    max_run_gross: "200",
    max_day_gross: "300",
    max_day_items: "2",
  });
  expect(b.expected_revision).toBe("4");
  expect(b.max_item_gross).toBe("100");
  expect(() =>
    authorizationBody("4", "0", {
      ...b,
      expires_at: "2026-10-23T00:00:00Z",
    } as any),
  ).toThrow();
});

it("uses dedicated super-only planning and merchant opt-in pages", () => {
  expect(viewKind("admin", "/settle/plans")).toBe("settlement-plans");
  expect(resolvePage("admin", "/settle/plans")?.roles).toEqual(["R_SUPER"]);
  expect(viewKind("merchant", "/account/settlement-schedule")).toBe(
    "settlement-schedule",
  );
});
