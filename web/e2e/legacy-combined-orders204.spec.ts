import { expect, test, type Page } from "@playwright/test";
import { openMerchantOrderDetails } from "./fixtures/merchant-order-navigation";
import { publicSiteFixture191 } from "./fixtures/site191";

async function legacyCombined204(page: Page, failOnce = false) {
  const reads: string[] = [],
    writes: string[] = [],
    errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/console/v1/**", async (route) => {
    const site = publicSiteFixture191(route);
    if (site) return site;
    const request = route.request(),
      path = new URL(request.url()).pathname;
    const reply = (data: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify({
          code: status === 200 ? 0 : -1,
          message: status === 200 ? "" : "历史子订单读取暂不可用",
          data,
        }),
      });
    if (request.method() !== "GET") {
      writes.push(path);
      return reply(null, 405);
    }
    if (path.endsWith("/session"))
      return reply({
        user: { id: "204", name: "历史合单商户204", role: "merchant" },
        csrf_token: "csrf-combined204",
      });
    if (path.endsWith("/suborders")) {
      reads.push(path);
      if (failOnce) {
        failOnce = false;
        return reply(null, 503);
      }
      return reply({
        trade_no: "PARENT-204",
        read_only: true,
        verified_payment_source: false,
        source: "epay_pro",
        export_id: "synthetic-read-only204",
        exported_at: "2026-10-27T01:02:03Z",
        parent_settle: 1,
        charged_money: "5.25",
        children_money: "5.25",
        amounts_match: true,
        items: [
          {
            sub_trade_no: "9007199254740993123",
            api_trade_no: "API-LONG-204/123456789012345678901234567890",
            money: "1.75",
            refundmoney: "0.50",
            status: 2,
            settle: 1,
          },
          {
            sub_trade_no: "CHILD-UNKNOWN-204",
            api_trade_no: "",
            money: "1.75",
            refundmoney: null,
            status: 1,
            settle: 0,
          },
          {
            sub_trade_no: "CHILD-UNPAID-204",
            api_trade_no: "API-UNPAID-204",
            money: "1.75",
            refundmoney: null,
            status: 0,
            settle: 0,
          },
        ],
      });
    }
    const order = {
      trade_no: "PARENT-204",
      merchant_id: "204",
      status: "paid",
      amount: "500",
      combine: 1,
      merchant_actions: { query: false, notify: false, refund: false },
    };
    if (path.endsWith("/orders/PARENT-204"))
      return reply({
        order,
        can_view_suborders: true,
        can_resend_notification: false,
        can_return_notification: false,
      });
    return reply({ items: [order], total: 1 });
  });
  await page.goto("/merchant/trade/order");
  await openMerchantOrderDetails(page);
  return { reads, writes, errors };
}

for (const width of [375, 1440])
  test(`historical combined children stay read-only, locally scrollable and keyboard accessible ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const f = await legacyCombined204(page);
    const detail = page.getByRole("dialog", { name: "订单详情", exact: true });
    const trigger = detail.getByRole("button", {
      name: "查看子订单",
      exact: true,
    });
    await expect(trigger).toBeVisible();
    expect(f.reads).toHaveLength(0);
    await trigger.click();
    const modal = page.getByRole("dialog", {
      name: "合单支付 - 子订单列表",
      exact: true,
    });
    await expect(modal).toBeVisible();
    await expect(modal).toContainText("仅供查询");
    await expect(modal).toContainText("5.25");
    await expect(modal).toContainText("已退款(0.50)");
    await expect(modal.locator("tbody tr")).toHaveCount(3);
    expect(await modal.getByRole("button").allTextContents()).not.toEqual(
      expect.arrayContaining([expect.stringMatching(/退款|结算|扣款/)]),
    );
    const scroll = modal.getByRole("region", {
      name: "子订单列表",
      exact: true,
    });
    await expect(scroll).toBeVisible();
    if (width === 375)
      expect(
        await scroll.evaluate(
          (element) => element.scrollWidth > element.clientWidth,
        ),
      ).toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await modal
      .getByRole("button", { name: "关闭子订单列表", exact: true })
      .focus();
    await page.keyboard.press("Tab");
    expect(
      await modal.evaluate((element) =>
        element.contains(document.activeElement),
      ),
    ).toBe(true);
    await page.keyboard.press("Escape");
    await expect(modal).toHaveCount(0);
    await expect(detail).toBeVisible();
    await expect(trigger).toBeFocused();
    expect(f.reads).toEqual([
      "/console/v1/merchant/orders/PARENT-204/suborders",
    ]);
    expect(f.writes).toHaveLength(0);
    expect(f.errors).toHaveLength(0);
  });

test("historical combined read failure can be retried from the same modal without mutations", async ({
  page,
}) => {
  const f = await legacyCombined204(page, true);
  await page.getByRole("button", { name: "查看子订单", exact: true }).click();
  const modal = page.getByRole("dialog", {
    name: "合单支付 - 子订单列表",
    exact: true,
  });
  await expect(modal.getByRole("alert")).toBeVisible();
  await expect(modal.locator("tbody tr")).toHaveCount(0);
  await modal
    .getByRole("button", { name: "重新读取子订单", exact: true })
    .click();
  await expect(modal.locator("tbody tr")).toHaveCount(3);
  expect(f.reads).toHaveLength(2);
  expect(f.writes).toHaveLength(0);
});
