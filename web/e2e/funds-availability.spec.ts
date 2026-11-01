import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({
    code: status === 200 ? 0 : -1,
    data,
    message: status === 200 ? "" : "资金政策读取失败",
  }),
});
test("malformed wallet funds policy cannot be assumed T0 or authorize a transfer", async ({
  page,
}) => {
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/capabilities"))
      return route.fulfill(
        ok({
          writes_enabled: true,
          platform_enabled: true,
          merchant_enabled: true,
          channels: [{ id: "1", types: ["alipay"] }],
          available_money: "10.00",
        }),
      );
    if (r.method() === "POST") writes++;
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/transfer");
  await page.getByRole("button", { name: "申请付款", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "转账付款" });
  await expect(modal).toContainText("资金政策或余额信息不完整");
  await expect(modal.getByRole("button", { name: "立即转账" })).toBeDisabled();
  await expect(modal).not.toContainText("T+0");
  await expect(modal).not.toContainText("0.00");
  expect(writes).toBe(0);
});
test("merchant settlement reads and displays T1 available versus ledger snapshot, and failed read blocks writes", async ({
  page,
}) => {
  let fail = false,
    writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/settlement-recipient/allowed-types"))
      return route.fulfill(
        ok({ settlement: ["alipay", "wxpay", "bank"], can_write: true }),
      );
    if (p.endsWith("/settlement-recipient"))
      return route.fulfill(
        ok({
          ...(r.method() === "GET" ? { can_write: true } : {}),
          state: "configured",
          config_id: "42",
          type: "alipay",
          app_id: "",
          account_masked: "***1234",
          name_masked: "***",
        }),
      );
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/settlement-policy"))
      return route.fulfill(
        ok({
          can_apply: true,
          external_payout: false,
          terms: {
            gross_amount: "1",
            fee_amount: "0",
            net_amount: "1",
            retained_cents: "0",
          },
        }),
      );
    if (p.endsWith("/funds-availability"))
      return route.fulfill(
        fail
          ? ok(null, 503)
          : ok({
              settle_type: 1,
              available_money: "9.90",
              ledger_available_money: "10.90",
              today_blocked_money: "1.00",
            }),
      );
    if (r.method() === "POST") {
      writes++;
      const body = r.postDataJSON();
      expect(body).toMatchObject({
        amount: "0.01",
        recipient_config_id: "42",
        expected_fee_cents: "0",
        expected_net_cents: "1",
      });
      return route.fulfill(
        ok({
          id: "1",
          kind: "settlements",
          merchant_id: "1000",
          external_no: body.external_no,
          status: "pending",
          amount: "0.01",
          amount_cents: "1",
          external_payout: false,
          created_at: "2026-10-30T00:00:00Z",
          settlement: {
            gross_amount: "1",
            fee_amount: "0",
            net_amount: "1",
            retained_cents: "0",
          },
          settlement_recipient: {
            state: "recorded",
            config_id: "42",
            type: "alipay",
            app_id: "",
            account_masked: "***1234",
            name_masked: "***",
          },
          settlement_funding: {
            state: "reserved",
            account_id: "7",
            gross_amount: "1",
            reserved_amount: "1",
          },
        }),
      );
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/settle/apply");
  const modal = page.getByRole("region", { name: "申请提现", exact: true });
  await expect(modal).toContainText("T+1");
  await expect(modal).toContainText("9.90");
  await expect(modal).toContainText("10.90");
  await expect(modal).toContainText("当日实际受限金额");
  await modal.getByLabel("金额（元）").fill("0.01");
  await modal.getByRole("button", { name: "确认提交", exact: true }).click();
  await modal
    .getByRole("button", { name: "按已核对费用提交", exact: true })
    .click();
  await expect(page.locator(".alert.success")).toContainText("申请已提交");
  expect(writes).toBe(1);
  fail = true;
  await page.getByRole("button", { name: "申请结算", exact: true }).click();
  await expect(modal).toContainText("资金政策读取失败");
  await expect(
    modal.getByRole("button", { name: "确认提交", exact: true }),
  ).toBeDisabled();
  await expect(modal).not.toContainText("0.00");
  expect(writes).toBe(1);
});
test("admin explicitly queries source merchant funds and late tenant response cannot authorize another merchant", async ({
  page,
}) => {
  let releaseFirst: () => void = () => {},
    writes = 0;
  const first = new Promise<void>((resolve) => (releaseFirst = resolve));
  let requestedFirst: () => void = () => {};
  const firstRequested = new Promise<void>(
    (resolve) => (requestedFirst = resolve),
  );
  await page.route("**/console/v1/**", async (route) => {
    const r = route.request(),
      url = new URL(r.url()),
      p = url.pathname;
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/funds-availability")) {
      const id = url.searchParams.get("merchant_id");
      if (id === "1000") {
        requestedFirst();
        await first;
      }
      return route
        .fulfill(
          ok({
            settle_type: id === "1000" ? 1 : 0,
            available_money: id === "1000" ? "99.00" : "2.00",
            ledger_available_money: id === "1000" ? "100.00" : "2.00",
            today_blocked_money: id === "1000" ? "1.00" : "0.00",
          }),
        )
        .catch(() => {});
    }
    if (r.method() === "POST") {
      writes++;
      const body = r.postDataJSON();
      expect(body).toMatchObject({
        merchant_id: "1001",
        recipient_id: "1002",
        amount: "0.01",
      });
      return route.fulfill(
        ok({
          id: "1",
          kind: "internal-transfers",
          merchant_id: "1001",
          recipient_id: "1002",
          external_no: body.external_no,
          status: "pending",
          amount: "0.01",
          amount_cents: "1",
          external_payout: false,
          created_at: "2026-10-30T00:00:00Z",
        }),
      );
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/admin/merchant/internal-transfer");
  await page.getByRole("button", { name: "发起站内转账", exact: true }).click();
  const modal = page.getByRole("dialog");
  await modal.getByLabel("付款商户 ID").fill("1000");
  await expect(
    modal.getByRole("button", { name: "确认提交", exact: true }),
  ).toBeDisabled();
  await modal
    .getByRole("button", { name: "查询可出账金额", exact: true })
    .click();
  await firstRequested;
  await modal.getByLabel("付款商户 ID").fill("1001");
  releaseFirst();
  await expect(modal).not.toContainText("99.00");
  await expect(
    modal.getByRole("button", { name: "确认提交", exact: true }),
  ).toBeDisabled();
  await modal
    .getByRole("button", { name: "查询可出账金额", exact: true })
    .click();
  await expect(modal).toContainText("2.00");
  await expect(modal).toContainText("T+0");
  await modal.getByLabel("收款商户 ID").fill("1002");
  await modal.getByLabel("金额（元）").fill("0.01");
  await modal.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(page.locator(".alert.success")).toContainText("申请已提交");
  expect(writes).toBe(1);
});
