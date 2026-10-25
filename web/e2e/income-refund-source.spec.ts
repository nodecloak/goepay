import { test, expect } from "@playwright/test";

test("prepared held income previews original funding before sending a refund", async ({
  page,
}) => {
  let previews = 0,
    refunds = 0;
  const original = {
    trade_no: "HELD",
    status: "paid",
    amount: "1000",
    income_status: "held",
  };
  const ok = (data: unknown) => ({
    contentType: "application/json",
    body: JSON.stringify({ code: 0, data }),
  });
  await page.route("**/console/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (path.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (path.endsWith("/orders/HELD"))
      return route.fulfill(
        ok({
          order: original,
          income_hold: {
            id: "9",
            state: "held",
            amount: "1000",
            remaining_amount: "1000",
            refund_reserved_amount: "0",
            source_mode: "fresh",
          },
          can_refund_held_income: true,
        }),
      );
    if (path.endsWith("/preview-refund")) {
      previews++;
      return route.fulfill(
        ok({
          hold_id: "9",
          held_amount:
            route.request().postDataJSON().amount === "4.00" ? "400" : "300",
          available_amount: "0",
          fee_amount: "0",
          held_remaining: "1000",
          funding_mode: "income_hold",
        }),
      );
    }
    if (path.endsWith("/refund")) {
      refunds++;
      return route.fulfill(ok({ status: "unknown", refund_no: "original" }));
    }
    return route.fulfill(ok({ items: [original], total: 1 }));
  });
  await page.goto("/admin/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await page
    .getByRole("dialog", { name: "订单详情" })
    .getByRole("button", { name: "申请退款", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "API退款确认", exact: true });
  await dialog.getByLabel("退款金额（元）").fill("4.00");
  await dialog
    .getByRole("button", { name: "核对退款资金来源", exact: true })
    .click();
  await expect(dialog).toContainText("本次使用原订单冻结款");
  await expect(dialog).toContainText("4.00");
  expect(previews).toBe(1);
  expect(refunds).toBe(0);
  await dialog.getByLabel("退款金额（元）").fill("3.00");
  await expect(
    dialog.getByRole("button", { name: "核对退款资金来源", exact: true }),
  ).toBeVisible();
  expect(refunds).toBe(0);
  await dialog
    .getByRole("button", { name: "核对退款资金来源", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "确认申请退款", exact: true })
    .click();
  await expect(
    page.getByRole("status").filter({ hasText: "尚未核实" }),
  ).toContainText("尚未核实");
  expect(previews).toBe(2);
  expect(refunds).toBe(1);
});
