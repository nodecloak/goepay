import { test, expect } from "@playwright/test";
for (const role of ["admin", "merchant"] as const)
  for (const width of [1280, 375])
    test(`original private transfer fields bind applied list and CSV for ${role} at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const reads: URL[] = [],
        exports: URL[] = [];
      let writes = 0;
      await page.route("**/console/v1/**", (route) => {
        const r = route.request(),
          u = new URL(r.url());
        if (r.method() !== "GET") writes++;
        if (u.pathname.endsWith("/transfers")) {
          if (u.searchParams.get("format") === "csv") {
            exports.push(u);
            return route.fulfill({
              status: 200,
              contentType: "text/csv",
              headers: {
                "Content-Disposition": 'attachment; filename="local158.csv"',
              },
              body: "\ufeffid,merchant_id\n1,1\n",
            });
          }
          reads.push(u);
        }
        const data = u.pathname.endsWith("/session")
          ? {
              user: { id: "1", role, admin_class: "super" },
              csrf_token: "local158",
            }
          : { items: [], total: 0 };
        return route.fulfill({ json: { code: 0, data } });
      });
      await page.goto(
        role === "admin" ? "/admin/transfer/list" : "/merchant/transfer",
      );
      await expect(page.getByLabel("搜索字段", { exact: true })).toBeVisible();
      for (const [field, label, value] of [
        ["account", "付款账号", "LOCAL158@"],
        ["username", "姓名", "本地姓名158"],
        ["desc", "备注", "本地备注 %_158"],
      ]) {
        const n = reads.length;
        await page
          .getByLabel("搜索字段", { exact: true })
          .selectOption({ label });
        await page.getByLabel("搜索付款记录", { exact: true }).fill(value);
        expect(reads.length).toBe(n);
        await page.getByRole("button", { name: "查询", exact: true }).click();
        await expect.poll(() => reads.length).toBe(n + 1);
        expect(reads.at(-1)!.searchParams.get("search_field")).toBe(field);
        expect(reads.at(-1)!.searchParams.get("q")).toBe(value);
        await page
          .getByLabel("搜索付款记录", { exact: true })
          .fill("changed-unapplied-private");
        const download = page.waitForEvent("download");
        await page
          .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
          .click();
        await download;
        expect(exports.at(-1)!.searchParams.get("search_field")).toBe(field);
        expect(exports.at(-1)!.searchParams.get("q")).toBe(value);
        expect(page.url()).not.toContain(encodeURIComponent(value));
      }
      expect(writes).toBe(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
    });
