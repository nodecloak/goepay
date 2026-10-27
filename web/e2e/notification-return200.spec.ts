import { expect, test } from "@playwright/test";
import { openMerchantOrderDetails } from "./fixtures/merchant-order-navigation";

const ok = (data: unknown) => ({
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});

test("original synchronous notification opens the signed merchant return after the asynchronous action", async ({
  page,
  context,
}) => {
  const posts: string[] = [];
  await context.route("https://merchant-return200.invalid/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: "<h1>Synthetic merchant received browser return200</h1>",
    }),
  );
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M200", role: "merchant" },
          csrf_token: "csrf-return200",
        }),
      );
    if (request.method() === "POST") {
      posts.push(path);
      expect(request.headers()["x-csrf-token"]).toBe("csrf-return200");
      expect(request.postDataJSON()).toEqual({});
      expect(path).toBe(
        "/console/v1/merchant/orders/ORDER-200/return-notification",
      );
      return route.fulfill(
        ok({
          url: "https://merchant-return200.invalid/receive?trade_no=ORDER-200&sign=synthetic200",
        }),
      );
    }
    const order = {
      trade_no: "ORDER-200",
      status: "paid",
      amount: "123",
      merchant_actions: { query: false, notify: true, refund: false },
    };
    return route.fulfill(
      ok(
        path.endsWith("/orders/ORDER-200")
          ? {
              order,
              can_resend_notification: true,
              can_return_notification: true,
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await openMerchantOrderDetails(page);
  const detail = page.getByRole("dialog", { name: "订单详情" });
  const sync = detail.getByRole("button", {
    name: "重新通知(同步)",
    exact: true,
  });
  await expect(sync).toBeVisible();
  const labels = await detail.getByRole("button").allTextContents();
  expect(labels.findIndex((value) => value.includes("重发商户通知"))).toBeLessThan(
    labels.findIndex((value) => value.includes("重新通知(同步)")),
  );
  expect(posts).toHaveLength(0);
  const opened = page.waitForEvent("popup");
  await sync.click();
  const popup = await opened;
  await expect(popup.getByRole("heading")).toHaveText(
    "Synthetic merchant received browser return200",
  );
  expect(await popup.evaluate(() => window.opener === null)).toBe(true);
  await expect(detail).not.toContainText("sign=synthetic200");
  await expect(detail).not.toContainText("商户通知已收到成功响应");
  expect(posts).toHaveLength(1);
  await popup.close();
});

test("unpaid and delegated merchant details never expose the synchronous return action", async ({
  page,
}) => {
  let delegated = false;
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M200", role: "merchant" },
          csrf_token: "csrf-return200",
          ...(delegated ? { delegated_by: { id: "1", name: "Admin200" } } : {}),
        }),
      );
    if (request.method() === "POST") writes++;
    const order = {
      trade_no: "BLOCKED-200",
      status: delegated ? "paid" : "pending",
      amount: "123",
    };
    return route.fulfill(
      ok(
        path.endsWith("/orders/BLOCKED-200")
          ? { order, can_return_notification: true }
          : { items: [order], total: 1 },
      ),
    );
  });
  for (const mode of [false, true]) {
    delegated = mode;
    await page.goto("/merchant/trade/order");
    await openMerchantOrderDetails(page);
    await expect(
      page
        .getByRole("dialog", { name: "订单详情" })
        .getByRole("button", { name: "重新通知(同步)", exact: true }),
    ).toHaveCount(0);
  }
  expect(writes).toBe(0);
});
