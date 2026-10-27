import { openMerchantOrderDetails } from "./fixtures/merchant-order-navigation";
import { test, expect } from "@playwright/test";

for (const area of ["admin", "merchant"] as const) {
  for (const width of [1280, 375]) {
    test(`${area} ${width}px original scan cancellation remains read-only and distinct`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      let writes = 0;
      const order = {
        trade_no: "ORDER-SCAN78",
        status: "unknown",
        amount: "100",
        merchant_id: "1000",
      };
      await page.route("**/console/v1/**", (route) => {
        const request = route.request();
        const path = new URL(request.url()).pathname;
        if (request.method() !== "GET") writes++;
        let data: unknown = { items: [order], total: 1 };
        if (path.endsWith("/session"))
          data = {
            user: {
              id: "1000",
              name: "Local",
              role: area,
              admin_class: "super",
            },
            csrf_token: "local78",
          };
        if (path.endsWith("/ORDER-SCAN78"))
          data = {
            order,
            provider: {
              channel_id: "7",
              method: "scan",
              state: "unknown",
              can_close: false,
            },
            scan_recovery: {
              state: "cancel_pending",
              queries: 6,
              maximum_queries: 6,
              last_provider_state: "WAIT_BUYER_PAY",
              started_at: "2026-10-24T10:00:00Z",
              expires_at: "2026-10-24T10:02:00Z",
              requires_reconciliation: true,
            },
          };
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(`/${area}/trade/order`);
      if (area === "merchant") await openMerchantOrderDetails(page);
      else
        await page.getByRole("button", { name: "详情", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "订单详情" });
      const recovery = dialog.getByRole("region", { name: "付款码支付处理" });
      await expect(recovery).toContainText("撤销结果待核实");
      await expect(recovery).toContainText("确认前不会入账");
      await expect(recovery).toContainText("6 / 6");
      await expect(recovery.locator("button, input, form, a")).toHaveCount(0);
      await expect(
        dialog.getByRole("button", { name: "关闭通道订单", exact: true }),
      ).toHaveCount(0);
      expect(writes).toBe(0);
      expect(
        await recovery.evaluate(
          (element) => element.scrollWidth <= element.clientWidth + 1,
        ),
      ).toBe(true);
      await recovery.scrollIntoViewIfNeeded();
      await expect(recovery).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath("scan-recovery.png") });
    });
  }
}
