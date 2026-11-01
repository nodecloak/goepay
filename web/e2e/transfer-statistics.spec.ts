import { test, expect } from "@playwright/test";

for (const width of [375, 1280])
  test(`transfer statistics use the full applied owner scope at ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    const reads: URL[] = [];
    await page.route("**/console/v1/**", async (route) => {
      const url = new URL(route.request().url());
      let data: unknown = { items: [], total: 0 };
      if (url.pathname.endsWith("/session"))
        data = {
          user: {
            role: "admin",
            id: "1",
            name: "Synthetic Admin",
            admin_class: "super",
          },
          csrf_token: "synthetic-statistics",
        };
      else if (url.pathname.endsWith("/transfers/statistics")) {
        reads.push(url);
        data = {
          count: "45",
          amount: "9007199254740993",
          fee_amount: "7",
          cost_amount: "9007199254741000",
          succeeded_count: "3",
          failed_count: "2",
          pending_count: "40",
        };
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/transfer/list?merchant_id=751");
    await expect(page.getByText("暂无数据", { exact: true })).toBeInViewport();
    await page.getByLabel("商户号", { exact: true }).fill("752");
    await page.getByRole("button", { name: "统计", exact: true }).click();
    const stats = page.getByRole("region", { name: "付款统计", exact: true });
    await expect(stats).toContainText("90071992547409.93");
    await expect(stats).toContainText("45");
    expect(reads).toHaveLength(1);
    expect(reads[0].searchParams.get("merchant_id")).toBe("751");
    expect(reads[0].searchParams.has("page")).toBe(false);
    expect(reads[0].searchParams.has("limit")).toBe(false);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`transfer-statistics-${width}.png`),
      fullPage: true,
    });
    await stats.getByRole("button", { name: "关闭统计", exact: true }).click();
    await expect(stats).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "统计", exact: true }),
    ).toBeFocused();
  });
