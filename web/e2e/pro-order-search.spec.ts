import { test, expect } from "@playwright/test";

for (const area of ["admin", "merchant"] as const) {
  test(`${area} Pro order fields preserve exact money and notification filters in list and CSV`, async ({
    page,
  }) => {
    const queries: URL[] = [];
    await page.setViewportSize(
      area === "admin"
        ? { width: 1280, height: 900 }
        : { width: 375, height: 812 },
    );
    await page.route("**/console/v1/**", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith("/session")) {
        return route.fulfill({
          json: {
            code: 0,
            data: {
              user: {
                id: "1000",
                name: "local fixture",
                role: area,
                admin_class: "super",
              },
              csrf_token: "fixture-only",
            },
          },
        });
      }
      queries.push(url);
      if (url.searchParams.get("format") === "csv") {
        return route.fulfill({
          status: 200,
          contentType: "text/csv; charset=utf-8",
          body: "trade_no\nlocal-fixture\n",
        });
      }
      return route.fulfill({
        json: { code: 0, data: { items: [], total: 0 } },
      });
    });
    await page.goto(`/${area}/trade/order`);
    const fields = page.getByLabel("订单搜索字段", { exact: true });
    // 订单搜索字段 is a KitSelect: open it, assert all options, pick by label.
    await fields.click();
    await expect(page.getByRole("option")).toHaveCount(12);
    await page.getByRole("option", { name: "实付金额（元）", exact: true }).click();
    await page
      .getByLabel("搜索内容", { exact: true })
      .fill("92233720368547758.07");
    await page.getByRole("button", { name: "展开", exact: true }).click();
    await page.getByLabel("通知状态筛选", { exact: true }).click();
    await page.getByRole("option", { name: "通知失败", exact: true }).click();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect
      .poll(() => queries.at(-1)?.searchParams.get("search_field"))
      .toBe("realmoney");
    expect(queries.at(-1)?.searchParams.get("q")).toBe("92233720368547758.07");
    expect(queries.at(-1)?.searchParams.get("notification_state")).toBe("dead");
    await page
      .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
      .click();
    await expect
      .poll(() =>
        queries
          .findLast((x) => x.searchParams.get("format") === "csv")
          ?.searchParams.get("search_field"),
      )
      .toBe("realmoney");
    const exported = queries.findLast(
      (x) => x.searchParams.get("format") === "csv",
    )!;
    expect(exported.searchParams.get("q")).toBe("92233720368547758.07");
    expect(exported.searchParams.get("notification_state")).toBe("dead");
    await fields.click();
    await page.getByRole("option", { name: "商户订单号", exact: true }).click();
    await page.getByLabel("搜索内容", { exact: true }).fill(" INV ");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect
      .poll(() => queries.at(-1)?.searchParams.get("q"))
      .toBe(" INV ");
    await page
      .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
      .click();
    await expect
      .poll(() =>
        queries
          .findLast((x) => x.searchParams.get("format") === "csv")
          ?.searchParams.get("q"),
      )
      .toBe(" INV ");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const input = await page
      .getByLabel("搜索内容", { exact: true })
      .boundingBox();
    expect(input!.width).toBeGreaterThan(140);
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await expect(fields).toContainText("综合搜索");
    await expect
      .poll(() => queries.at(-1)?.searchParams.has("search_field"))
      .toBe(false);
    await expect
      .poll(() => queries.at(-1)?.searchParams.has("notification_state"))
      .toBe(false);
    if (area === "merchant")
      await page.getByRole("button", { name: "展开菜单", exact: true }).click();
    await page.getByRole("link", { name: "退款记录", exact: true }).click();
    await expect(fields).toHaveCount(0);
  });
}
