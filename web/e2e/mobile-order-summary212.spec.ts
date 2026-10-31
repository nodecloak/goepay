import { test, expect } from "@playwright/test";
for (const area of ["admin", "merchant"] as const) {
  test(`${area} order amount and authoritative payment state are visible before horizontal scrolling at 375px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 375, height: 812 });
    const order = {
      trade_no: "MOBILE212",
      out_trade_no: "external212",
      merchant_id: "1000",
      name: "测试商品",
      amount: "1234",
      type: "wxpay",
      status: "pending",
      admin_status: "paid",
      refunded_amount: "0",
      created_at: "2026-10-30T00:00:00Z",
    };
    await page.route("**/console/v1/**", (route) =>
      route.fulfill({
        json: {
          code: 0,
          data: new URL(route.request().url()).pathname.endsWith("/session")
            ? {
                user: { id: "1000", role: area, admin_class: "super" },
                csrf_token: "local212",
              }
            : { items: [order], total: 1 },
        },
      }),
    );
    await page.goto(`/${area}/trade/order`);
    const row = page.getByRole("row").filter({ hasText: "external212" });
    const summary = row.locator(".order-mobile-summary");
    await expect(summary).toBeVisible();
    await expect(summary).toContainText("¥12.34");
    await expect(summary).toContainText("待支付");
    if (area === "admin")
      await expect(summary).toContainText("后台标记：已支付");
    const fit = await summary.evaluate((el) => {
      const b = el.getBoundingClientRect();
      return b.left >= 0 && b.right <= innerWidth;
    });
    expect(fit).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath("order-mobile.png"),
      fullPage: true,
    });
    await row.getByRole("checkbox").check();
    await expect(row.getByRole("checkbox")).toBeChecked();
    await row
      .getByRole("button", { name: "订单操作 MOBILE212", exact: true })
      .click();
    await expect(
      page.getByRole("menu", { name: "订单操作 MOBILE212", exact: true }),
    ).toBeVisible();
    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 1440, height: 1000 });
    await expect(summary).toBeHidden();
    await expect(row.locator("td.col-status")).toContainText(
      area === "admin" ? "实际状态：待支付" : "待支付",
    );
  });
}
