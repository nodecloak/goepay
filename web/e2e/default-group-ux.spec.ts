import { test, expect } from "@playwright/test";

for (const width of [1280, 375])
  test(`original real default group can be edited and cannot be deleted at ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    let record = {
      id: "0",
      name: "默认用户组",
      enabled: true,
      rate_bps: 0,
      info: { alipay: { channel: "0", type: "", rate: "0" } },
    };
    const writes: { method: string; path: string; body: unknown }[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        path = new URL(req.url()).pathname;
      let data: unknown = { items: [], total: 0 };
      if (path.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super", name: "local" },
          csrf_token: "default154",
        };
      else if (path.endsWith("/groups/schema"))
        data = {
          title: "用户组设置",
          fields: [
            { name: "name", label: "名称", type: "string", required: true },
            { name: "rate_bps", label: "费率（万分比）", type: "integer" },
            { name: "info", label: "支付方式分成比例", type: "json" },
          ],
          permissions: { create: true, update: true, delete: true },
        };
      else if (path.endsWith("/groups/routing-options"))
        data = { available: true, items: [] };
      else if (path.endsWith("/pay-rolls")) data = { items: [], total: 0 };
      else if (path.endsWith("/groups/0")) {
        if (req.method() !== "GET") {
          const body = req.postDataJSON();
          writes.push({ method: req.method(), path, body });
          record = { ...record, ...body };
        }
        data = record;
      } else if (path.endsWith("/groups")) data = { items: [record], total: 1 };
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/merchant/group-list");
    const row = page.getByRole("row").filter({
      has: page.getByRole("cell", { name: "默认用户组", exact: true }),
    });
    await expect(
      row.getByRole("link", { name: "用户", exact: true }),
    ).toHaveAttribute("href", "/admin/merchant/merchant-list?gid=0");
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(
      "系统自带默认用户组不支持删除",
    );
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(writes).toHaveLength(0);
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "编辑用户组 GID:0",
      exact: true,
    });
    await expect(
      dialog.getByRole("combobox", { name: "支付宝通道", exact: true }),
    ).toHaveValue("disabled");
    await dialog
      .getByRole("combobox", { name: "支付宝通道", exact: true })
      .selectOption("sequential");
    await dialog.getByLabel("显示名称 *", { exact: true }).fill("系统默认商户");
    expect(writes).toHaveLength(0);
    await page.screenshot({
      path: info.outputPath(`default-group-${width}.png`),
      fullPage: true,
      animations: "disabled",
    });
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect.poll(() => writes.length).toBe(1);
    expect(writes[0]).toMatchObject({
      method: "PATCH",
      path: "/console/v1/admin/resources/groups/0",
      body: {
        name: "系统默认商户",
        info: { alipay: { channel: "-4", type: "channel", rate: "0" } },
      },
    });
    await expect(dialog).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
