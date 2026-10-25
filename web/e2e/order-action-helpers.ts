import { expect, type Page } from "@playwright/test";

export async function openAdminAPIRefund(page: Page, tradeNo: string) {
  await page
    .getByRole("button", { name: `订单操作 ${tradeNo}`, exact: true })
    .click();
  await page.getByRole("menuitem", { name: "API退款", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "API退款确认", exact: true }),
  ).toBeVisible();
}
