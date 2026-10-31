import { expect, type Page } from "@playwright/test";

// The shared shell exposes logout inside the account menu.
export async function logoutConsole(page: Page) {
  const merchant = new URL(page.url()).pathname.startsWith("/merchant/");
  await page.locator('.topbar-avatar button[aria-haspopup="menu"]').click();
  await page.getByRole("menuitem", { name: "退出登录", exact: true }).click();
  if (merchant) await expect(page).toHaveURL(/\/merchant\/login(?:\?|$)/);
  await expect(page.getByRole("button", { name: "登录工作台" })).toBeVisible();
}

export async function expectNoConsoleErrors(page: Page) {
  // Dashboard account-completion tips are alerts, but are expected on a new
  // seeded merchant. Keep all actual error alerts in the assertion.
  await expect(page.locator('[role="alert"]:not(.dashboard-tip)')).toHaveCount(
    0,
  );
}
