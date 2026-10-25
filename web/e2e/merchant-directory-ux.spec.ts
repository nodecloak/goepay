import { test, expect, type Page } from "@playwright/test";
async function setup(page: Page) {
  const reads: URL[] = [],
    writes: string[] = [],
    relatedReads: URL[] = [],
    sessions: string[] = [];
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      p = u.pathname;
    let data: any = { items: [], total: 0 };
    if (p.endsWith("/session")) sessions.push(p);
    if (req.method() !== "GET") writes.push(p);
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "admin", role: "admin", admin_class: "super" },
        csrf_token: "directory-csrf",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/resources/merchants/schema"))
      data = {
        title: "商户管理",
        fields: [
          { name: "name", label: "名称", type: "string", required: true },
          { name: "enabled", label: "启用", type: "boolean" },
          {
            name: "pay",
            label: "支付权限",
            type: "select",
            options: [
              { value: "0", label: "关闭" },
              { value: "1", label: "开启" },
              { value: "2", label: "未审核" },
            ],
          },
        ],
        permissions: { create: true, update: true, delete: false },
      };
    else if (p.endsWith("/orders")) relatedReads.push(u);
    else if (p.endsWith("/resources/merchants")) {
      reads.push(u);
      data = {
        items: [{ id: "1", name: "本地商户", enabled: true, pay: "2" }],
        total: 1,
      };
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  return { reads, writes, relatedReads, sessions };
}
for (const width of [375, 1280])
  test(`merchant directory honors original filter context and typed controls at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await setup(page);
    await page.goto("/admin/merchant/merchant-list?pay=2");
    await expect
      .poll(() => f.reads.at(-1)?.searchParams.get("dstatus"))
      .toBe("pay_2");
    await page.getByLabel("商户搜索字段", { exact: true }).selectOption("uid");
    await page.getByLabel("搜索商户管理", { exact: true }).fill("2");
    await page
      .getByLabel("商户状态筛选", { exact: true })
      .selectOption("status_0");
    await page
      .getByLabel("商户排序", { exact: true })
      .selectOption("money_desc");
    await page.getByLabel("注册开始日期", { exact: true }).fill("2026-10-01");
    await page.getByLabel("注册结束日期", { exact: true }).fill("2026-10-25");
    await page.getByLabel("用户组ID", { exact: true }).fill("3");
    await page.getByLabel("上级商户", { exact: true }).fill("4");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect.poll(() => f.reads.at(-1)?.searchParams.get("q")).toBe("2");
    const q = f.reads.at(-1)!.searchParams;
    for (const [k, v] of Object.entries({
      column: "uid",
      dstatus: "status_0",
      order: "money_desc",
      from: "2026-10-01",
      to: "2026-10-25",
      gid: "3",
      upid: "4",
      page: "1",
    }))
      expect(q.get(k)).toBe(v);
    await page.evaluate(() => {
      history.pushState({}, "", "/admin/merchant/merchant-list?uid=1000");
      dispatchEvent(new PopStateEvent("popstate"));
    });
    await expect.poll(() => f.reads.at(-1)?.searchParams.get("q")).toBe("1000");
    await expect(page.getByLabel("商户搜索字段", { exact: true })).toHaveValue(
      "uid",
    );
    expect(f.reads.at(-1)!.searchParams.has("gid")).toBe(false);
    expect(f.writes).toHaveLength(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
test("duplicate merchant route IDs fail without silently reading the whole directory", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/admin/merchant/merchant-list?uid=1&uid=2");
  await expect(page.getByRole("alert")).toContainText("商户筛选参数无效");
  expect(f.reads).toHaveLength(0);
  expect(f.writes).toHaveLength(0);
});

test("route page size and paging remain bound to the original merchant filters", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/admin/merchant/merchant-list?pay=2&page=2&limit=1");
  await expect.poll(() => f.reads.at(-1)?.searchParams.get("page")).toBe("2");
  expect(f.reads.at(-1)!.searchParams.get("limit")).toBe("1");
  await page.getByRole("button", { name: "上一页", exact: true }).click();
  await expect.poll(() => f.reads.at(-1)?.searchParams.get("page")).toBe("1");
  expect(f.reads.at(-1)!.searchParams.get("dstatus")).toBe("pay_2");
  expect(f.reads.at(-1)!.searchParams.get("limit")).toBe("1");
  expect(f.writes).toHaveLength(0);
});

for (const width of [375, 1280])
  test(`merchant original related-record menu opens a scoped target at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await setup(page);
    await page.goto("/admin/merchant/merchant-list?uid=1");
    const trigger = page.getByRole("button", {
      name: "商户关联记录 1",
      exact: true,
    });
    await trigger.click();
    const menu = page.getByRole("menu", {
      name: "商户关联记录 1",
      exact: true,
    });
    await expect(
      menu.getByRole("menuitem", { name: "订单管理", exact: true }),
    ).toHaveAttribute("href", "/admin/trade/order?uid=1");
    await expect(
      menu.getByRole("menuitem", { name: "资金明细", exact: true }),
    ).toHaveAttribute("href", "/admin/merchant/record?uid=1");
    const bounds = (await menu.boundingBox())!;
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
    await page.keyboard.press("Escape");
    await expect(menu).not.toBeVisible();
    await expect(trigger).toBeFocused();
    await trigger.click();
    await menu.getByRole("menuitem", { name: "订单管理", exact: true }).click();
    await expect
      .poll(() => f.relatedReads.at(-1)?.searchParams.get("merchant_id"))
      .toBe("1");
    expect(f.sessions).toHaveLength(1);
    expect(f.writes).toHaveLength(0);
  });
