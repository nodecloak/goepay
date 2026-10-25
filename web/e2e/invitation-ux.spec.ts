import { test, expect, type Page } from "@playwright/test";
async function setup(page: Page) {
  const writes: { path: string; body: any }[] = [];
  const queries: string[] = [];
  let usedVisible = true;
  let failGenerate = false;
  const ids: string[] = [];
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      p = url.pathname;
    let data: any = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "admin", role: "admin", admin_class: "super" },
        csrf_token: "invite-csrf",
      };
    else if (p.endsWith("/invitation-codes") && req.method() === "GET") {
      queries.push(url.search);
      data = {
        items: usedVisible
          ? [
              {
                id: "42",
                code: "USED-ONE",
                status: "used",
                enabled: true,
                max_uses: "2",
                use_count: 1,
                created_at: "2026-10-20T01:00:00Z",
                used_at: "2026-10-21T02:00:00Z",
                used_by: "1000",
                uses: [
                  { merchant_id: "1000", used_at: "2026-10-21T02:00:00Z" },
                ],
              },
            ]
          : [],
        total: usedVisible ? 1 : 0,
      };
    } else if (req.method() !== "GET") {
      expect(req.headers()["x-csrf-token"]).toBe("invite-csrf");
      const body = req.postDataJSON();
      writes.push({ path: p, body });
      if (p.endsWith("/generate")) {
        ids.push(body.request_id);
        if (failGenerate) {
          failGenerate = false;
          return route.abort("failed");
        }
        data = {
          items: [
            { id: "50", code: "GENERATED-ONE" },
            { id: "51", code: "GENERATED-TWO" },
          ],
          replayed: ids.length > 1,
        };
      } else if (p.endsWith("/clear")) {
        usedVisible = false;
        data = { archived: 1, remaining: 0, replayed: false };
      } else if (req.method() === "DELETE") {
        usedVisible = false;
        data = {};
      }
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  return { writes, queries, failNext: () => (failGenerate = true), ids };
}
test("invitation filters, generation and confirmed clearing use real dedicated requests", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/admin/merchant/invite-code");
  await expect(
    page.getByRole("button", { name: "生成邀请码", exact: true }),
  ).toBeVisible();
  expect(f.writes).toHaveLength(0);
  for (const label of [
    "邀请码",
    "状态",
    "添加时间",
    "使用时间",
    "使用者",
    "操作",
  ])
    await expect(
      page.getByRole("columnheader", { name: label, exact: true }),
    ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "1000", exact: true }),
  ).toHaveAttribute("href", "/admin/merchant/merchant-list?uid=1000");
  await page.getByLabel("状态筛选").selectOption("used");
  await page.getByLabel("搜索邀请码").fill("USED");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect.poll(() => f.queries.at(-1)).toContain("status=used");
  await page.getByRole("button", { name: "生成邀请码", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "生成邀请码", exact: true });
  await dialog.getByLabel("生成个数").fill("2");
  await dialog.getByRole("button", { name: "生成", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "生成结果", exact: true }),
  ).toContainText("GENERATED-TWO");
  expect(f.writes[0].body.count).toBe(2);
  await page.getByRole("button", { name: "关闭结果" }).click();
  await page.getByRole("button", { name: "清空已使用", exact: true }).click();
  const clear = page.getByRole("dialog", { name: "清空已使用的邀请码" });
  await expect(clear).toContainText("保留注册凭据");
  expect(f.writes).toHaveLength(1);
  await clear.getByRole("button", { name: "确认清空", exact: true }).click();
  await expect(clear).toBeHidden();
  expect(f.writes[1].body).toMatchObject({ scope: "used", confirm: true });
});
test("unknown generation keeps the original batch request instead of creating more codes", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/admin/merchant/invite-code");
  f.failNext();
  await page.getByRole("button", { name: "生成邀请码", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "生成邀请码", exact: true });
  await dialog.getByLabel("生成个数").fill("2");
  await dialog.getByRole("button", { name: "生成", exact: true }).click();
  await expect(dialog).toContainText("结果未知");
  await expect(dialog.getByLabel("生成个数")).toBeDisabled();
  await dialog.getByRole("button", { name: "关闭并保留原请求" }).click();
  await page.getByRole("link", { name: "仪表盘", exact: true }).first().click();
  await page.goBack();
  await page.getByRole("button", { name: "生成邀请码", exact: true }).click();
  await expect(dialog.getByLabel("生成个数")).toHaveValue("2");
  await dialog
    .getByRole("button", { name: "核对原批生成结果", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "生成结果", exact: true }),
  ).toBeVisible();
  expect(f.ids).toHaveLength(2);
  expect(f.ids[1]).toBe(f.ids[0]);
});
test("invitation management fits a narrow screen and only deletes after confirmation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 900 });
  const f = await setup(page);
  await page.goto("/admin/merchant/invite-code");
  await page.getByRole("button", { name: "删除", exact: true }).click();
  const d = page.getByRole("dialog", { name: "删除邀请码" });
  await expect(d).toContainText("USED-ONE");
  expect(f.writes).toHaveLength(0);
  await d.getByRole("button", { name: "取消", exact: true }).click();
  expect(f.writes).toHaveLength(0);
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
});
