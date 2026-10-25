import { expect, it } from "vitest";
import {
  autoAuthorizationBody,
  readAutoAuthorization,
} from "../src/settlement-auto";
const plan = {
  id: "4",
  revision: "3",
  last_authorization_id: "8",
  merchant_ids: ["2"],
};
const channel = {
  id: "7",
  provider: "alipay",
  product: "alipay.fund.trans.toaccount.transfer",
  app_id: "local-app",
  payer_masked: "***0073",
  merchant_ids: ["2"],
};
const view = {
  id: "0",
  plan_id: "4",
  available: true,
  wallet_channels: [channel],
};
const draft = {
  valid_from: "2026-10-24T00:00:00Z",
  expires_at: "2026-10-25T00:00:00Z",
  max_item_gross: "100",
  max_run_gross: "200",
  max_day_gross: "300",
  max_day_items: "2",
};
it("reads only the original plan and explicitly admitted wallet products", () => {
  expect(readAutoAuthorization(view, plan).id).toBe("0");
  for (const bad of [
    { ...view, plan_id: "5" },
    { ...view, available: "true" },
    { ...view, wallet_channels: [{ ...channel, product: "bank" }] },
    { ...view, wallet_channels: [channel, channel] },
    { ...view, wallet_channels: [{ ...channel, merchant_ids: ["02"] }] },
  ])
    expect(() => readAutoAuthorization(bad, plan)).toThrow();
});
it("freezes exactly the eleven fields and requires acknowledgement of the actual cost boundary", () => {
  const current = readAutoAuthorization(view, plan);
  const body = autoAuthorizationBody(plan, current, draft, { "2": "7" }, true);
  expect(Object.keys(body)).toHaveLength(11);
  expect(body).toMatchObject({
    expected_revision: "3",
    expected_authorization_id: "0",
    generation_authorization_id: "8",
    cost_policy: "provider_managed_excluded",
    channels: [{ merchant_id: "2", channel_id: "7" }],
  });
  expect(() =>
    autoAuthorizationBody(plan, current, draft, { "2": "7" }, false),
  ).toThrow();
  expect(() =>
    autoAuthorizationBody(plan, current, draft, { "2": "8" }, true),
  ).toThrow();
  expect(() =>
    autoAuthorizationBody(plan, current, draft, { "2": "7", "3": "7" }, true),
  ).toThrow();
  expect(() =>
    autoAuthorizationBody(
      { ...plan, last_authorization_id: "0" },
      current,
      draft,
      { "2": "7" },
      true,
    ),
  ).toThrow();
});
it("rejects noncanonical IDs, unbounded time, limits, and counts", () => {
  const current = readAutoAuthorization(view, plan);
  for (const change of [
    { max_item_gross: "01" },
    { max_item_gross: "0" },
    { max_item_gross: "9223372036854775808" },
    { max_run_gross: "99" },
    { max_day_gross: "199" },
    { max_day_items: "101" },
    { expires_at: "2026-12-01T00:00:00Z" },
    { valid_from: "2026-10-24" },
    { expires_at: "2026-10-24T00:00:00Z" },
  ])
    expect(() =>
      autoAuthorizationBody(
        plan,
        current,
        { ...draft, ...change },
        { "2": "7" },
        true,
      ),
    ).toThrow();
});
