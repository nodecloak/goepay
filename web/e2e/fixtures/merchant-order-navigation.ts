import type { Page, Locator } from "@playwright/test";

// The original merchant UX exposes details in its per-order action menu.
export async function openMerchantOrderDetails(page: Page, row?: Locator) {
  await (row || page).getByRole("button", { name: /^订单操作 / }).click();
  await page.getByRole("menuitem", { name: "查看详情", exact: true }).click();
}
