import { test, expect } from "@playwright/test";
for (const area of ["admin", "merchant"])
  for (const width of [1280, 375])
    test(`${area} refunds use original six-field statistics and current filters at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const reads: URL[] = [],
        writes: string[] = [];
      await page.route("**/console/v1/**", (route) => {
        const req = route.request(),
          u = new URL(req.url()),
          p = u.pathname;
        if (req.method() !== "GET") writes.push(p);
        let data: unknown = { items: [], total: 0 };
        if (p.endsWith("/session"))
          data = {
            user: {
              id: "1",
              name: "local",
              role: area,
              admin_class: area === "admin" ? "super" : undefined,
            },
            csrf_token: "refund127",
          };
        else if (p.endsWith("/refunds/summary")) {
          reads.push(u);
          data = {
            totalCount: "9007199254740993",
            totalMoney: "90071992547409.93",
            successCount: "1",
            successMoney: "0.01",
            pendingCount: "2",
            pendingMoney: "0.80",
          };
        }
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto("/" + area + "/trade/refund");
      await page.getByLabel("搜索内容", { exact: true }).fill("原退款-127");
      const opener = page.getByRole("button", {
        name: "退款统计",
        exact: true,
      });
      await opener.click();
      const dialog = page.getByRole("dialog", {
        name: "退款统计",
        exact: true,
      });
      await expect(
        dialog.getByText("9007199254740993", { exact: true }),
      ).toBeVisible();
      await expect(
        dialog.getByText("¥90071992547409.93", { exact: true }),
      ).toBeVisible();
      for (const label of [
        "退款总笔数",
        "退款总金额",
        "成功笔数",
        "成功金额",
        "处理中笔数",
        "处理中金额",
      ])
        await expect(dialog.getByText(label, { exact: true })).toBeVisible();
      expect(reads).toHaveLength(1);
      expect(reads[0]!.searchParams.get("q")).toBe("原退款-127");
      expect(reads[0]!.searchParams.has("page")).toBe(false);
      expect(reads[0]!.searchParams.has("limit")).toBe(false);
      const box = await dialog.boundingBox();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(width);
      await dialog
        .getByRole("button", { name: "关闭", exact: true })
        .press("Escape");
      await expect(dialog).toHaveCount(0);
      await expect(opener).toBeFocused();
      expect(writes).toHaveLength(0);
    });
