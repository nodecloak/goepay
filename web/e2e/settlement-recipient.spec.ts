import { test, expect } from "@playwright/test";
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
  await page.route("**/console/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname,
      method = route.request().method();
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Merchant", role: "merchant" },
          csrf_token: "csrf",
        }),
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
      return route.fulfill(ok(recipient(current)));
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
      expect(route.request().postDataJSON()).toMatchObject({
        recipient_config_id: "43",
        expected_fee_cents: "3",
        expected_net_cents: "120",
      });
      return route.fulfill({ ...ok({}), status: 201 });
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
  await dialog.getByLabel("金额（元）", { exact: true }).fill("1.23");
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "按已核对费用提交", exact: true }),
  ).toBeVisible();
  await dialog
    .getByRole("button", { name: "修改收款资料", exact: true })
    .click();
  await expect(
    dialog.getByRole("button", { name: "按已核对费用提交", exact: true }),
  ).toHaveCount(0);
  await expect(dialog.getByLabel("新收款账号", { exact: true })).toHaveValue(
    "",
  );
  await dialog
    .getByLabel("新收款账号", { exact: true })
    .fill("synthetic-private-account");
  await dialog
    .getByLabel("新收款姓名", { exact: true })
    .fill("Synthetic Private Name");
  await dialog
    .getByRole("button", { name: "保存收款版本", exact: true })
    .click();
  await expect(
    dialog.getByTestId("settlement-recipient-current"),
  ).toContainText("版本 43");
  expect(saves).toBe(1);
  expect(posts).toBe(0);
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog
    .getByRole("button", { name: "按已核对费用提交", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  expect(posts).toBe(1);
  expect(quotes).toBe(2);
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
            ? recipient("44")
            : {
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
  await dialog.getByLabel("新收款账号").fill("synthetic-recipient");
  await dialog.getByLabel("新收款姓名").fill("Synthetic Name");
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
    if (p.endsWith("/settlement-recipient")) {
      if (route.request().method() === "POST") {
        saves++;
        expect(route.request().postDataJSON().expected_config_id).toBe("42");
        return route.fulfill(ok(recipient("43")));
      }
      return route.fulfill(ok(recipient("42")));
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/settle/settings");
  await expect(page.getByTestId("settlement-recipient-current")).toContainText(
    "***1234",
  );
  await page.getByRole("button", { name: "修改收款资料", exact: true }).click();
  await expect(page.getByLabel("新收款账号")).toHaveValue("");
  await page.getByLabel("新收款账号").fill("new-local-recipient");
  await page.getByLabel("新收款姓名").fill("Local Name");
  await page.getByRole("button", { name: "保存收款版本" }).click();
  await expect(page.getByTestId("settlement-recipient-current")).toContainText(
    "版本 43",
  );
  expect(legacy).toBe(0);
  expect(saves).toBe(1);
});
