import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const scenario of [
  {
    name: "auto-only settlement mode",
    mode: "1",
    source: "group",
    automatic: true,
    reason: "当前结算开关不允许手动申请结算",
  },
  {
    name: "merchant settlement permission closed",
    mode: "0",
    source: "merchant_settle",
    automatic: false,
    reason: "当前商户的结算权限已关闭",
  },
])
  test(`${scenario.name} gives a business reason and never opens fee confirmation`, async ({
    page,
  }) => {
    let writes = 0;
    await page.route("**/console/v1/**", (route) => {
      const p = new URL(route.request().url()).pathname;
      if (route.request().method() !== "GET") {
        writes++;
        return route.fulfill(ok({}));
      }
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "1", name: "Local", role: "merchant" },
            csrf_token: "csrf",
          }),
        );
      if (p.endsWith("/settlement-recipient/allowed-types"))
        return route.fulfill(
          ok({ settlement: ["alipay", "wxpay", "bank"], can_write: true }),
        );
      if (p.endsWith("/settlement-recipient"))
        return route.fulfill(
          ok({
            ...(route.request().method() === "GET" ? { can_write: true } : {}),
            state: "configured",
            config_id: "42",
            type: "alipay",
            app_id: "",
            account_masked: "***1234",
            name_masked: "***",
          }),
        );
      if (p.endsWith("/funds-availability"))
        return route.fulfill(
          ok({
            available_money: "10.00",
            ledger_available_money: "10.00",
            today_blocked_money: "0.00",
            settle_type: 0,
          }),
        );
      if (p.endsWith("/settlement-policy"))
        return route.fulfill(
          ok({
            can_apply: false,
            external_payout: false,
            settlement_access: {
              mode: scenario.mode,
              manual_allowed: false,
              automatic_allowed: scenario.automatic,
              source: scenario.source,
              group_id: "12",
            },
          }),
        );
      return route.fulfill(ok({ items: [], total: 0 }));
    });
    await page.goto("/merchant/settle/apply");
    const dialog = page.getByRole("region", { name: "申请提现", exact: true });
    await dialog.getByLabel("金额（元）", { exact: true }).fill("1.00");
    await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
    await expect(dialog).toContainText(scenario.reason);
    await expect(
      dialog.getByRole("button", { name: "按已核对费用提交", exact: true }),
    ).toHaveCount(0);
    expect(writes).toBe(0);
  });
test("settlement fee confirmation binds amount and never treats approval as payout", async ({
  page,
}) => {
  let posts = 0,
    quotes = 0;
  const quoteReads: {
    path: string;
    method: string;
    amount: string | null;
    merchantID: string | null;
  }[] = [];
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url()),
      p = url.pathname;
    if (p.endsWith("/settlement-recipient/allowed-types"))
      return route.fulfill(
        ok({ settlement: ["alipay", "wxpay", "bank"], can_write: true }),
      );
    if (p.endsWith("/settlement-recipient"))
      return route.fulfill(
        ok({
          ...(route.request().method() === "GET" ? { can_write: true } : {}),
          state: "configured",
          config_id: "42",
          type: "alipay",
          app_id: "",
          account_masked: "***1234",
          name_masked: "***",
        }),
      );
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Merchant", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/funds-availability"))
      return route.fulfill(
        ok({
          available_money: "10.00",
          ledger_available_money: "10.00",
          today_blocked_money: "0.00",
          settle_type: 0,
        }),
      );
    if (p.endsWith("/settlement-policy")) {
      quotes++;
      quoteReads.push({
        path: p,
        method: route.request().method(),
        amount: url.searchParams.get("amount"),
        merchantID: url.searchParams.get("merchant_id"),
      });
      return route.fulfill(
        ok({
          can_apply: true,
          terms: {
            gross_amount: "123",
            fee_amount: "3",
            net_amount: "120",
            retained_cents: "0",
          },
          external_payout: false,
        }),
      );
    }
    if (route.request().method() === "POST") {
      posts++;
      expect(route.request().postDataJSON().amount).toBe("1.23");
      expect(route.request().postDataJSON().expected_fee_cents).toBe("3");
      expect(route.request().postDataJSON().expected_net_cents).toBe("120");
      expect(route.request().postDataJSON().recipient_config_id).toBe("42");
      return route.fulfill({
        ...ok({
          id: "212",
          kind: "settlements",
          merchant_id: "1",
          external_no: route.request().postDataJSON().external_no,
          status: "pending",
          amount: "1.23",
          amount_cents: "123",
          external_payout: false,
          created_at: "2026-10-30T00:00:00Z",
          settlement: {
            gross_amount: "123",
            fee_amount: "3",
            net_amount: "120",
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
            gross_amount: "123",
            reserved_amount: "123",
          },
        }),
        status: 201,
      });
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/settle/apply");
  await page.getByLabel("金额（元）", { exact: true }).fill("1.23");
  await expect(page.locator(".settle-live")).toContainText("实际到账：1.20 元");
  const beforeFirstConfirmation = quotes;
  await page.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(
    page.getByText("预计手续费：0.03 元", { exact: true }),
  ).toBeVisible();
  expect(posts).toBe(0);
  expect(quotes).toBe(beforeFirstConfirmation + 1);
  expect(quoteReads.at(-1)).toEqual({
    path: "/console/v1/merchant/workflows/settlement-policy",
    method: "GET",
    amount: "1.23",
    merchantID: null,
  });
  await page.getByLabel("金额（元）", { exact: true }).fill("1.24");
  await expect(
    page.getByRole("button", { name: "按已核对费用提交", exact: true }),
  ).toHaveCount(0);
  await page.getByLabel("金额（元）", { exact: true }).fill("1.23");
  await expect(page.locator(".settle-live")).toContainText("实际到账：1.20 元");
  const beforeUpdatedConfirmation = quotes;
  await page.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "按已核对费用提交", exact: true }),
  ).toBeVisible();
  expect(quotes).toBe(beforeUpdatedConfirmation + 1);
  expect(quoteReads.at(-1)).toEqual({
    path: "/console/v1/merchant/workflows/settlement-policy",
    method: "GET",
    amount: "1.23",
    merchantID: null,
  });
  await page
    .getByRole("button", { name: "按已核对费用提交", exact: true })
    .click();
  const notice = page.getByRole("status").filter({ hasText: "申请已提交" });
  await expect(notice).toContainText("已提交并登记");
  await expect(notice).toContainText("根据原记录状态核实处理结果");
  expect(posts).toBe(1);
  expect(quotes).toBe(beforeUpdatedConfirmation + 1);
});

test("failed or refused settlement quote never submits and fee conflict requires another confirmation", async ({
  page,
}) => {
  let mode = "failure",
    posts = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/settlement-recipient/allowed-types"))
      return route.fulfill(
        ok({ settlement: ["alipay", "wxpay", "bank"], can_write: true }),
      );
    if (p.endsWith("/settlement-recipient"))
      return route.fulfill(
        ok({
          ...(route.request().method() === "GET" ? { can_write: true } : {}),
          state: "configured",
          config_id: "42",
          type: "alipay",
          app_id: "",
          account_masked: "***1234",
          name_masked: "***",
        }),
      );
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/funds-availability"))
      return route.fulfill(
        ok({
          available_money: "10.00",
          ledger_available_money: "10.00",
          today_blocked_money: "0.00",
          settle_type: 0,
        }),
      );
    if (p.endsWith("/settlement-policy")) {
      if (mode === "failure")
        return route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "quote unavailable" }),
        });
      return route.fulfill(
        ok({
          can_apply: mode !== "refused",
          external_payout: false,
          terms: {
            gross_amount: "100",
            fee_amount: "3",
            net_amount: "97",
            retained_cents: "0",
          },
        }),
      );
    }
    if (route.request().method() === "POST") {
      posts++;
      return route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({ code: -1, message: "fee changed" }),
      });
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/settle/apply");
  const dialog = page.getByRole("region", { name: "申请提现", exact: true });
  await dialog.getByLabel("金额（元）").fill("1.00");
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(dialog).toContainText("quote unavailable");
  expect(posts).toBe(0);
  mode = "refused";
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(dialog).toContainText("不允许该结算申请");
  expect(posts).toBe(0);
  mode = "allowed";
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog
    .getByRole("button", { name: "按已核对费用提交", exact: true })
    .click();
  await expect(dialog).toContainText("fee changed");
  await expect(
    dialog.getByRole("button", { name: "按已核对费用提交", exact: true }),
  ).toHaveCount(0);
  expect(posts).toBe(1);
});
