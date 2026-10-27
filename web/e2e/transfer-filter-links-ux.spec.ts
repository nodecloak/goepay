import { test, expect } from "@playwright/test";
import { transferListFixture as fixture } from "../tests/fixtures/transfer-list";
for (const width of [1280, 375]) {
  test(`original transfer filter links remain scoped and invalid links block reads at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const reads: URL[] = [];
    let writes = 0;
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        u = new URL(r.url()),
        p = u.pathname;
      if (r.method() !== "GET") writes++;
      if (p.endsWith("/transfers")) reads.push(u);
      const data = p.endsWith("/session")
        ? {
            user: {
              id: "1",
              role: p.includes("/merchant/") ? "merchant" : "admin",
              admin_class: "super",
            },
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
      "/admin/transfer/list?uid=9223372036854775807&column=money&value=90071992547409.93&status=unknown&channel_id=9007199254740993&from=2026-10-01&to=2026-10-26&page=2&limit=10",
    );
    await expect(page.getByLabel("商户号", { exact: true })).toHaveValue(
      "9223372036854775807",
    );
    await expect(page.getByLabel("搜索字段", { exact: true })).toHaveValue(
      "money",
    );
    await expect(page.getByLabel("搜索付款记录", { exact: true })).toHaveValue(
      "90071992547409.93",
    );
    expect(reads).toHaveLength(1);
    const q = reads[0]!.searchParams;
    expect(q.get("merchant_id")).toBe("9223372036854775807");
    expect(q.get("search_field")).toBe("money");
    expect(q.get("q")).toBe("90071992547409.93");
    expect(q.get("page")).toBe("2");
    expect(q.get("limit")).toBe("10");
    expect(q.has("column")).toBe(false);
    expect(q.has("value")).toBe(false);
    const reset = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname.endsWith("/transfers") &&
        !new URL(r.url()).searchParams.has("merchant_id"),
    );
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await reset;
    await expect(page).toHaveURL(/\/admin\/transfer\/list$/);
    for (const path of [
      "/admin/transfer/list?uid=0",
      "/admin/transfer/list?uid=1&uid=2",
      "/admin/transfer/list?column=unsupported-private&value=private",
      "/admin/transfer/list?column=money&value=1.001",
      "/admin/transfer/list?unknown=x",
      "/merchant/transfer?uid=2",
    ]) {
      const before = reads.length;
      await page.goto(path);
      await expect(page.getByRole("alert").first()).toContainText(
        "关联记录筛选参数无效",
      );
      await expect(
        page.getByRole("button", { name: "查询", exact: true }),
      ).toBeDisabled();
      await expect(
        page.getByRole("button", { name: "导出筛选结果 CSV", exact: true }),
      ).toBeDisabled();
      expect(reads).toHaveLength(before);
    }
    const recovered = page.waitForResponse(
      (r) => new URL(r.url()).pathname === "/console/v1/merchant/transfers",
    );
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await recovered;
    await expect(page).toHaveURL(/\/merchant\/transfer$/);
    expect(writes).toBe(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
  test(`transfer query that leaves the current filter returns focus to its query form at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const row = fixture({ merchant_id: "1", status: "unknown" });
    let paid = false,
      queries = 0;
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        p = new URL(r.url()).pathname;
      let data: unknown = { items: paid ? [] : [row], total: paid ? 0 : 1 };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "transfer151",
        };
      else if (r.method() === "POST") {
        queries++;
        paid = true;
        data = { ...row, status: "succeeded", ledger_status: "succeeded" };
      } else if (p.endsWith("/transfers/" + row.id))
        data = {
          transfer: row,
          recipient: { account: "***0150", name: "***" },
        };
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/transfer/list?status=unknown");
    await page
      .locator('tr[data-transfer-id="' + row.id + '"]')
      .getByRole("button", { name: "查询状态", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: "付款详情", exact: true });
    await expect(dialog.getByRole("status")).toContainText("付款成功");
    await expect(
      page.getByRole("cell", { name: "暂无付款记录", exact: true }),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "关闭详情", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "查询", exact: true }),
    ).toBeFocused();
    expect(queries).toBe(1);
  });
}
