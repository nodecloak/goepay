import { test, expect } from "@playwright/test";
import { chooseKitOption } from "./kit-select-helpers";
for (const area of ["admin", "merchant"] as const)
  for (const width of [1280, 375]) {
    test(`original ${area} transfer filters keep applied CSV scope at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const reads: URL[] = [],
        exports: URL[] = [];
      let writes = 0;
      await page.route("**/console/v1/**", (route) => {
        const r = route.request(),
          u = new URL(r.url()),
          p = u.pathname;
        if (r.method() !== "GET") writes++;
        if (p.endsWith("/transfers")) {
          if (u.searchParams.get("format") === "csv") {
            exports.push(u);
            return route.fulfill({
              status: 200,
              contentType: "text/csv",
              headers: {
                "Content-Disposition": 'attachment; filename="local151.csv"',
              },
              body: "\ufeffid,merchant_id\n1,1\n",
            });
          }
          reads.push(u);
        }
        const data = p.endsWith("/session")
          ? {
              user: { id: "1", role: area, admin_class: "super" },
              csrf_token: "transfer151",
            }
          : { items: [], total: 0 };
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(
        area === "admin" ? "/admin/transfer/list" : "/merchant/transfer",
      );
      await expect(page.getByLabel("搜索字段", { exact: true })).toBeVisible({
        timeout: 3000,
      });
      await expect(
        page.getByRole("button", { name: "搜索字段", exact: true }),
      ).toContainText("交易号");
      await page.getByRole("button", { name: "展开", exact: true }).click();
      await expect(page.getByLabel("开始日期")).toBeVisible();
      expect(reads).toHaveLength(1);
      await chooseKitOption(
        page,
        page.getByRole("button", { name: "搜索字段", exact: true }),
        "付款金额",
      );
      await page
        .getByLabel("搜索内容", { exact: true })
        .fill("90071992547409.93");
      await chooseKitOption(
        page,
        page.getByRole("button", { name: "状态", exact: true }),
        "结果待核对",
      );
      await page.getByLabel("开始日期").fill("2026-10-01");
      await page.getByLabel("结束日期").fill("2026-10-26");
      if (area === "admin") {
        await page
          .getByLabel("商户号", { exact: true })
          .fill("9223372036854775807");
        await page
          .getByLabel("通道ID", { exact: true })
          .fill("9007199254740993");
      } else {
        await expect(page.getByLabel("商户号", { exact: true })).toHaveCount(0);
        await expect(page.getByLabel("通道ID", { exact: true })).toHaveCount(0);
      }
      expect(reads).toHaveLength(1);
      let download = page.waitForEvent("download");
      await page
        .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
        .click();
      await download;
      expect(exports[0]!.searchParams.get("q")).toBe("");
      expect(exports[0]!.searchParams.has("status")).toBe(false);
      const response = page.waitForResponse(
        (r) =>
          new URL(r.url()).pathname.endsWith("/transfers") &&
          new URL(r.url()).searchParams.get("q") === "90071992547409.93",
      );
      await page.getByRole("button", { name: "查询", exact: true }).click();
      await response;
      const applied = reads.at(-1)!;
      expect(applied.searchParams.get("search_field")).toBe("money");
      expect(applied.searchParams.get("status")).toBe("unknown");
      expect(applied.searchParams.get("from")).toBe("2026-10-01");
      expect(applied.searchParams.get("to")).toBe("2026-10-26");
      if (area === "admin") {
        expect(applied.searchParams.get("merchant_id")).toBe(
          "9223372036854775807",
        );
        expect(applied.searchParams.get("channel_id")).toBe("9007199254740993");
      }
      download = page.waitForEvent("download");
      await page
        .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
        .click();
      await download;
      expect(exports.at(-1)!.searchParams.get("q")).toBe("90071992547409.93");
      expect(exports.at(-1)!.searchParams.get("status")).toBe("unknown");
      await page.getByLabel("搜索内容", { exact: true }).fill("1.001");
      await expect(
        page.getByRole("button", { name: "查询", exact: true }),
      ).toBeDisabled();
      await expect(
        page.getByRole("button", { name: "导出筛选结果 CSV", exact: true }),
      ).toBeDisabled();
      await expect(page.getByRole("alert")).toContainText("付款金额");
      expect(reads).toHaveLength(2);
      const reset = page.waitForResponse(
        (r) =>
          new URL(r.url()).pathname.endsWith("/transfers") &&
          new URL(r.url()).searchParams.get("q") === "",
      );
      await page.getByRole("button", { name: "重置", exact: true }).click();
      await reset;
      await expect(page.getByLabel("搜索内容", { exact: true })).toHaveValue(
        "",
      );
      expect(reads.at(-1)!.searchParams.has("status")).toBe(false);
      expect(writes).toBe(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
    });
  }
