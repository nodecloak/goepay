import { expect, test } from "@playwright/test";
import { chooseKitOption } from "./kit-select-helpers";

for (const width of [1280, 375]) {
  for (const kind of ["orders", "refunds"] as const) {
    test(`merchant ${kind} primary filters match the reference and apply only on query at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const queries: URL[] = [],
        writes: string[] = [];
      await page.route("**/console/v1/**", (route) => {
        const request = route.request(),
          url = new URL(request.url());
        if (request.method() !== "GET") {
          writes.push(url.pathname);
          return route.abort();
        }
        if (url.pathname.endsWith("/" + kind)) queries.push(url);
        const data = url.pathname.endsWith("/session")
          ? {
              user: {
                id: "1000",
                role: "merchant",
                name: "Synthetic merchant",
              },
              csrf_token: "synthetic-filters-224",
            }
          : { items: [], total: 0 };
        return route.fulfill({ json: { code: 0, data } });
      });
      await page.goto(
        "/merchant/trade/" + (kind === "orders" ? "order" : "refund"),
      );
      await expect.poll(() => queries.length).toBeGreaterThan(0);
      const originalReads = queries.length;
      const payment = page.getByRole("button", {
        name: "支付方式筛选",
        exact: true,
      });
      const from = page.getByLabel("创建开始日期", { exact: true });
      const to = page.getByLabel("创建结束日期", { exact: true });
      if (kind === "orders") {
        await expect(payment).toBeVisible();
        await chooseKitOption(page, payment, "支付宝");
      } else {
        await expect(from).toBeVisible();
        await expect(to).toBeVisible();
        await from.fill("2026-10-01");
        await to.fill("2026-10-12");
      }
      expect(queries).toHaveLength(originalReads);
      await page.getByRole("button", { name: "查询", exact: true }).click();
      await expect.poll(() => queries.length).toBe(originalReads + 1);
      expect(Object.fromEntries(queries.at(-1)!.searchParams)).toEqual({
        page: "1",
        limit: "20",
        ...(kind === "orders"
          ? { type: "alipay" }
          : { from: "2026-10-01", to: "2026-10-12" }),
      });
      await page.getByRole("button", { name: "展开", exact: true }).click();
      await expect(payment).toHaveCount(1);
      await expect(from).toHaveCount(1);
      await expect(to).toHaveCount(1);
      await page.getByRole("button", { name: "收起", exact: true }).click();
      if (kind === "orders") await expect(payment).toContainText("支付宝");
      else {
        await expect(from).toHaveValue("2026-10-01");
        await expect(to).toHaveValue("2026-10-12");
      }
      await page.getByRole("button", { name: "重置", exact: true }).click();
      await expect
        .poll(() => Object.fromEntries(queries.at(-1)!.searchParams))
        .toEqual({ page: "1", limit: "20" });
      if (kind === "orders") await expect(payment).toContainText("全部方式");
      else {
        await expect(from).toHaveValue("");
        await expect(to).toHaveValue("");
      }
      await expect(page.getByLabel("商户号筛选", { exact: true })).toHaveCount(
        0,
      );
      expect(writes).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }
}
