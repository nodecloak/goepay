import { openMerchantOrderDetails } from "./fixtures/merchant-order-navigation";
import { test, expect } from "@playwright/test";

for (const area of ["admin", "merchant"] as const)
  for (const width of [1280, 375])
    test(`${area} long order identifiers stay in their cell and operations stay visible at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const writes: string[] = [];
      const order = {
        trade_no: "9007199254740993123",
        out_trade_no: "MERCHANT" + "1".repeat(120),
        api_trade_no: "PROVIDER" + "2".repeat(120),
        merchant_id: "1000",
        name: "本地可读性测试",
        amount: "9007199254740993",
        merchant_amount: "9007199254740992",
        platform_fee: "1",
        type: "alipay",
        status: "paid",
        notification_state: "succeeded",
        created_at: "2026-10-25T00:00:00Z",
      };
      await page.route("**/console/v1/**", (route) => {
        const req = route.request(),
          path = new URL(req.url()).pathname;
        if (req.method() !== "GET") writes.push(path);
        const data = path.endsWith("/session")
          ? {
              user: { id: "1000", role: area, admin_class: "super" },
              csrf_token: "local-only",
            }
          : path.endsWith(order.trade_no)
            ? { order, reporting: {} }
            : { items: [order], total: 1 };
        return route.fulfill({ json: { code: 0, data } });
      });
      await page.goto(`/${area}/trade/order`);
      const row = page.getByRole("row").filter({ hasText: order.out_trade_no });
      await expect(row).toHaveCount(1);
      await expect(row).toContainText("90,071,992,547,409.93");
      // The merchant number is one line that ends in an ellipsis inside its
      // own cell; the full merchant and API numbers are in its hover text.
      const merchantNo = row.locator("td.col-trade_no small");
      await expect(merchantNo).toHaveAttribute(
        "title",
        `商户订单号：${order.out_trade_no}\n接口订单号：${order.api_trade_no}`,
      );
      const fit = await merchantNo.evaluate((n) => {
        const box = n.getBoundingClientRect(),
          cell = n.closest("td")!.getBoundingClientRect(),
          style = getComputedStyle(n);
        return {
          inside: box.left >= cell.left && box.right <= cell.right,
          clipped:
            n.scrollWidth > n.clientWidth &&
            style.overflowX === "hidden" &&
            style.textOverflow === "ellipsis",
        };
      });
      expect(fit).toEqual({ inside: true, clipped: true });
      const table = row.locator(
        "xpath=ancestor::div[contains(@class,'table-scroll')]",
      );
      const operation = row.getByRole("cell").last();
      for (const position of [0, 1]) {
        await table.evaluate((el, p) => {
          el.scrollLeft = p ? el.scrollWidth : 0;
        }, position);
        await expect
          .poll(async () => {
            const c = await operation.boundingBox(),
              t = await table.boundingBox();
            return (
              !!c && !!t && c.x >= t.x - 1 && c.x + c.width <= t.x + t.width + 1
            );
          })
          .toBe(true);
      }
      if (area === "admin") {
        const trigger = row.getByRole("button", {
          name: `订单操作 ${order.trade_no}`,
          exact: true,
        });
        await trigger.click();
        await expect(
          page.getByRole("menuitem", { name: "API退款", exact: true }),
        ).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(trigger).toBeFocused();
      }
      if (area === "merchant") await openMerchantOrderDetails(page, row);
      else await row.getByRole("button", { name: "详情", exact: true }).click();
      await expect(
        page.getByRole("dialog", { name: "订单详情" }),
      ).toContainText(order.out_trade_no);
      expect(writes).toEqual([]);
    });

for (const area of ["admin", "merchant"] as const)
  for (const width of [1280, 375])
    test(`${area} complete long refund identifiers remain within their own columns at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const writes: string[] = [];
      const merchantRefund = "MERCHANT" + "3".repeat(120);
      await page.route("**/console/v1/**", (route) => {
        const req = route.request(),
          path = new URL(req.url()).pathname;
        if (req.method() !== "GET") writes.push(path);
        const data = path.endsWith("/session")
          ? {
              user: { id: "1000", role: area, admin_class: "super" },
              csrf_token: "local-only",
            }
          : {
              items: [
                {
                  id: "138",
                  platform_refund_no: "138",
                  out_refund_no: merchantRefund,
                  refund_no: merchantRefund,
                  trade_no: "9007199254740993123",
                  merchant_id: "1000",
                  amount: "9007199254740993",
                  merchant_amount: "9007199254740992",
                  status: "succeeded",
                  created_at: "2026-10-25T00:00:00Z",
                  completed_at: "2026-10-25T00:01:00Z",
                },
              ],
              total: 1,
            };
        return route.fulfill({ json: { code: 0, data } });
      });
      await page.goto(`/${area}/trade/refund`);
      const row = page.getByRole("row").filter({ hasText: merchantRefund });
      await expect(row).toHaveCount(1);
      await expect(row).toContainText("¥90,071,992,547,409.93");
      expect(
        await row.evaluate((el) => {
          const text = el.querySelector("small.refund-merchant-no")!;
          const cell = text.closest("td")!.getBoundingClientRect();
          const range = document.createRange();
          range.selectNodeContents(text);
          return Array.from(range.getClientRects()).every(
            (r) => r.left >= cell.left && r.right <= cell.right,
          );
        }),
      ).toBe(true);
      expect(writes).toEqual([]);
    });
