import { test, expect } from "@playwright/test";
import { transferListFixture } from "../tests/fixtures/transfer-list";
for (const area of ["admin", "merchant"] as const)
  for (const width of [1280, 375])
    test(`transfer165 ${area} original secondary fields ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 1000 });
      const errors: string[] = [],
        requests: string[] = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const remark = "原始转账业务备注".repeat(4);
      const rows = [
        transferListFixture({
          amount: "10000",
          fee_amount: "200",
          cost_amount: "10200",
          remark,
          status: "succeeded",
          pay_date: "2026-10-25 23:31:02",
        }),
        transferListFixture({
          id: "9007199254740994",
          biz_no: "1000000000000000002",
          amount: "10000",
          fee_amount: "200",
          cost_amount: "10200",
          remark: "含\t制表符",
          pay_date: "",
          status: "unknown",
        }),
      ];
      await page.route("**/console/v1/**", (route) => {
        requests.push(
          route.request().method() +
            " " +
            new URL(route.request().url()).pathname,
        );
        const data = route.request().url().includes("/session")
          ? {
              user: {
                id: "9223372036854775807",
                role: area,
                admin_class: "super",
              },
              csrf_token: "synthetic-transfer165",
            }
          : { items: rows, total: 2 };
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(
        area === "admin" ? "/admin/transfer/list" : "/merchant/transfer",
      );
      const table = page.locator(".original-transfer-list");
      await expect(table).toBeVisible();
      await expect(table.getByRole("columnheader")).toHaveText([
        "交易号",
        ...(area === "admin" ? ["商户号"] : []),
        "付款方式",
        "付款账号",
        "付款金额",
        "提交时间",
        "状态",
        "操作",
      ]);
      const row = table.locator('tr[data-transfer-id="9007199254740993"]');
      await expect(row).toContainText(remark);
      await expect(row).toContainText("花费金额：¥102.00");
      await expect(row).toContainText("付款时间：2026-10-25 23:31:02");
      await expect(row).toContainText("转账成功");
      const pending = table.locator('tr[data-transfer-id="9007199254740994"]');
      await expect(pending).toContainText("付款时间：未记录");
      await expect(pending).toContainText("结果待核对");
      expect(await pending.locator(".transfer-remark").textContent()).toBe(
        "备注：含\t制表符",
      );
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
      const note = row.locator(".transfer-remark");
      await expect(note).toBeVisible();
      const box = await note.boundingBox();
      expect(box!.width).toBeLessThanOrEqual(260);
      await page.screenshot({
        path: info.outputPath(`transfer165-${area}-${width}.png`),
        fullPage: true,
      });
      const scroll = page.locator(".original-transfer-list");
      if (width === 375) {
        for (const [field, index] of [
          ["remark", area === "admin" ? 2 : 1],
          ["cost", area === "admin" ? 4 : 3],
          ["paid-time", area === "admin" ? 5 : 4],
        ] as const) {
          await scroll.evaluate((e, i) => {
            const cell = e.querySelector("tbody tr")!.children[
              i
            ] as HTMLElement;
            e.scrollLeft = cell.offsetLeft;
          }, index);
          await page.screenshot({
            path: info.outputPath(`transfer165-${area}-${width}-${field}.png`),
            fullPage: true,
          });
        }
      }
      await scroll.evaluate((e) => (e.scrollLeft = e.scrollWidth));
      await expect(
        row.getByRole("button", { name: "付款详情", exact: true }),
      ).toBeVisible();
      await page.screenshot({
        path: info.outputPath(`transfer165-${area}-${width}-right.png`),
        fullPage: true,
      });
      expect(errors).toEqual([]);
      expect(requests.every((v) => v.startsWith("GET "))).toBe(true);
    });
