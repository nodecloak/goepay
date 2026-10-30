import { test, expect } from "@playwright/test";
import fs from "node:fs/promises";
for (const area of ["admin", "merchant"])
  for (const width of [1280, 375])
    test(`${area} original refund identifiers and search links share exact summary and CSV filters at ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 1000 });
      const reads: URL[] = [],
        writes: string[] = [],
        big = "9223372036854775807",
        external = " shared-refund-request ";
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
            csrf_token: "refund128",
          };
        else if (p.endsWith("/refunds/summary")) {
          reads.push(u);
          data = {
            totalCount: "1",
            totalMoney: "12.34",
            successCount: "0",
            successMoney: "0.00",
            pendingCount: "1",
            pendingMoney: "12.34",
          };
        } else if (p.endsWith("/refunds")) {
          reads.push(u);
          if (u.searchParams.get("format") === "csv")
            return route.fulfill({
              contentType: "text/csv; charset=utf-8",
              headers: {
                "Content-Disposition": "attachment; filename=refunds.csv",
              },
              body:
                "\ufeffplatform_refund_no,out_refund_no\r\n" +
                big +
                "," +
                external +
                "\r\n",
            });
          data = {
            items: [
              {
                id: big,
                platform_refund_no: big,
                out_refund_no: external,
                refund_no: "legacy-console-key",
                trade_no: "1000000000000000000",
                merchant_id: "1",
                amount: "1234",
                status: "pending",
                created_at: "2026-10-25T00:00:00Z",
              },
            ],
            total: 1,
          };
        }
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(
        "/" +
          area +
          "/trade/refund?column=out_refund_no&value=" +
          encodeURIComponent(external) +
          "&uid=1",
      );
      const field = page.getByLabel("退款搜索字段", { exact: true }),
        search = page.getByLabel("搜索内容", { exact: true });
      // 退款搜索字段 is a KitSelect: its trigger shows the selected option's label.
      await expect(field).toContainText("商户退款单号");
      await expect(search).toHaveValue(external);
      await expect(page.getByText(big, { exact: true })).toBeVisible();
      await expect(
        page.getByRole("columnheader").filter({ hasText: "退款单号" }),
      ).toContainText("商户退款单号");
      await expect(
        page.getByText("legacy-console-key", { exact: true }),
      ).toHaveCount(0);
      expect(reads.at(-1)!.searchParams.get("q")).toBe(external);
      expect(reads.at(-1)!.searchParams.get("search_field")).toBe(
        "out_refund_no",
      );
      await field.click();
      await page.getByRole("option", { name: "退款单号", exact: true }).click();
      await search.fill(big);
      await page.getByRole("button", { name: "查询", exact: true }).click();
      await expect
        .poll(() => reads.at(-1)!.searchParams.get("search_field"))
        .toBe("refund_no");
      await page.getByRole("button", { name: "退款统计", exact: true }).click();
      const dialog = page.getByRole("dialog", {
        name: "退款统计",
        exact: true,
      });
      await expect(
        dialog.getByText("¥12.34", { exact: true }).first(),
      ).toBeVisible();
      expect(reads.at(-1)!.pathname).toMatch(/refunds\/summary$/);
      expect(reads.at(-1)!.searchParams.get("search_field")).toBe("refund_no");
      expect(reads.at(-1)!.searchParams.get("q")).toBe(big);
      await dialog.getByRole("button", { name: "关闭", exact: true }).click();
      const downloaded = page.waitForEvent("download");
      await page
        .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
        .click();
      const download = await downloaded,
        path = info.outputPath("refund-identifiers.csv");
      await download.saveAs(path);
      expect(await fs.readFile(path, "utf8")).toContain(big + "," + external);
      expect(reads.at(-1)!.searchParams.get("search_field")).toBe("refund_no");
      expect(reads.at(-1)!.searchParams.get("q")).toBe(big);
      expect(writes).toHaveLength(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
    });
