import { pageReload248 } from "./fixtures/page-reload248";
import { test, expect, type Page } from "@playwright/test";
async function fixture(page: Page, rejected = false, withTest = false) {
  let smtp = {
    name: "smtp",
    title: "SMTP发信",
    configured: withTest,
    status: withTest ? 1 : 0,
    config: {
      host: withTest ? "smtp.synthetic212.invalid" : "",
      port: "465",
      username: withTest ? "test212@example.invalid" : "",
    },
    password_set: withTest,
    revision: withTest ? "1" : "0",
    fingerprint: "a".repeat(64),
    test_available: withTest,
    effective_source: withTest ? "console" : "deployment",
  };
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    let data: unknown = {};
    if (path.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: "super",
          name: "Embedded212",
        },
        csrf_token: "embedded212",
      };
    else if (path.endsWith("/resources/settings/schema"))
      data = {
        title: "设置",
        fields: [{ name: "key", type: "select", options: ["site_name"] }],
        permissions: { create: true, update: true, delete: false },
      };
    else if (path.endsWith("/resources/settings"))
      data = {
        items: [{ id: "1", key: "site_name", value: "独立面板测试" }],
        total: 1,
      };
    else if (path.endsWith("/settings/mail-channels/smtp")) {
      if (req.method() === "PATCH") {
        if (rejected)
          return route.fulfill({
            status: 400,
            contentType: "application/json",
            body: JSON.stringify({
              code: -1,
              message: "Synthetic rejection212",
            }),
          });
        const body = req.postDataJSON();
        smtp = {
          ...smtp,
          configured: true,
          status: body.status,
          password_set: true,
          revision: "1",
          fingerprint: "b".repeat(64),
          effective_source: "console",
          config: {
            host: body.config.host,
            port: body.config.port,
            username: body.config.username,
          },
        };
      }
      data = smtp;
    } else if (path.endsWith("/settings/mail-channels/smtp/test"))
      data = { request_id: req.postDataJSON().request_id, status: "unknown" };
    else if (path.includes("/settings/mail-channels/smtp/tests/"))
      data = { request_id: path.split("/").at(-1), status: "unknown" };
    else if (path.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (path.endsWith("/dashboard"))
      data = {
        orders: 0,
        paid_orders: 0,
        summary: {},
        trend: [],
        payment_mix: [],
        announcements: [],
        recent_settlements: [],
      };
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/system/settings");
  await page.getByRole("tab", { name: "邮箱配置", exact: true }).click();
  if (withTest)
    return page.getByRole("dialog", { name: "SMTP发信账号设置", exact: true });
  await page.getByRole("button", { name: "账号设置", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "SMTP发信账号设置",
    exact: true,
  });
  await dialog
    .getByLabel("SMTP服务器", { exact: true })
    .fill("smtp.synthetic212.invalid");
  await dialog
    .getByLabel("邮箱账号", { exact: true })
    .fill("draft212@example.invalid");
  await dialog
    .getByLabel("邮箱密码", { exact: true })
    .fill("synthetic-secret212");
  return dialog;
}
async function unloadBlocked(page: Page) {
  return page.evaluate(() => {
    const e = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(e);
    return e.defaultPrevented;
  });
}
async function suspend(page: Page) {
  await page
    .getByRole("dialog", { name: "SMTP发信账号设置", exact: true })
    .getByRole("button", { name: "收起并保留草稿", exact: true })
    .click();
}
test("SMTP draft remains in memory across categories and cancel explicitly clears it", async ({
  page,
}) => {
  await fixture(page);
  await suspend(page);
  await page.getByRole("tab", { name: "高级设置", exact: true }).click();
  expect(await unloadBlocked(page)).toBe(true);
  await page.getByRole("tab", { name: "邮箱配置", exact: true }).click();
  await page.getByRole("button", { name: "继续编辑草稿", exact: true }).click();
  await expect(page.getByLabel("SMTP服务器", { exact: true })).toHaveValue(
    "smtp.synthetic212.invalid",
  );
  await expect(page.getByLabel("邮箱密码", { exact: true })).toHaveValue(
    "synthetic-secret212",
  );
  await page
    .getByRole("dialog", { name: "SMTP发信账号设置", exact: true })
    .getByRole("button", { name: "取消", exact: true })
    .click();
  expect(await unloadBlocked(page)).toBe(false);
  expect(
    await page.evaluate(() =>
      JSON.stringify({
        local: { ...localStorage },
        session: { ...sessionStorage },
      }),
    ),
  ).not.toContain("synthetic-secret212");
});
test("SMTP draft stays guarded across categories; page reload discards private fields", async ({
  page,
}) => {
  await fixture(page);
  await suspend(page);
  await page.getByRole("tab", { name: "高级设置", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "刷新设置", exact: true }),
  ).toHaveCount(0);
  expect(await unloadBlocked(page)).toBe(true);
  await page.getByRole("tab", { name: "邮箱配置", exact: true }).click();
  await page.getByRole("button", { name: "继续编辑草稿", exact: true }).click();
  await expect(page.getByLabel("SMTP服务器", { exact: true })).toHaveValue(
    "smtp.synthetic212.invalid",
  );
  await suspend(page);
  await pageReload248(page);
  await page.getByRole("button", { name: "账号设置", exact: true }).click();
  await expect(page.getByLabel("SMTP服务器", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("邮箱密码", { exact: true })).toHaveValue("");
});

test("SMTP draft protects route leave; success clears the unload guard", async ({
  page,
}) => {
  const dialog = await fixture(page);
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(await unloadBlocked(page)).toBe(false);
  await page.getByRole("button", { name: "账号设置", exact: true }).click();
  await page
    .getByLabel("SMTP服务器", { exact: true })
    .fill("second.synthetic212.invalid");
  await suspend(page);
  await page.getByRole("link", { name: "仪表盘", exact: true }).first().click();
  const confirm = page.getByRole("dialog", {
    name: "放弃未保存的设置？",
    exact: true,
  });
  await expect(confirm).toContainText("SMTP");
  await confirm.getByRole("button", { name: "继续编辑", exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/system\/settings$/);
});
test("a rejected SMTP save retains a guarded draft until explicit discard", async ({
  page,
}) => {
  const dialog = await fixture(page, true);
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: /保存/ }).first(),
  ).toBeVisible();
  expect(await unloadBlocked(page)).toBe(true);
});

test("SMTP local reread cannot silently destroy an unresolved descendant test-mail request", async ({
  page,
}) => {
  await fixture(page, false, true);
  await page.getByRole("button", { name: "发送测试邮件", exact: true }).click();
  const sending = page.getByRole("dialog", {
    name: "发送测试邮件",
    exact: true,
  });
  await sending.getByLabel("收件邮箱地址").fill("test212@example.invalid");
  await sending.getByRole("button", { name: "发送", exact: true }).click();
  await expect(sending).toContainText("发送结果尚未确认");
  await sending.getByRole("button", { name: "关闭", exact: true }).click();
  expect(await unloadBlocked(page)).toBe(true);
  await expect(
    page.getByRole("button", { name: "重新读取SMTP账号", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "查看原测试结果", exact: true }),
  ).toBeVisible();
  expect(await unloadBlocked(page)).toBe(true);
  await page.getByRole("button", { name: "账号设置", exact: true }).click();
  await page
    .getByLabel("SMTP服务器", { exact: true })
    .fill("new.smtp.synthetic212.invalid");
  await page
    .getByLabel("邮箱密码", { exact: true })
    .fill("synthetic-secret212");
  const save = page
    .waitForRequest(
      (r) => r.method() === "PATCH" && r.url().includes("mail-channels/smtp"),
      { timeout: 600 },
    )
    .then(() => true)
    .catch(() => false);
  await page
    .getByRole("dialog", { name: "SMTP发信账号设置", exact: true })
    .getByRole("button", { name: "保存", exact: true })
    .click();
  await expect(
    page
      .getByRole("alert")
      .filter({ hasText: /测试邮件/ })
      .first(),
  ).toBeVisible();
  expect(await save).toBe(false);
});
