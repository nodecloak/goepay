import { test, expect } from "@playwright/test";

for (const width of [1280, 375]) {
  test(`original merchant settlement permission edits and filters at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const id = "9223372036854775807",
      reads: URL[] = [],
      writes: Record<string, unknown>[] = [];
    const record = {
      id,
      name: "本地权限商户",
      settle: "1",
      settlement_permission_revision: "1",
      enabled: true,
      pay: "1",
      available_minor: "1000",
      effective_group_id: "0",
      effective_group_name: "默认用户组",
      email: "merchant160@example.invalid",
      created_at: "2026-10-26T00:00:00Z",
      settlement_recipient: { state: "not_configured" },
    };
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        u = new URL(req.url()),
        p = u.pathname;
      let data: unknown = { items: [], total: 0 };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "operator" },
          csrf_token: "settle160-local",
        };
      else if (p.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (p.endsWith("/merchants/schema"))
        data = {
          title: "商户管理",
          permissions: { create: true, update: true, delete: false },
          fields: [
            { name: "name", label: "名称", type: "string", required: true },
            ...[
              "effective_group_id",
              "effective_group_name",
              "available_minor",
              "email",
              "created_at",
              "pay",
              "enabled",
              "settlement_recipient",
            ].map((name) => ({
              name,
              label: name,
              type:
                name === "enabled"
                  ? "boolean"
                  : name === "settlement_recipient"
                    ? "json"
                    : "string",
              read_only: true,
            })),
            {
              name: "settle",
              label: "结算权限",
              type: "select",
              default: "1",
              admin_only: true,
              options: [
                { value: "1", label: "开启" },
                { value: "0", label: "关闭" },
              ],
            },
            {
              name: "settlement_permission_revision",
              label: "结算权限修订",
              type: "string",
              read_only: true,
            },
          ],
        };
      else if (p.endsWith("/merchants/" + id)) {
        if (req.method() === "PATCH") {
          const body = req.postDataJSON();
          writes.push(body);
          Object.assign(record, body, { settlement_permission_revision: "2" });
        }
        data = record;
      } else if (p.endsWith("/resources/merchants")) {
        reads.push(u);
        data = { items: [record], total: 1 };
      }
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/merchant-list");
    const row = page.getByRole("row").filter({
      has: page.getByRole("button", {
        name: "商户关联记录 " + id,
        exact: true,
      }),
    });
    await expect(row).toContainText("结算：正常");
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "编辑商户管理",
      exact: true,
    });
    const permission = dialog
      .getByRole("region", { name: "结算与代付", exact: true })
      .getByRole("combobox", { name: "结算权限", exact: true });
    await expect(permission).toHaveValue("1");
    await permission.selectOption("0");
    expect(writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(permission).toHaveValue("1");
    await permission.selectOption("0");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
    expect(writes).toHaveLength(1);
    expect(writes[0]).toMatchObject({ settle: "0" });
    expect(writes[0]).not.toHaveProperty("settlement_permission_revision");
    await expect(row).toContainText("结算：关闭");
    await page
      .getByLabel("商户状态筛选", { exact: true })
      .selectOption("settle_0");
    expect(reads.at(-1)!.searchParams.has("dstatus")).toBe(false);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect
      .poll(() => reads.at(-1)!.searchParams.get("dstatus"))
      .toBe("settle_0");
    await page.screenshot({
      path: test.info().outputPath(`merchant-settle-${width}.png`),
      fullPage: true,
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
