import { test, expect } from "@playwright/test";

for (const area of ["admin", "merchant"])
  for (const width of [1280, 375])
    test(`${area} original refund columns and associated order remain read only at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const writes: string[] = [],
        details: string[] = [];
      await page.route("**/console/v1/**", (route) => {
        const request = route.request(),
          path = new URL(request.url()).pathname;
        if (request.method() !== "GET") writes.push(path);
        let data: unknown = { items: [], total: 0 };
        if (path.endsWith("/session"))
          data = {
            user: {
              id: "1",
              name: "local",
              role: area,
              admin_class: area === "admin" ? "super" : undefined,
            },
            csrf_token: "refund130",
          };
        else if (path.endsWith("/refunds"))
          data = {
            total: 1,
            items: [
              {
                id: "130",
                platform_refund_no: "130",
                out_refund_no: "merchant-refund-130",
                refund_no: "merchant-refund-130",
                trade_no: "ORDER-130",
                merchant_id: "1",
                amount: "1234",
                merchant_amount: "1100",
                status: "succeeded",
                created_at: "2026-10-25T00:00:00Z",
                completed_at: "2026-10-25T00:01:00Z",
                updated_at: "2026-10-27T00:00:00Z",
              },
            ],
          };
        else if (path.endsWith("/orders/ORDER-130")) {
          details.push(path);
          data = {
            order: {
              trade_no: "ORDER-130",
              out_trade_no: "merchant-order-130",
              merchant_id: "1",
              amount: "10000",
              merchant_amount: "9900",
              platform_fee: "100",
              status: "paid",
              created_at: "2026-10-24T00:00:00Z",
            },
            provider: null,
          };
        }
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(`/${area}/trade/refund`);
      const headers = page.getByRole("columnheader");
      await expect(headers).toHaveCount(area === "admin" ? 6 : 5);
      await expect(headers.nth(0)).toContainText("商户退款单号");
      await expect(headers.nth(1)).toHaveText("关联订单号");
      await expect(headers.filter({ hasText: "退款金额" })).toContainText(
        "扣减金额",
      );
      await expect(headers.filter({ hasText: "申请时间" })).toContainText(
        "完成时间",
      );
      await expect(
        page.getByRole("cell", { name: "¥12.34 ¥11.00", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText("2026-10-25 08:01:00", { exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("table").getByText("退款成功", { exact: true }),
      ).toBeVisible();
      await expect(page.getByText("2026-10-27", { exact: false })).toHaveCount(
        0,
      );
      const open = page.getByRole("button", {
        name: "查看关联订单 ORDER-130",
        exact: true,
      });
      await open.click();
      const dialog = page.getByRole("dialog", {
        name: "订单详情",
        exact: true,
      });
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText("merchant-order-130");
      await expect(dialog).toContainText("100.00");
      await expect(dialog).toContainText("99.00");
      await expect(dialog).not.toContainText("12.34");
      expect(details).toEqual([`/console/v1/${area}/orders/ORDER-130`]);
      await dialog
        .getByRole("button", { name: "关闭详情", exact: true })
        .click();
      await expect(dialog).toHaveCount(0);
      await expect(open).toBeFocused();
      expect(writes).toHaveLength(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
    });
