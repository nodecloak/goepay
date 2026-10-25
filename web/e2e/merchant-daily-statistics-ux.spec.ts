import { test, expect } from "@playwright/test";
const total = {
  day: "合计",
  realmoney: "100.50",
  getmoney: "90.45",
  refundmoney: "12.00",
  orderCount: "3",
  successCount: "2",
  successRate: "66.67",
};
const items = Array.from({ length: 7 }, (_, i) => ({
  day: `2026-10-${20 + i}`,
  realmoney: i === 5 ? "100.50" : "0.00",
  getmoney: i === 5 ? "90.45" : "0.00",
  refundmoney: i === 5 ? "12.00" : "0.00",
  orderCount: i === 5 ? "3" : "0",
  successCount: i === 5 ? "2" : "0",
  successRate: i === 5 ? "66.67" : null,
  profitmoney: "private-sentinel",
}));
for (const width of [1280, 375])
  test(`merchant original daily restores income, weighted footer and type-scoped CSV at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const reads: URL[] = [],
      writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        u = new URL(req.url());
      if (req.method() !== "GET") writes.push(u.pathname);
      let data: unknown = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "merchant" },
          csrf_token: "merchant137",
        };
      else if (u.pathname.endsWith("/reports/daily")) {
        reads.push(u);
        if (u.searchParams.get("format") === "csv")
          return route.fulfill({
            contentType: "text/csv;charset=utf-8",
            headers: {
              "Content-Disposition": "attachment; filename=merchant-daily.csv",
            },
            body: "\ufeff日期,订单金额,分成金额,退款金额,总订单数,成功订单数,支付成功率\n合计,100.50,90.45,12.00,3,2,66.67\n",
          });
        data = { items, summary: total, total: 7 };
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/merchant/trade/daily-stat");
    await expect(page.getByRole("columnheader")).toHaveText([
      "日期",
      "订单金额",
      "分成金额",
      "退款金额",
      "总订单数",
      "成功订单数",
      "支付成功率",
    ]);
    await expect(page.getByRole("row")).toHaveCount(9);
    await expect(page.locator("tfoot")).toContainText("合计");
    await expect(page.locator("tfoot")).toContainText("90.45");
    await expect(page.locator("tfoot")).toContainText("66.67%");
    await expect(
      page.getByRole("img", { name: "近7天交易趋势" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "分成金额", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("private-sentinel", { exact: true }),
    ).toHaveCount(0);
    await expect(page.getByText("手续费利润", { exact: true })).toHaveCount(0);
    await expect(page.getByLabel("商户号", { exact: true })).toHaveCount(0);
    await expect(page.getByLabel("通道ID", { exact: true })).toHaveCount(0);
    await expect(page.getByLabel("查询日期（开始）")).toHaveCount(0);
    await page.getByLabel("支付方式", { exact: true }).selectOption("alipay");
    await expect(page.locator("tfoot")).toHaveCount(0);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect
      .poll(
        () =>
          reads.filter((u) => u.searchParams.get("type") === "alipay").length,
      )
      .toBe(1);
    await expect(page.locator("tfoot")).toBeVisible();
    const pending = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
      .click();
    await pending;
    await expect
      .poll(() =>
        reads.some(
          (u) =>
            u.searchParams.get("format") === "csv" &&
            u.searchParams.get("type") === "alipay" &&
            u.searchParams.get("view") === "original",
        ),
      )
      .toBe(true);
    expect(
      reads.every(
        (u) =>
          !u.searchParams.has("uid") &&
          !u.searchParams.has("channel") &&
          !u.searchParams.has("from") &&
          !u.searchParams.has("to"),
      ),
    ).toBe(true);
    expect(writes).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
