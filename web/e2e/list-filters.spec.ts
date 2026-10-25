import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});

test("order filters reach the server as exact scoped values and clear on page change", async ({
  page,
}) => {
  const queries: URL[] = [];
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "token",
        }),
      );
    queries.push(url);
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/admin/trade/order");
  await page.getByText("更多筛选", { exact: true }).click({ timeout: 5000 });
  await page.getByLabel("状态筛选", { exact: true }).selectOption("paid");
  await page.getByLabel("订单收入状态", { exact: true }).selectOption("held");
  await page.getByLabel("支付方式筛选", { exact: true }).selectOption("alipay");
  await page.getByLabel("商户 ID 筛选", { exact: true }).fill("1000");
  await page.getByLabel("通道 ID 筛选", { exact: true }).fill("7");
  await page.getByLabel("创建开始日期", { exact: true }).fill("2026-10-22");
  await page.getByLabel("创建结束日期", { exact: true }).fill("2026-10-23");
  await page.getByLabel("买家标识筛选", { exact: true }).fill("buyer-001");
  await page.getByLabel("客户 IP 筛选", { exact: true }).fill("2001:db8::1");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect
    .poll(() => queries.at(-1)?.searchParams.get("buyer"))
    .toBe("buyer-001");
  expect(Object.fromEntries(queries.at(-1)!.searchParams)).toEqual({
    page: "1",
    limit: "20",
    status: "paid",
    income_status: "held",
    type: "alipay",
    merchant_id: "1000",
    channel_id: "7",
    from: "2026-10-22",
    to: "2026-10-23",
    buyer: "buyer-001",
    client_ip: "2001:db8::1",
  });
  await page.getByRole("link", { name: "退款记录", exact: true }).click();
  await expect
    .poll(() => queries.at(-1)?.pathname)
    .toBe("/console/v1/admin/refunds");
  expect(Object.fromEntries(queries.at(-1)!.searchParams)).toEqual({
    page: "1",
    limit: "20",
  });
});

test("merchant list filters never expose an owner override and reset exact query", async ({
  page,
}) => {
  const queries: URL[] = [];
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "token",
        }),
      );
    queries.push(url);
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto("/merchant/trade/order");
  await page.getByText("更多筛选", { exact: true }).click({ timeout: 5000 });
  await expect(page.getByLabel("商户 ID 筛选", { exact: true })).toHaveCount(0);
  await page.getByLabel("状态筛选", { exact: true }).selectOption("unknown");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect
    .poll(() => queries.at(-1)?.searchParams.get("status"))
    .toBe("unknown");
  await page.getByRole("button", { name: "清空筛选", exact: true }).click();
  await expect
    .poll(() => queries.at(-1)?.searchParams.has("status"))
    .toBe(false);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
