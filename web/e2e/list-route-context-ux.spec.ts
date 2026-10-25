import { test, expect, type Page } from "@playwright/test";
async function fixture(page: Page, role = "admin") {
  const reads: URL[] = [],
    writes: string[] = [];
  await page.route("**/console/v1/**", (r) => {
    const req = r.request(),
      u = new URL(req.url()),
      p = u.pathname;
    if (req.method() !== "GET") writes.push(p);
    let data: any = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1000", name: role, role, admin_class: "super" },
        csrf_token: "route-csrf",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (/\/(orders|refunds|ledger)$/.test(p)) {
      reads.push(u);
      if (u.searchParams.get("format") === "csv")
        return r.fulfill({
          contentType: "text/csv;charset=utf-8",
          headers: {
            "Content-Disposition": "attachment; filename=context.csv",
          },
          body: "\ufeffid,merchant_id\r\n1,2000",
        });
    }
    return r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  return { reads, writes };
}
for (const width of [375, 1280])
  for (const [path, kind] of [
    ["/admin/trade/order", "orders"],
    ["/admin/trade/refund", "refunds"],
    ["/admin/merchant/record", "ledger"],
  ])
    test(`original merchant link scopes ${kind} and same-route navigation at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const f = await fixture(page);
      await page.goto(path + "?uid=1000&page=2&limit=1");
      await expect
        .poll(() => f.reads.at(-1)?.searchParams.get("merchant_id"))
        .toBe("1000");
      expect(f.reads.at(-1)!.searchParams.get("page")).toBe("2");
      expect(f.reads.at(-1)!.searchParams.get("limit")).toBe("1");
      await page.evaluate((path) => {
        history.pushState({}, "", path + "?uid=2000");
        dispatchEvent(new PopStateEvent("popstate"));
      }, path);
      await expect
        .poll(() => f.reads.at(-1)?.searchParams.get("merchant_id"))
        .toBe("2000");
      expect(f.reads.at(-1)!.searchParams.get("page")).toBe("1");
      expect(f.reads.at(-1)!.searchParams.get("limit")).toBe("20");
      await Promise.all([
        page.waitForEvent("download"),
        page
          .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
          .click(),
      ]);
      expect(f.reads.at(-1)!.searchParams.get("merchant_id")).toBe("2000");
      const before = f.reads.length;
      await page.goto(path + "?uid=1&uid=2");
      await expect(page.getByRole("alert")).toContainText("列表筛选参数无效");
      expect(f.reads).toHaveLength(before);
      await expect(
        page.getByRole("button", { name: "导出筛选结果 CSV", exact: true }),
      ).toBeDisabled();
      expect(f.writes).toHaveLength(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
test("merchant route cannot silently substitute another merchant identity", async ({
  page,
}) => {
  const f = await fixture(page, "merchant");
  await page.goto("/merchant/trade/order?uid=2000");
  await expect(page.getByRole("alert")).toContainText(
    "商户后台只能查看本人记录",
  );
  expect(f.reads).toHaveLength(0);
  expect(f.writes).toHaveLength(0);
});
