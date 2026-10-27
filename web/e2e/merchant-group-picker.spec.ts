import { test, expect, type Page } from "@playwright/test";
const big = "9223372036854775807";
async function fixture(page: Page, fail = false, includeDefault = false) {
  const reads: URL[] = [],
    writes: Record<string, unknown>[] = [];
  const record = {
    id: "11901",
    name: "本地组选择商户",
    group_id: big,
    enabled: true,
  };
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      p = u.pathname;
    let data: unknown = { items: [], total: 0 };
    if (req.method() !== "GET") {
      writes.push(req.postDataJSON());
      Object.assign(record, req.postDataJSON());
    }
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "Local", role: "admin", admin_class: "super" },
        csrf_token: "group123-csrf",
      };
    else if (p.endsWith("/merchants/schema"))
      data = {
        title: "商户管理",
        permissions: { create: false, update: true, delete: false },
        fields: [
          { name: "name", label: "名称", type: "string", required: true },
          {
            name: "group_id",
            label: "基础用户组 ID（管理员指定）",
            type: "string",
          },
          {
            name: "confirm_group_assignment",
            label: "明确确认基础组（需同时提交用户组 ID）",
            type: "boolean",
            write_only: true,
          },
          { name: "enabled", label: "启用", type: "boolean" },
        ],
      };
    else if (p.endsWith("/resources/merchants/11901")) data = record;
    else if (p.endsWith("/resources/merchants"))
      data = { items: [record], total: 1 };
    else if (p.includes("/resources/groups")) {
      reads.push(u);
      if (fail)
        return route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({
            code: -1,
            message: "local groups unavailable",
          }),
        });
      if (p.endsWith("/" + big)) data = { id: big, name: "当前大编号组" };
      else if (u.searchParams.get("q"))
        data = { items: [{ id: "300", name: "高级用户组" }], total: 1 };
      else
        data = {
          items: [
            ...(includeDefault
              ? [{ id: "0", name: "系统默认商户", enabled: true }]
              : []),
            {
              id: u.searchParams.get("page") === "2" ? "2" : "1",
              name: "普通用户组",
            },
          ],
          total: 101,
        };
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/merchant/merchant-list");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "编辑商户管理",
    exact: true,
  });
  await expect(dialog.getByLabel("用户组", { exact: true })).toBeVisible();
  return { reads, writes, dialog };
}
for (const width of [1280, 375]) {
  test(`real default group catalog preserves positive selection and one canonical default option at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page, false, true),
      select = f.dialog.getByLabel("用户组", { exact: true });
    await expect(select).toHaveValue(big);
    await expect(select).toContainText("当前大编号组");
    await expect(
      select.getByRole("option", { name: "系统默认商户", exact: true }),
    ).toHaveCount(1);
    expect(f.writes).toHaveLength(0);
    await select.selectOption("");
    await f.dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect.poll(() => f.writes.length).toBe(1);
    expect(f.writes[0]!.group_id).toBe("");
  });
  test(`original group selector searches without submitting and writes a canonical explicit group at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page),
      select = f.dialog.getByLabel("用户组", { exact: true });
    await expect(select).toHaveValue(big);
    await expect(select).toContainText("当前大编号组");
    expect(f.writes).toHaveLength(0);
    await f.dialog
      .getByRole("button", { name: "下页用户组", exact: true })
      .click();
    await expect.poll(() => f.reads.at(-1)!.searchParams.get("page")).toBe("2");
    await expect(select).toBeEnabled();
    const search = f.dialog.getByLabel("搜索用户组名称", { exact: true });
    await search.fill("高级");
    await search.press("Enter");
    await expect.poll(() => f.reads.at(-1)!.searchParams.get("q")).toBe("高级");
    await expect(select).toBeEnabled();
    expect(f.writes).toHaveLength(0);
    await select.selectOption("300");
    await f.dialog
      .getByLabel("明确确认基础组（需同时提交用户组 ID）", { exact: true })
      .check();
    await page.screenshot({
      path: info.outputPath("merchant-groups-" + width + ".png"),
    });
    await f.dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0]).toMatchObject({
      group_id: "300",
      confirm_group_assignment: true,
    });
  });
  test(`group lookup failure retains the original until the default group is explicitly selected at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page, true),
      select = f.dialog.getByLabel("用户组", { exact: true });
    await expect(f.dialog.getByRole("alert")).toContainText("原选择不会被清空");
    await expect(select).toHaveValue(big);
    expect(f.writes).toHaveLength(0);
    await select.selectOption("");
    await f.dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0]!.group_id).toBe("");
  });
}
