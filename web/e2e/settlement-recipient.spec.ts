import { test, expect } from "@playwright/test";
import { accountSettingsReadFixture } from "./fixtures/account-settings";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const recipient = (id = "42") => ({
  state: "configured",
  config_id: id,
  type: "alipay",
  app_id: "",
  account_masked: "***1234",
  name_masked: "***",
});
test("settlement recipient edit clears fee confirmation and binds new snapshot version without storing PII", async ({
  page,
}) => {
  let current = "42",
    posts = 0,
    quotes = 0,
    saves = 0;
  const quoteReads: {
    path: string;
    method: string;
    amount: string | null;
    merchantID: string | null;
  }[] = [];
  await page.route("**/console/v1/**", async (route) => {
    const url = new URL(route.request().url()),
      p = url.pathname,
      method = route.request().method();
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Merchant", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/settlement-recipient/allowed-types"))
      return route.fulfill(
        ok({ settlement: ["alipay", "wxpay", "bank"], can_write: true }),
      );
    if (p.endsWith("/settlement-recipient")) {
      if (method === "POST") {
        saves++;
        expect(route.request().postDataJSON()).toMatchObject({
          expected_config_id: "42",
          account: "synthetic-private-account",
          name: "Synthetic Private Name",
        });
        current = "43";
      }
      return route.fulfill(
        ok({
          ...recipient(current),
          ...(method === "GET" ? { can_write: true } : {}),
        }),
      );
    }
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
        method,
        amount: url.searchParams.get("amount"),
        merchantID: url.searchParams.get("merchant_id"),
      });
      return route.fulfill(
        ok({
          can_apply: true,
          external_payout: false,
          terms: {
            gross_amount: "123",
            fee_amount: "3",
            net_amount: "120",
            retained_cents: "0",
          },
        }),
      );
    }
    if (p.endsWith("/settlements") && method === "POST") {
      posts++;
      const body = route.request().postDataJSON();
      expect(body).toMatchObject({
        recipient_config_id: "43",
        expected_fee_cents: "3",
        expected_net_cents: "120",
      });
      return route.fulfill({
        ...ok({
          id: "212",
          kind: "settlements",
          merchant_id: "1",
          external_no: body.external_no,
          status: "pending",
          amount: "1.23",
          amount_cents: "123",
          external_payout: false,
          created_at: "2026-10-30T00:00:00Z",
          settlement: {
            gross_amount: "123",
            fee_amount: "3",
            net_amount: "120",
          },
          settlement_recipient: { ...recipient("43"), state: "recorded" },
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
    return route.fulfill(
      ok({
        items: [
          {
            id: "old",
            status: "approved",
            merchant_id: "1",
            amount: "1.00",
            external_no: "historical",
            settlement: null,
            settlement_recipient: null,
            settlement_funding: null,
          },
        ],
        total: 1,
      }),
    );
  });
  await page.goto("/merchant/settle/list");
  await expect(
    page.getByText("未记录收款快照，不能出款", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "申请结算", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByTestId("settlement-recipient-current"),
  ).toContainText("版本 42");
  await dialog.getByLabel("金额（元）").fill("1.23");
  // Inline preview is a legitimate debounced GET. Let it finish before
  // measuring the separate, explicit fee-confirmation action.
  await expect(dialog.locator(".settle-live")).toContainText(
    "实际到账：1.20 元",
  );
  const beforeOriginalConfirmation = quotes;
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "按已核对费用提交", exact: true }),
  ).toBeVisible();
  expect(quotes).toBe(beforeOriginalConfirmation + 1);
  expect(posts).toBe(0);
  await dialog
    .getByRole("button", { name: "修改收款资料", exact: true })
    .click();
  await expect(
    dialog.getByRole("button", { name: "按已核对费用提交", exact: true }),
  ).toHaveCount(0);
  await expect(dialog.getByLabel("收款账号", { exact: true })).toHaveValue("");
  await dialog
    .getByLabel("收款账号", { exact: true })
    .fill("synthetic-private-account");
  await dialog
    .getByLabel("真实姓名", { exact: true })
    .fill("Synthetic Private Name");
  await dialog
    .getByRole("button", { name: "保存收款版本", exact: true })
    .click();
  await expect(
    dialog.getByTestId("settlement-recipient-current"),
  ).toContainText("版本 43");
  expect(saves).toBe(1);
  expect(posts).toBe(0);
  await expect(dialog.locator(".settle-live")).toContainText(
    "实际到账：1.20 元",
  );
  const beforeUpdatedConfirmation = quotes;
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "按已核对费用提交", exact: true }),
  ).toBeVisible();
  expect(quotes).toBe(beforeUpdatedConfirmation + 1);
  await dialog
    .getByRole("button", { name: "按已核对费用提交", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  expect(posts).toBe(1);
  expect(quotes).toBe(beforeUpdatedConfirmation + 1);
  for (const quote of quoteReads)
    expect(quote).toEqual({
      path: "/console/v1/merchant/workflows/settlement-policy",
      method: "GET",
      amount: "1.23",
      merchantID: null,
    });
  const stored = await page.evaluate(() =>
    JSON.stringify({
      local: { ...localStorage },
      session: { ...sessionStorage },
    }),
  );
  expect(stored).not.toContain("synthetic-private-account");
  expect(stored).not.toContain("Synthetic Private Name");
});

test("missing recipient prevents new UI application and configuration conflict never retries credentials", async ({
  page,
}) => {
  let configured = false,
    saves = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
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
    if (p.endsWith("/settlement-recipient/allowed-types"))
      return route.fulfill(
        ok({ settlement: ["alipay", "wxpay", "bank"], can_write: true }),
      );
    if (p.endsWith("/settlement-recipient")) {
      if (route.request().method() === "POST") {
        saves++;
        configured = true;
        return route.fulfill({
          status: 409,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "config changed" }),
        });
      }
      return route.fulfill(
        ok(
          configured
            ? { ...recipient("44"), can_write: true }
            : {
                can_write: true,
                state: "missing",
                config_id: "0",
                type: "",
                app_id: "",
                account_masked: "",
                name_masked: "",
              },
        ),
      );
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/settle/list");
  await page.getByRole("button", { name: "申请结算", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByText(
      "尚未配置收款资料；请先配置。历史缺失快照的申请不能出款。",
    ),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "确认提交", exact: true }),
  ).toBeDisabled();
  await dialog
    .getByRole("button", { name: "配置收款资料", exact: true })
    .click();
  await dialog.getByLabel("收款账号").fill("synthetic-recipient");
  await dialog.getByLabel("真实姓名").fill("Synthetic Name");
  await dialog.getByRole("button", { name: "保存收款版本" }).click();
  await expect(dialog).toContainText(
    "收款版本已变化，请核对当前版本后重新编辑",
  );
  await expect(
    dialog.getByTestId("settlement-recipient-current"),
  ).toContainText("版本 44");
  expect(saves).toBe(1);
});

test("original settlement settings menu uses encrypted version API and never writes legacy plaintext resources", async ({
  page,
}) => {
  let legacy = 0,
    saves = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (
      p.includes("/resources/settlement-settings") ||
      p.includes("/manage/settlement-settings")
    )
      legacy++;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Merchant", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/settlement-recipient/allowed-types"))
      return route.fulfill(
        ok({ settlement: ["alipay", "wxpay", "bank"], can_write: true }),
      );
    if (p.endsWith("/settlement-recipient")) {
      if (route.request().method() === "POST") {
        saves++;
        expect(route.request().postDataJSON().expected_config_id).toBe("42");
        return route.fulfill(ok(recipient("43")));
      }
      return route.fulfill(ok({ ...recipient("42"), can_write: true }));
    }
    return route.fulfill(
      ok(accountSettingsReadFixture(p, "1") ?? { items: [], total: 0 }),
    );
  });
  await page.goto("/merchant/settle/settings");
  await expect(page.getByTestId("settlement-recipient-current")).toContainText(
    "***1234",
  );
  await page.getByRole("button", { name: "修改收款资料", exact: true }).click();
  await expect(page.getByLabel("收款账号", { exact: true })).toHaveValue("");
  await page
    .getByLabel("收款账号", { exact: true })
    .fill("new-local-recipient");
  await page.getByLabel("真实姓名").fill("Local Name");
  await page.getByRole("button", { name: "保存收款版本" }).click();
  await expect(page.getByTestId("settlement-recipient-current")).toContainText(
    "版本 43",
  );
  expect(legacy).toBe(0);
  expect(saves).toBe(1);
});
