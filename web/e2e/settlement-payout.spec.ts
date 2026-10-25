import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const recipient = {
  state: "recorded",
  config_id: "42",
  type: "alipay",
  app_id: "",
  account_masked: "***1234",
  name_masked: "***",
};
const funding = {
  state: "reserved",
  account_id: "90",
  gross_amount: "123",
  reserved_amount: "123",
};
const request = {
  id: "12",
  merchant_id: "1000",
  status: "approved",
  amount_cents: "123",
  settlement: { gross_amount: "123", fee_amount: "3", net_amount: "120" },
  funding,
  recipient,
};
const intent = (state: string, send_state: string) => ({
  request_id: "12",
  merchant_id: "1000",
  channel_id: "1",
  transfer_id: "90",
  state,
  send_state,
  transfer: { id: "90", transfer_no: "TF90", amount: "120", status: "pending" },
  funding,
  recipient,
  created_at: "2026-10-23T00:00:00Z",
});
test("approved settlement requires channel confirmation then separate funds authorization; unknown only queries original TF", async ({
  page,
}) => {
  let payout: unknown = null,
    prepared = 0,
    executed = 0,
    queried = 0;
  await page.route("**/console/v1/**", async (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            name: "Admin",
            role: "admin",
            admin_class: "operator",
          },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: true,
          enforced: true,
          credential_epoch: 1,
          totp_required: false,
        }),
      );
    if (p.endsWith("/funds-authorizations/challenges")) {
      expect(r.postDataJSON()).toEqual({
        target: "/console/v1/admin/settlement-payouts/12/execute",
        body: {},
      });
      return route.fulfill(
        ok({
          challenge_id: "payout-challenge",
          expires_at: new Date(Date.now() + 120000).toISOString(),
          summary: {
            action: "结算出款",
            request_id: "12",
            amount: "1.20",
            recipient_masked: "***1234",
          },
        }),
      );
    }
    if (p.endsWith("/funds-authorizations/confirm"))
      return route.fulfill(
        ok({
          authorization: "payout-grant",
          expires_at: new Date(Date.now() + 120000).toISOString(),
        }),
      );
    if (p.endsWith("/settlement-payouts/12/capabilities"))
      return route.fulfill(
        ok({
          request_id: "12",
          can_prepare: !payout,
          writes_enabled: true,
          channels: [{ id: "1", type: "alipay", app_id: "" }],
          reason: "",
        }),
      );
    if (p.endsWith("/settlement-payouts/12/prepare")) {
      prepared++;
      expect(r.postDataJSON()).toEqual({ channel_id: "1" });
      expect(r.headers()["x-payment-authorization"]).toBeUndefined();
      payout = intent("unsent", "unsent");
      return route.fulfill(ok(payout));
    }
    if (p.endsWith("/settlement-payouts/12/execute")) {
      executed++;
      expect(r.postDataJSON()).toEqual({});
      expect(r.headers()["x-payment-authorization"]).toBe("payout-grant");
      payout = intent("unknown", "claimed");
      return route.fulfill(ok(payout, 202));
    }
    if (p.endsWith("/settlement-payouts/12/query")) {
      queried++;
      expect(r.headers()["x-payment-authorization"]).toBeUndefined();
      return route.fulfill(ok(payout, 202));
    }
    if (p.endsWith("/settlement-payouts/12"))
      return route.fulfill(ok({ request, payout }));
    return route.fulfill(
      ok({
        items: [
          {
            ...request,
            amount: "1.23",
            settlement_recipient: recipient,
            settlement_funding: funding,
          },
        ],
        total: 1,
      }),
    );
  });
  await page.goto("/admin/settle/list");
  await page.getByRole("button", { name: "核对结算出款", exact: true }).click();
  const panel = page.getByRole("region", { name: "结算出款", exact: true });
  await expect(panel).toContainText("核准不代表已出款");
  await panel.getByLabel("出款通道").selectOption("1");
  await panel
    .getByRole("button", { name: "准备唯一出款绑定", exact: true })
    .click();
  expect(prepared).toBe(0);
  await panel.getByRole("button", { name: "确认此操作", exact: true }).click();
  await expect(
    panel.getByRole("button", { name: "发送原结算出款", exact: true }),
  ).toBeEnabled();
  expect(prepared).toBe(1);
  await panel
    .getByRole("button", { name: "发送原结算出款", exact: true })
    .click();
  await panel
    .getByRole("button", { name: "确认发送并核对资金授权", exact: true })
    .click();
  const modal = page.getByRole("dialog", { name: "管理员资金二次授权" });
  await expect(modal).toContainText("***1234");
  expect(executed).toBe(0);
  await modal
    .getByRole("button", { name: "已核对摘要，验证资金密码", exact: true })
    .click();
  await modal
    .getByLabel("本人管理员资金密码", { exact: true })
    .fill("Test-funds-password");
  await modal
    .getByRole("button", { name: "确认资金密码", exact: true })
    .click();
  expect(executed).toBe(0);
  await modal
    .getByRole("button", { name: "执行已核对的原操作", exact: true })
    .click();
  await expect(panel).toContainText("结果待核实");
  await expect(
    panel.getByRole("button", { name: "发送原结算出款", exact: true }),
  ).toHaveCount(0);
  await panel
    .getByRole("button", { name: "只查询原出款", exact: true })
    .click();
  await expect.poll(() => queried).toBe(1);
  expect(executed).toBe(1);
  expect(prepared).toBe(1);
  expect(
    await page.evaluate(() =>
      Object.keys(localStorage).concat(Object.keys(sessionStorage)),
    ),
  ).toEqual([]);
  expect(await page.content()).not.toContain("payout-grant");
});
test("merchant reads and queries only the claimed original payout without fetching administrator capabilities", async ({
  page,
}) => {
  let queries = 0,
    caps = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/capabilities")) {
      caps++;
      return route.fulfill({ status: 403, body: "forbidden" });
    }
    if (p.endsWith("/query")) {
      queries++;
      return route.fulfill(ok(intent("unknown", "claimed"), 202));
    }
    if (p.endsWith("/settlement-payouts/12"))
      return route.fulfill(
        ok({ request, payout: intent("unknown", "claimed") }),
      );
    return route.fulfill(
      ok({
        items: [
          {
            ...request,
            amount: "1.23",
            settlement_funding: funding,
            settlement_recipient: recipient,
          },
        ],
        total: 1,
      }),
    );
  });
  await page.goto("/merchant/settle/list");
  await page.getByRole("button", { name: "核对结算出款", exact: true }).click();
  const panel = page.getByRole("region", { name: "结算出款", exact: true });
  await expect(panel).toContainText("TF90");
  await expect(panel.locator("select")).toHaveCount(0);
  await expect(
    panel.getByRole("button", { name: "发送原结算出款", exact: true }),
  ).toHaveCount(0);
  await panel
    .getByRole("button", { name: "只查询原出款", exact: true })
    .click();
  await expect.poll(() => queries).toBe(1);
  expect(caps).toBe(0);
});
