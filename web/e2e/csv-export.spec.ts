import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("filtered orders export includes all active filters, rejects JSON failure and downloads only verified CSV", async ({
  page,
}) => {
  const queries: URL[] = [];
  let exports = 0;
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (url.searchParams.get("format") === "csv") {
      queries.push(url);
      exports++;
      if (exports === 1)
        return route.fulfill({
          status: 422,
          contentType: "application/json",
          body: JSON.stringify({
            code: -1,
            message: "too many rows; narrow filters",
          }),
        });
      return route.fulfill({
        status: 200,
        contentType: "text/csv; charset=utf-8",
        headers: { "Content-Disposition": 'attachment; filename="orders.csv"' },
        body: '\uFEFF"trade_no"\r\n"ORDER"',
      });
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/admin/trade/order");
  await page.getByLabel("搜索记录", { exact: true }).fill("ORDER");
  await page.getByText("更多筛选", { exact: true }).click();
  await page.getByLabel("订单收入状态", { exact: true }).selectOption("held");
  await page.getByLabel("商户 ID 筛选", { exact: true }).fill("1000");
  await page
    .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("too many rows");
  expect(queries[0]?.searchParams.get("q")).toBe("ORDER");
  expect(queries[0]?.searchParams.get("income_status")).toBe("held");
  expect(queries[0]?.searchParams.get("merchant_id")).toBe("1000");
  const download = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
    .click();
  expect((await download).suggestedFilename()).toBe("orders-filtered.csv");
  await expect(page.getByText(/50,000/)).toBeVisible();
});
test("transfer report server export shares date filters and cancels a pending export on filter change", async ({
  page,
}) => {
  let exports = 0;
  await page.route("**/console/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (url.searchParams.get("format") === "csv") {
      exports++;
      expect(url.searchParams.get("from")).toBe("2026-10-01");
      expect(url.searchParams.get("to")).toBe("2026-10-22");
      await new Promise((resolve) => setTimeout(resolve, 400));
      return route
        .fulfill({
          status: 200,
          contentType: "text/csv",
          headers: { "Content-Disposition": "attachment" },
          body: '\uFEFF"day"\r\n"2026-10-01"',
        })
        .catch(() => {});
    }
    return route.fulfill(
      ok({
        columns: [{ key: "day", label: "日期" }],
        items: [{ day: "2026-10-01" }],
        total: 1,
      }),
    );
  });
  let downloads = 0;
  page.on("download", () => downloads++);
  await page.goto("/admin/transfer/channel-stat");
  await page.getByLabel("开始日期", { exact: true }).fill("2026-10-01");
  await page.getByLabel("结束日期", { exact: true }).fill("2026-10-22");
  await expect(
    page.getByRole("button", { name: "导出本页 CSV", exact: true }),
  ).toBeEnabled();
  await page
    .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "取消导出", exact: true }),
  ).toBeVisible();
  await page.getByLabel("开始日期", { exact: true }).fill("2026-10-02");
  await expect(
    page.getByRole("button", { name: "取消导出", exact: true }),
  ).toHaveCount(0);
  await page.waitForTimeout(500);
  expect(exports).toBe(1);
  expect(downloads).toBe(0);
});
