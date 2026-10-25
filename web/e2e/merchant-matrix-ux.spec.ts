import { test, expect } from "@playwright/test";

for (const width of [375, 1280])
  test(`configured zero channels remain sortable with no fabricated merchant rows at ${width}`, async ({
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
          user: { id: "1", name: "admin", role: "admin", admin_class: "super" },
          csrf_token: "zero-matrix",
        };
      else if (u.pathname.includes("/reports/")) {
        reads.push(u);
        const channel = u.searchParams.get("method") === "channel",
          key = channel ? "channel_9223372036854775807" : "type_alipay",
          label = channel ? "通道 #9223372036854775807" : "支付宝";
        data = {
          columns: [
            { key: "uid", label: "商户ID" },
            { key: "total", label: "总计" },
            {
              key,
              label,
            },
          ],
          items: [],
          summary: {
            uid: "总计",
            total: "0.00",
            [key]: "0.00",
          },
          total: 0,
          missing_costs: "0",
          source: "local_verified_payment_facts",
        };
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/trade/stat/user");
    await page.getByLabel("查看方式", { exact: true }).selectOption("channel");
    await page.getByRole("button", { name: "查询统计", exact: true }).click();
    await expect(
      page.getByRole("columnheader", {
        name: "通道 #9223372036854775807",
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "0.00", exact: true }),
    ).toHaveCount(2);
    await expect(page.getByRole("link", { name: /^\d+$/ })).toHaveCount(0);
    await page
      .getByRole("button", {
        name: "按通道 #9223372036854775807排序",
        exact: true,
      })
      .click();
    await expect
      .poll(() => reads.at(-1)?.searchParams.get("sort"))
      .toBe("channel_9223372036854775807");
    expect(writes).toHaveLength(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });

for (const transfer of [false, true])
  for (const width of [375, 1280])
    test(`original ${transfer ? "transfer" : "payment"} merchant matrix controls and exact totals at ${width}`, async ({
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
        let data: any = { items: [], total: 0 };
        if (p.endsWith("/session"))
          data = {
            user: {
              id: "1",
              name: "admin",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "matrix-csrf",
          };
        else if (p.endsWith("/security/payment-password"))
          data = {
            configured: false,
            enforced: false,
            credential_epoch: 0,
            totp_required: false,
          };
        else if (p.includes("/reports/")) {
          reads.push(u);
          if (u.searchParams.get("format") === "csv")
            return route.fulfill({
              contentType: "text/csv; charset=utf-8",
              headers: {
                "Content-Disposition": "attachment; filename=matrix.csv",
              },
              body: "\ufeff商户ID,总计\r\n总计,22.30\r\n1,22.30",
            });
          const key =
              u.searchParams.get("method") === "channel"
                ? "channel_7"
                : "type_alipay",
            unknown = u.searchParams.get("type") === "3";
          data = {
            columns: [
              { key: "uid", label: "商户ID" },
              { key: "total", label: "总计" },
              { key, label: key === "channel_7" ? "通道 #7" : "支付宝" },
            ],
            items: [
              {
                uid: "1",
                total: unknown ? null : "90071992547409.93",
                [key]: unknown ? null : "90071992547409.93",
              },
            ],
            summary: {
              uid: "总计",
              total: unknown ? null : "90071992547409.93",
              [key]: unknown ? null : "90071992547409.93",
            },
            total: 1,
            missing_costs: unknown ? "2" : "0",
            source: "local_verified_payment_facts",
          };
        }
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(
        transfer ? "/admin/transfer/user-stat" : "/admin/trade/stat/user",
      );
      await expect(page.getByLabel("查看方式", { exact: true })).toHaveCount(1);
      await expect
        .poll(() => reads.at(-1)?.searchParams.get("view"))
        .toBe("matrix");
      await expect(
        page
          .getByRole("cell", { name: "90071992547409.93", exact: true })
          .first(),
      ).toBeVisible();
      await expect
        .poll(
          async () =>
            (await page.locator(".matrix-filters").boundingBox())!.height,
        )
        .toBeLessThan(width === 375 ? 360 : 170);
      await expect(
        page.getByRole("link", { name: "1", exact: true }),
      ).toHaveAttribute("href", "/admin/merchant/merchant-list?uid=1");
      await page
        .getByRole("button", { name: "按商户ID排序", exact: true })
        .click();
      await expect
        .poll(() => reads.at(-1)?.searchParams.get("sort"))
        .toBe("uid");
      expect(reads.at(-1)!.searchParams.get("direction")).toBe("asc");
      const before = reads.length;
      await page
        .getByLabel("查看方式", { exact: true })
        .selectOption("channel");
      expect(reads).toHaveLength(before);
      await expect(
        page.getByRole("button", { name: "导出筛选结果 CSV", exact: true }),
      ).toBeDisabled();
      await page.getByRole("button", { name: "查询统计", exact: true }).click();
      await expect(
        page.getByRole("columnheader", { name: "通道 #7", exact: true }),
      ).toBeVisible();
      expect(reads.at(-1)!.searchParams.get("method")).toBe("channel");
      if (!transfer) {
        await page.getByLabel("统计类型", { exact: true }).selectOption("3");
        await page
          .getByRole("button", { name: "查询统计", exact: true })
          .click();
        await expect(page.getByRole("status")).toContainText("2");
        await expect(
          page.getByRole("cell", { name: "未知", exact: true }).first(),
        ).toBeVisible();
      } else
        expect(await page.getByLabel("统计类型", { exact: true }).count()).toBe(
          0,
        );
      await Promise.all([
        page.waitForEvent("download"),
        page
          .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
          .click(),
      ]);
      expect(reads.at(-1)!.searchParams.get("view")).toBe("matrix");
      expect(reads.at(-1)!.searchParams.get("format")).toBe("csv");
      expect(writes).toHaveLength(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
