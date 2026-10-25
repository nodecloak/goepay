import { test, expect } from "@playwright/test";
import { transferListFixture as fixture } from "../tests/fixtures/transfer-list";
for (const area of ["admin", "merchant"] as const)
  for (const width of [1280, 375]) {
    test(`original ${area} transfer list columns and historical state at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      let writes = 0;
      const rows = [
        fixture(),
        fixture({
          id: "9007199254740994",
          biz_no: "1000000000000000002",
          type: "bank",
          status: "reversed",
          ledger_status: "succeeded",
          bank_lifecycle: "reversed",
        }),
      ];
      await page.route("**/console/v1/**", (route) => {
        const r = route.request(),
          p = new URL(r.url()).pathname;
        if (r.method() !== "GET") writes++;
        let data: unknown = { items: rows, total: 2 };
        if (p.endsWith("/session"))
          data = {
            user: {
              id: "9223372036854775807",
              role: area,
              admin_class: area === "admin" ? "super" : undefined,
            },
            csrf_token: "transfer150",
          };
        if (p.endsWith("/transfers/" + rows[0]!.id))
          data = {
            transfer: rows[0],
            recipient: { account: "***0150", name: "***" },
          };
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
        ...(area === "admin" ? ["交易号", "商户号"] : ["交易号"]),
        "付款方式",
        "付款账号",
        "付款金额",
        "提交时间",
        "状态",
        "操作",
      ]);
      const row = table.locator('tr[data-transfer-id="9007199254740993"]');
      await expect(row).toContainText("90071992547409.93");
      await expect(row).toContainText("2026-10-25 23:30:00");
      await expect(row).toContainText("***0150");
      await expect(row).toContainText("正在处理");
      await expect(
        table.locator('tr[data-transfer-id="9007199254740994"]'),
      ).toContainText("已退回");
      const details = row.getByRole("button", {
        name: "付款详情",
        exact: true,
      });
      await details.click();
      const modal = page.getByRole("dialog", { name: "付款详情", exact: true });
      await expect(modal).toBeVisible();
      await expect(modal).toContainText("付款手续费");
      await expect(modal).toContainText("9007199254740993");
      await modal
        .getByRole("button", { name: "关闭详情", exact: true })
        .focus();
      await page.keyboard.press("Escape");
      await expect(modal).toHaveCount(0);
      await expect(details).toBeFocused();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
      expect(writes).toBe(0);
    });
    test(`invalid complete ${area} transfer projection refuses actions at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      let detailsReads = 0;
      await page.route("**/console/v1/**", (route) => {
        const p = new URL(route.request().url()).pathname;
        if (/\/transfers\/[0-9]+$/.test(p)) detailsReads++;
        const data = p.endsWith("/session")
          ? {
              user: { id: "1", role: area, admin_class: "super" },
              csrf_token: "transfer150",
            }
          : {
              items: [fixture({ account_masked: "PRIVATE-MUST-NOT-RENDER" })],
              total: 1,
            };
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(
        area === "admin" ? "/admin/transfer/list" : "/merchant/transfer",
      );
      await expect(page.getByRole("alert")).toContainText("付款记录");
      await expect(
        page.getByRole("button", { name: "付款详情", exact: true }),
      ).toHaveCount(0);
      await expect(page.locator("main")).not.toContainText(
        "PRIVATE-MUST-NOT-RENDER",
      );
      expect(detailsReads).toBe(0);
    });
  }
