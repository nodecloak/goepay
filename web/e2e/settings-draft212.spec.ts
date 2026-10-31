import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";
const actualPlatform = JSON.parse(
  fs.readFileSync(
    new URL("./globalaccess179-http-fixture.json", import.meta.url),
    "utf8",
  ),
);

// Break caught: refresh/navigation discards a draft, a different settings tab
// hides a dirty field from the guard, or a rejected save clears that guard.
async function fixture(page: Page, rejectSave = false) {
  const rows = [
    { id: "1", key: "site_name", value: "本地支付212" },
    { id: "2", key: "settlement_minimum_cents", value: "100" },
    { id: "3", key: "complain_open", value: "0" },
  ];
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: unknown = {};
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: "super",
          name: "Local212",
        },
        csrf_token: "draft212-session",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/resources/settings/schema"))
      data = {
        title: "系统设置",
        permissions: { create: true, update: true, delete: false },
        fields: [
          {
            name: "key",
            type: "select",
            options: rows.map((r) => ({ label: r.key, value: r.key })),
          },
          { name: "value", type: "string" },
        ],
      };
    else if (p.endsWith("/resources/settings"))
      data = { items: rows, total: rows.length };
    else if (/\/resources\/settings\/[123]$/.test(p)) {
      const row = rows.find((r) => p.endsWith("/" + r.id))!;
      if (req.method() !== "GET") {
        if (rejectSave)
          return route.fulfill({
            status: 400,
            contentType: "application/json",
            body: JSON.stringify({ code: -1, message: "本地校验拒绝212" }),
          });
        Object.assign(row, req.postDataJSON());
      }
      data = row;
    } else if (p.endsWith("/settings/merchant-access"))
      data = actualPlatform.admin_default;
    else if (p.endsWith("/dashboard"))
      data = {
        orders: 0,
        paid_orders: 0,
        available_money: "0.00",
        frozen_money: "0.00",
        summary: {},
        trend: [],
        payment_mix: [],
        announcements: [],
        recent_settlements: [],
        business_date: "2026-10-31",
        fee_schedule: {},
        payment_channels: [],
      };
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/system/settings");
  await expect(page.getByLabel("网站名称", { exact: true })).toHaveValue(
    "本地支付212",
  );
}
async function dirtyTwoTabs(page: Page) {
  await page.getByLabel("网站名称", { exact: true }).fill("未保存的网站212");
  await page.getByRole("tab", { name: "结算规则", exact: true }).click();
  await page.getByLabel("最低结算金额（元）", { exact: true }).fill("2.00");
  await page.getByRole("tab", { name: "网站信息", exact: true }).click();
  await expect(page.getByLabel("网站名称", { exact: true })).toHaveValue(
    "未保存的网站212",
  );
}
async function unloadBlocked(page: Page) {
  return page.evaluate(() => {
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  });
}

test("refresh cancel keeps drafts in every category, discard resets every category", async ({
  page,
}) => {
  await fixture(page);
  await dirtyTwoTabs(page);
  await page.getByRole("button", { name: "刷新设置", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "放弃未保存的设置？",
    exact: true,
  });
  await expect(dialog).toContainText("2项");
  await dialog.getByRole("button", { name: "继续编辑", exact: true }).click();
  await expect(page.getByLabel("网站名称", { exact: true })).toHaveValue(
    "未保存的网站212",
  );
  await page.getByRole("tab", { name: "结算规则", exact: true }).click();
  await expect(
    page.getByLabel("最低结算金额（元）", { exact: true }),
  ).toHaveValue("2.00");
  await page.getByRole("button", { name: "刷新设置", exact: true }).click();
  await dialog
    .getByRole("button", { name: "放弃修改并刷新", exact: true })
    .click();
  await expect(
    page.getByLabel("最低结算金额（元）", { exact: true }),
  ).toHaveValue("1.00");
  await page.getByRole("tab", { name: "网站信息", exact: true }).click();
  await expect(page.getByLabel("网站名称", { exact: true })).toHaveValue(
    "本地支付212",
  );
  expect(await unloadBlocked(page)).toBe(false);
});

test("leaving settings confirms hidden-category drafts and cancel preserves the current route", async ({
  page,
}) => {
  await fixture(page);
  await page.getByLabel("网站名称", { exact: true }).fill("未保存的网站212");
  await page.getByRole("tab", { name: "高级设置", exact: true }).click();
  await page.getByRole("link", { name: "仪表盘", exact: true }).first().click();
  const dialog = page.getByRole("dialog", {
    name: "放弃未保存的设置？",
    exact: true,
  });
  await expect(dialog).toContainText("1项");
  await dialog.getByRole("button", { name: "继续编辑", exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/system\/settings$/);
  await page.getByRole("tab", { name: "网站信息", exact: true }).click();
  await expect(page.getByLabel("网站名称", { exact: true })).toHaveValue(
    "未保存的网站212",
  );
  await page.getByRole("link", { name: "仪表盘", exact: true }).first().click();
  await dialog
    .getByRole("button", { name: "放弃修改并离开", exact: true })
    .click();
  await expect(page).toHaveURL(/\/admin\/dashboard$/);
  expect(await unloadBlocked(page)).toBe(false);
});

test("browser unload warns for another category and a successful save removes the warning", async ({
  page,
}) => {
  await fixture(page);
  expect(await unloadBlocked(page)).toBe(false);
  await page.getByLabel("网站名称", { exact: true }).fill("已保存的网站212");
  await page.getByRole("tab", { name: "高级设置", exact: true }).click();
  expect(await unloadBlocked(page)).toBe(true);
  await page.getByRole("tab", { name: "网站信息", exact: true }).click();
  await page.getByRole("button", { name: "保存本页设置", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: /^已保存1项设置$/ }),
  ).toBeVisible();
  expect(await unloadBlocked(page)).toBe(false);
});

test("a rejected settings save keeps the draft guarded", async ({ page }) => {
  await fixture(page, true);
  await page.getByLabel("网站名称", { exact: true }).fill("拒绝的修改212");
  await page.getByRole("button", { name: "保存本页设置", exact: true }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "本地校验拒绝212" }),
  ).toBeVisible();
  expect(await unloadBlocked(page)).toBe(true);
  await page.getByRole("button", { name: "刷新设置", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "放弃未保存的设置？", exact: true }),
  ).toBeVisible();
});

test("a permitted complaint settings deep link opens the relevant category", async ({
  page,
}) => {
  await fixture(page);
  await page.goto("/admin/system/settings?tab=complaint");
  await expect(
    page.getByRole("tab", { name: "交易投诉", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  const panel = page.getByRole("tabpanel", { name: "交易投诉", exact: true });
  await expect(panel.getByText("开启交易投诉", { exact: true })).toBeVisible();
  await expect(panel.getByRole("switch")).toBeVisible();
});
