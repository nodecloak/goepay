import { test, expect } from "@playwright/test";

for (const width of [1280, 375])
  test(`original group list and supported editor sections preserve the existing JSON contract at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const id = "9007199254740993",
      writes: { path: string; body: Record<string, unknown> }[] = [];
    let record: Record<string, unknown> = {
      id,
      name: "本地测试组",
      description: "原组说明",
      rate_bps: 38,
      price_cents: 1234,
      duration_days: 30,
      enabled: true,
      user_transfer: "0",
      settle_open: "3",
      settle_rate: "1.25",
    };
    const fields = [
      {
        name: "name",
        label: "名称",
        type: "string",
        required: true,
        max_length: 100,
      },
      { name: "description", label: "说明", type: "text" },
      {
        name: "rate_bps",
        label: "费率（万分比）",
        type: "integer",
        min: 0,
        max: 10000,
      },
      { name: "price_cents", label: "会员价格（分）", type: "integer" },
      { name: "duration_days", label: "会员天数", type: "integer" },
      { name: "enabled", label: "启用", type: "boolean" },
      {
        name: "user_transfer",
        label: "用户代付（日报代付统计）",
        type: "select",
        options: ["0", "1"],
      },
      {
        name: "settle_open",
        label: "结算开关",
        type: "select",
        options: [
          { value: "0", label: "继承全局" },
          { value: "1", label: "自动" },
          { value: "2", label: "手动" },
          { value: "3", label: "自动与手动" },
        ],
      },
      { name: "settle_rate", label: "余额自动转账比例（%）", type: "money" },
    ];
    await page.route("**/console/v1/**", (route) => {
      const request = route.request(),
        path = new URL(request.url()).pathname;
      let data: unknown = { items: [], total: 0 };
      if (request.method() !== "GET") {
        const body = request.postDataJSON();
        writes.push({ path, body });
        record = { ...record, ...body };
        data = record;
      } else if (path.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super", name: "local" },
          csrf_token: "group133",
        };
      else if (path.endsWith("/groups/schema"))
        data = {
          title: "用户组设置",
          fields,
          permissions: { create: true, update: true, delete: true },
        };
      else if (path.endsWith("/groups/" + id)) data = record;
      else if (path.endsWith("/groups")) data = { items: [record], total: 1 };
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/merchant/group-list");
    await expect(page.getByRole("columnheader")).toHaveText([
      "GID",
      "用户组名称",
      "操作",
    ]);
    await expect(
      page.getByRole("cell", { name: id, exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByLabel("用户组名称", { exact: false })
      .fill("已修改本地组");
    await dialog.getByRole("tab", { name: "支付配置", exact: true }).click();
    await expect(
      dialog.getByLabel("费率（万分比）", { exact: true }),
    ).toHaveValue("38");
    await dialog.getByRole("tab", { name: "结算配置", exact: true }).click();
    await expect(
      dialog.getByRole("combobox", { name: "结算开关", exact: true }),
    ).toHaveValue("3");
    await dialog
      .getByRole("combobox", { name: "结算开关", exact: true })
      .selectOption("2");
    await dialog.getByRole("tab", { name: "通知配置", exact: true }).click();
    await expect(dialog).toContainText("不等于开通商户代付权限");
    await dialog.getByRole("tab", { name: "基本信息", exact: true }).click();
    await expect(dialog.getByLabel("用户组名称", { exact: false })).toHaveValue(
      "已修改本地组",
    );
    expect(writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect.poll(() => writes.length).toBe(1);
    expect(writes[0].path).toBe("/console/v1/admin/resources/groups/" + id);
    expect(writes[0].body).toEqual({
      name: "已修改本地组",
      description: "原组说明",
      rate_bps: 38,
      price_cents: 1234,
      duration_days: 30,
      enabled: true,
      user_transfer: "0",
      settle_open: "2",
      settle_rate: "1.25",
    });
    await expect(dialog).toHaveCount(0);
    await expect(
      page.getByRole("cell", { name: "已修改本地组", exact: true }),
    ).toBeVisible();
    const configure = page.getByRole("button", {
      name: "用户组配置 " + id,
      exact: true,
    });
    await configure.click();
    await page.getByRole("menuitem", { name: "支付配置", exact: true }).click();
    await expect(
      dialog.getByRole("tab", { name: "支付配置", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await expect(
      dialog.getByLabel("费率（万分比）", { exact: true }),
    ).toBeFocused();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(configure).toBeFocused();
    await configure.click();
    await page.getByRole("menuitem", { name: "结算配置", exact: true }).click();
    await expect(
      dialog.getByRole("tab", { name: "结算配置", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await expect(
      dialog.getByRole("combobox", { name: "结算开关", exact: true }),
    ).toHaveValue("2");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    expect(writes).toHaveLength(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
