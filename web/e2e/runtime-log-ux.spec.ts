import { test, expect } from "@playwright/test";

for (const width of [1280, 375])
  test(`original runtime logs have date and three columns and stable explicit cleanup at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: { body: Record<string, unknown> }[] = [],
      reads: URL[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        u = new URL(req.url()),
        path = u.pathname;
      let data: unknown = { items: [], total: 0 };
      if (path.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super", name: "local" },
          csrf_token: "runtime132",
        };
      else if (path.endsWith("/runtime-log/cleanup")) {
        if (req.method() === "POST") {
          writes.push({ body: req.postDataJSON() });
          return route.abort();
        }
        reads.push(u);
        data = {
          days: Number(u.searchParams.get("days")),
          cutoff: "2026-09-25T00:00:00+08:00",
          count: "2",
        };
      } else if (path.endsWith("/runtime-log")) {
        reads.push(u);
        data = {
          items: [
            {
              id: "9007199254740993",
              time: "2026-10-25T00:01:00Z",
              type: "error",
              content: "数据库操作失败（SQLSTATE 42P01）",
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
    await page.goto("/admin/system/runtime-log");
    await expect
      .poll(() => reads.at(-1)?.pathname.endsWith("/runtime-log"))
      .toBe(true);
    await expect(page.getByRole("columnheader")).toHaveText([
      "时间",
      "日志类型",
      "日志内容",
    ]);
    await expect(page.getByRole("table")).toContainText("2026-10-25 08:01:00");
    await page.getByLabel("日期", { exact: true }).fill("2026-10-24");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect
      .poll(() => reads.at(-1)?.searchParams.get("date"))
      .toBe("2026-10-24");
    await page.getByRole("button", { name: "清除日志", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "清除运行日志",
      exact: true,
    });
    await expect(dialog).toBeVisible();
    await page.getByLabel("保留天数", { exact: true }).fill("31");
    await dialog
      .getByRole("button", { name: "预览清除范围", exact: true })
      .click();
    await expect(dialog).toContainText("2 条");
    expect(writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "确认清除", exact: true }).click();
    await expect(dialog).toContainText("结果未知");
    expect(writes).toHaveLength(1);
    const first = writes[0].body;
    expect(first.days).toBe(31);
    expect(first.cutoff).toBe("2026-09-25T00:00:00+08:00");
    expect(first.external_no).toMatch(/^[A-Za-z0-9_-]{8,96}$/);
    await expect(page.getByLabel("保留天数", { exact: true })).toBeDisabled();
    await dialog
      .getByRole("button", { name: "关闭并保留原请求", exact: true })
      .click();
    await page.getByRole("button", { name: "清除日志", exact: true }).click();
    await expect(dialog).toContainText("结果未知");
    expect(writes).toHaveLength(1);
    await dialog
      .getByRole("button", { name: "核对原清除请求", exact: true })
      .click();
    await expect.poll(() => writes.length).toBe(2);
    expect(writes[1].body).toEqual(first);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });

test("a refused retry cannot discard an earlier unknown runtime cleanup intent", async ({
  page,
}) => {
  const bodies: unknown[] = [];
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    let data: unknown = { items: [], total: 0 };
    if (path.endsWith("/session"))
      data = {
        user: { id: "1", role: "admin", admin_class: "super" },
        csrf_token: "runtime-refused132",
      };
    else if (path.endsWith("/runtime-log/cleanup")) {
      if (req.method() === "POST") {
        bodies.push(req.postDataJSON());
        if (bodies.length === 1) return route.abort();
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "local retry refused" }),
        });
      }
      data = { days: 30, cutoff: "2026-09-25T00:00:00+08:00", count: "2" };
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/system/runtime-log");
  await page.getByRole("button", { name: "清除日志", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "清除运行日志",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "预览清除范围", exact: true })
    .click();
  await dialog.getByRole("button", { name: "确认清除", exact: true }).click();
  await expect(dialog).toContainText("结果未知");
  await dialog
    .getByRole("button", { name: "核对原清除请求", exact: true })
    .click();
  await expect.poll(() => bodies.length).toBe(2);
  await expect(dialog).toContainText("local retry refused");
  await expect(page.getByLabel("保留天数", { exact: true })).toBeDisabled();
  await expect(
    dialog.getByRole("button", { name: "预览清除范围", exact: true }),
  ).toHaveCount(0);
  expect(bodies[1]).toEqual(bodies[0]);
  await dialog
    .getByRole("button", { name: "关闭并保留原请求", exact: true })
    .click();
  await page.getByRole("button", { name: "清除日志", exact: true }).click();
  await expect(page.getByLabel("保留天数", { exact: true })).toBeDisabled();
});
