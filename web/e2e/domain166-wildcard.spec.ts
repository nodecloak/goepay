import { test, expect } from "@playwright/test";
import schemas from "./domain166-schemas.json" with { type: "json" };
for (const area of ["admin", "merchant"] as const)
  for (const width of [1440, 375])
    test(`domain166 ${area} wildcard form and original owner approval ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 1000 });
      const saved: Record<string, unknown>[] = [];
      let rows: Record<string, unknown>[] = [];
      const errors: string[] = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", (route) => {
        const r = route.request(),
          u = new URL(r.url());
        if (!["127.0.0.1", "localhost"].includes(u.hostname))
          return route.abort();
        if (!u.pathname.startsWith("/console/v1/")) return route.continue();
        let data: unknown = { items: [], total: 0 };
        if (u.pathname.endsWith("/session"))
          data = {
            user: {
              id: "1",
              name: "synthetic domain166",
              role: area,
              admin_class: "super",
            },
            csrf_token: "synthetic166",
          };
        else if (u.pathname.endsWith("/resources/domains/schema"))
          data = schemas[area];
        else if (
          u.pathname.endsWith("/resources/domains") &&
          r.method() === "GET"
        )
          data = { items: rows, total: rows.length };
        else if (
          u.pathname.includes("/resources/domains") &&
          ["POST", "PATCH"].includes(r.method())
        ) {
          const payload = r.postDataJSON();
          saved.push(payload);
          const record = {
            id: "9007199254740993",
            merchant_id: "1",
            domain: String(payload.domain).toLowerCase().replace(/\.$/, ""),
            approved: area === "admin" ? payload.approved === true : false,
            enabled: true,
          };
          rows = [record];
          data = record;
        } else if (/\/resources\/domains\/\d+$/.test(u.pathname))
          data = rows[0];
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(
        area === "admin" ? "/admin/merchant/domain" : "/merchant/domain",
      );
      await page
        .getByRole("button", { name: "新增授权域名", exact: true })
        .click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toContainText("*.example.com");
      await expect(dialog).toContainText("不含根域名");
      if (area === "admin")
        await dialog.locator("#field-merchant_id").fill("1");
      await dialog.locator("#field-domain").fill("*.Example.COM.");
      if (area === "admin") await dialog.locator("#field-approved").check();
      else await expect(dialog.locator("#field-approved")).toHaveCount(0);
      await page.screenshot({
        path: info.outputPath(`domain166-${area}-${width}-form.png`),
        fullPage: true,
      });
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toHaveCount(0);
      expect(saved[0]!.domain).toBe("*.Example.COM.");
      await expect(page.locator("table")).toContainText("*.example.com");
      await expect(page.locator("table")).toContainText("9007199254740993");
      await expect(
        page.getByRole("columnheader", {
          name: area === "admin" ? "商户号" : "商户 ID",
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        page.getByRole("columnheader", {
          name: area === "admin" ? "状态" : "审核通过",
          exact: true,
        }),
      ).toBeVisible();
      const firstRow = page.locator("table tbody tr").first();
      await expect(firstRow.locator("td").nth(1)).toHaveText("1");
      await expect(firstRow.locator("td").nth(3)).toHaveText(
        area === "admin" ? "已审核" : "待审核",
      );
      await page
        .getByRole("button", { name: "编辑", exact: true })
        .first()
        .click();
      await expect(dialog.locator("#field-domain")).toHaveValue(
        "*.example.com",
      );
      await dialog.locator("#field-domain").fill("*.Changed.Example.COM.");
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toHaveCount(0);
      expect(saved[1]!.domain).toBe("*.Changed.Example.COM.");
      await expect(page.locator("table")).toContainText(
        "*.changed.example.com",
      );
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
      await page.screenshot({
        path: info.outputPath(`domain166-${area}-${width}-saved.png`),
        fullPage: true,
      });
      expect(errors).toEqual([]);
    });
