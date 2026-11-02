import { test, expect } from "@playwright/test";
for (const width of [1280, 375])
  test(`original group rates column and users link preserve exact GID without writes at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const id = "9007199254740993",
      reads: URL[] = [],
      writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        url = new URL(req.url()),
        path = url.pathname;
      if (req.method() !== "GET") writes.push(path);
      let data: unknown = { items: [], total: 0 };
      if (path.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "local-directory",
        };
      else if (path.endsWith("/groups/schema"))
        data = {
          title: "用户组设置",
          fields: [
            { name: "name", label: "名称", type: "string" },
            { name: "rate_bps", label: "费率（万分比）", type: "integer" },
            { name: "info", label: "支付方式分成比例", type: "json" },
          ],
          permissions: { create: true, update: true, delete: true },
        };
      else if (path.endsWith("/groups"))
        data = {
          items: [
            {
              id,
              name: "本地原组",
              rate_bps: "38",
              info: { alipay: { rate: "99.50" }, wxpay: { rate: "0" } },
            },
          ],
          total: 1,
        };
      else if (path.endsWith("/merchants/schema"))
        data = {
          title: "商户列表",
          fields: [{ name: "name", label: "名称", type: "string" }],
          permissions: { create: false, update: false, delete: false },
        };
      else if (path.endsWith("/merchants")) reads.push(url);
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/group-list");
    await expect(page.getByRole("columnheader")).toHaveText([
      "GID",
      "用户组名称",
      "通道与费率",
      "操作",
    ]);
    const row = page
      .getByRole("row")
      .filter({ has: page.getByRole("cell", { name: id, exact: true }) });
    await expect(row).toContainText("99.62%");
    await expect(row).toContainText("支付宝：99.50%");
    await expect(row).toContainText("继承商户通道配置");
    const users = row.getByRole("link", { name: "用户", exact: true });
    await expect(users).toHaveAttribute(
      "href",
      "/admin/merchant/merchant-list?gid=" + id,
    );
    await users.click();
    await expect(page).toHaveURL(
      new RegExp("/admin/merchant/merchant-list\\?gid=" + id + "$"),
    );
    await expect.poll(() => reads.at(-1)?.searchParams.get("gid")).toBe(id);
    expect(reads.every((u) => u.searchParams.get("gid") === id)).toBe(true);
    expect(writes).toEqual([]);
  });

for (const width of [1280, 375])
  test(`default group cannot enter single or batch deletion at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const ordinaryID = "9007199254740993";
    const writes: { path: string; body: unknown }[] = [];
    let removed = false;
    await page.route("**/console/v1/**", (route) => {
      const request = route.request(),
        path = new URL(request.url()).pathname;
      let data: unknown = { items: [], total: 0 };
      if (path.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "local-protected-group",
        };
      else if (path.endsWith("/groups/schema"))
        data = {
          title: "用户组设置",
          fields: [{ name: "name", label: "名称", type: "string" }],
          batch_delete: true,
          permissions: { create: false, update: false, delete: true },
        };
      else if (
        path.endsWith("/groups/batch-delete") &&
        request.method() === "POST"
      ) {
        writes.push({ path, body: request.postDataJSON() });
        removed = true;
        data = { deleted: 1 };
      } else if (path.endsWith("/groups")) {
        const items = [
          { id: "0", name: "默认用户组", rate_bps: "0", info: {} },
          ...(removed
            ? []
            : [
                { id: ordinaryID, name: "普通用户组", rate_bps: "0", info: {} },
              ]),
        ];
        data = { items, total: items.length };
      }
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/group-list");
    const defaultRow = page
      .getByRole("row")
      .filter({ has: page.getByRole("cell", { name: "0", exact: true }) });
    await expect(
      defaultRow.getByRole("button", { name: "删除", exact: true }),
    ).toBeDisabled();
    await expect(
      defaultRow.getByRole("button", { name: "删除", exact: true }),
    ).toHaveAttribute("title", /默认用户组.*不可删除/);
    await expect(page.getByLabel("选择记录 0", { exact: true })).toBeDisabled();
    await page.getByLabel("选择本页全部记录", { exact: true }).check();
    await expect(
      page.getByLabel("选择记录 0", { exact: true }),
    ).not.toBeChecked();
    await expect(
      page.getByLabel("选择记录 " + ordinaryID, { exact: true }),
    ).toBeChecked();
    await page
      .getByRole("button", { name: "批量删除 (1)", exact: true })
      .click();
    await page
      .getByRole("dialog", { name: "批量删除确认" })
      .getByRole("button", { name: "删除", exact: true })
      .click();
    await expect.poll(() => writes.length).toBe(1);
    expect(writes[0]).toEqual({
      path: "/console/v1/admin/resources/groups/batch-delete",
      body: { ids: [ordinaryID] },
    });
    await expect(
      page.getByLabel("选择本页全部记录", { exact: true }),
    ).toBeDisabled();
    await expect(
      page.getByRole("button", { name: "批量删除", exact: true }),
    ).toBeDisabled();
  });
