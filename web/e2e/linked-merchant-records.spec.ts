import { test, expect } from "@playwright/test";
for (const width of [1280, 375])
  for (const [path, api, label] of [
    ["/admin/transfer/list", "/transfers", "代付记录"],
    ["/admin/settle/list", "/workflows/settlements", "结算记录"],
  ]) {
    test(`original merchant ${label} navigation and same-path scope remain exact at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const reads: URL[] = [],
        writes: string[] = [];
      const id = "9223372036854775807";
      await page.route("**/console/v1/**", (route) => {
        const r = route.request(),
          u = new URL(r.url()),
          p = u.pathname;
        if (r.method() !== "GET") writes.push(p);
        let data: unknown = { items: [], total: 0 };
        if (p.endsWith("/session"))
          data = {
            user: { id: "1", role: "admin", admin_class: "operator" },
            csrf_token: "linked148",
          };
        else if (p.endsWith("/merchants/schema"))
          data = {
            title: "商户管理",
            fields: [{ name: "name", label: "名称", type: "string" }],
            permissions: { create: false, update: false, delete: false },
          };
        else if (p.endsWith("/resources/merchants"))
          data = { items: [{ id, name: "目标商户" }], total: 1 };
        else if (p === "/console/v1/admin" + api) {
          reads.push(u);
          if (u.searchParams.get("format") === "csv")
            return route.fulfill({
              contentType: "text/csv;charset=utf-8",
              headers: {
                "Content-Disposition": "attachment; filename=linked.csv",
              },
              body:
                "\ufeffid,merchant_id\r\n1," +
                u.searchParams.get("merchant_id"),
            });
        }
        return route.fulfill({ json: { code: 0, data } });
      });
      await page.goto("/admin/merchant/merchant-list");
      await page
        .getByRole("button", { name: "商户关联记录 " + id, exact: true })
        .click();
      const link = page.getByRole("menuitem", { name: label, exact: true });
      await expect(link).toHaveAttribute("href", path + "?uid=" + id);
      await page.evaluate(() => {
        (window as any).__linked_read_probe = true;
      });
      await link.click();
      expect(
        await page.evaluate(() => (window as any).__linked_read_probe),
      ).toBe(true);
      await expect
        .poll(() => reads.at(-1)?.searchParams.get("merchant_id"))
        .toBe(id);
      await expect(
        page.getByText("商户号：" + id, { exact: true }),
      ).toBeVisible();
      // Client-side navigation must remount the read context before loading the new owner.
      await page.evaluate((next) => {
        history.pushState({ ...history.state, current: next }, "", next);
        window.dispatchEvent(
          new PopStateEvent("popstate", { state: history.state }),
        );
      }, path + "?uid=2&page=2&limit=1");
      await expect
        .poll(() => reads.at(-1)?.searchParams.get("merchant_id"))
        .toBe("2");
      expect(reads.at(-1)?.searchParams.get("page")).toBe("2");
      expect(reads.at(-1)?.searchParams.get("limit")).toBe("1");
      await page
        .getByRole("button", {
          name: api === "/transfers" ? "查询" : "刷新",
          exact: true,
        })
        .click();
      await expect
        .poll(() => reads.at(-1)?.searchParams.get("merchant_id"))
        .toBe("2");
      if (api === "/transfers") {
        const download = page.waitForEvent("download");
        await page
          .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
          .click();
        await download;
        expect(reads.at(-1)?.searchParams.get("merchant_id")).toBe("2");
        expect(reads.at(-1)?.searchParams.get("format")).toBe("csv");
      }
      expect(writes).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
    });
    test(`invalid linked ${label} scope never loads an unfiltered list at ${width}`, async ({
      page,
    }) => {
      const reads: string[] = [];
      await page.setViewportSize({ width, height: 1000 });
      await page.route("**/console/v1/**", (route) => {
        const p = new URL(route.request().url()).pathname;
        if (p === "/console/v1/admin" + api) reads.push(p);
        return route.fulfill({
          json: {
            code: 0,
            data: p.endsWith("/session")
              ? {
                  user: { id: "1", role: "admin", admin_class: "super" },
                  csrf_token: "linked148",
                }
              : { items: [], total: 0 },
          },
        });
      });
      await page.goto(path + "?uid=1&uid=2");
      await expect(page.getByRole("alert")).toContainText(
        "关联记录筛选参数无效",
      );
      if (api === "/transfers")
        await expect(
          page.getByRole("button", { name: "导出筛选结果 CSV", exact: true }),
        ).toBeDisabled();
      expect(reads).toEqual([]);
    });
  }
