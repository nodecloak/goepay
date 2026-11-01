import { test, expect, type Page, type Route } from "@playwright/test";

async function stubConsole(
  page: Page,
  handler: (route: Route, path: string) => Promise<void>,
) {
  const writes: string[] = [];
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (request.method() !== "GET") {
      writes.push(path);
      return route.abort();
    }
    if (path.endsWith("/session"))
      return route.fulfill({
        json: {
          code: 0,
          data: {
            user: {
              id: "1",
              name: "Test",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "synthetic",
          },
        },
      });
    return handler(route, path);
  });
  return writes;
}

for (const width of [375, 1280]) {
  test(`deposit review keeps empty columns and exact yuan at ${width}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    let populated = false;
    const writes = await stubConsole(page, async (route) =>
      route.fulfill({
        json: {
          code: 0,
          data: {
            items: populated
              ? [
                  {
                    id: "9007199254740993",
                    merchant_id: "101",
                    amount: "90071992547409.93",
                    amount_cents: "9007199254740993",
                    action: "release",
                    external_no: "synthetic-deposit",
                    status: "pending",
                    created_at: "2026-11-01T01:02:03Z",
                  },
                ]
              : [],
            total: populated ? 1 : 0,
          },
        },
      }),
    );
    await page.goto("/admin/merchant/deposit-review");
    await expect(page.getByRole("columnheader")).toHaveText([
      "编号",
      "商户 ID",
      "操作类型",
      "金额",
      "业务请求号",
      "状态",
      "创建时间",
      "操作",
    ]);
    populated = true;
    await page.getByRole("button", { name: "刷新", exact: true }).click();
    await expect(page.locator("tbody")).toContainText("90,071,992,547,409.93");
    await expect(page.locator("tbody")).toContainText("申请释放保证金");
    await page.getByRole("button", { name: "核准释放", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByRole("dialog")).toContainText("90071992547409.93");
    await page.screenshot({
      path: testInfo.outputPath(`deposit-review-${width}.png`),
      fullPage: true,
    });
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "取消", exact: true })
      .click();
    expect(writes).toEqual([]);
  });

  test(`complaint task query, selection and native reset at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let reads = 0;
    const writes = await stubConsole(page, async (route, path) => {
      if (path.endsWith("/complaint-sync-tasks")) reads++;
      return route.fulfill({
        json: {
          code: 0,
          data: {
            items: path.endsWith("/complaint-sync-tasks")
              ? [
                  {
                    channel_id: "7",
                    name: "微信",
                    enabled: true,
                    frequency: 6,
                    num: 20,
                  },
                  {
                    channel_id: "9007199254740993",
                    name: "支付宝",
                    enabled: false,
                    frequency: 6,
                    num: 20,
                  },
                ]
              : [],
          },
        },
      });
    });
    await page.goto("/admin/complain/task");
    await expect(page.locator("tbody tr")).toHaveCount(2);
    await page
      .getByRole("textbox", { name: "通道ID", exact: true })
      .fill("9007199254740993");
    await page.getByRole("button", { name: "状态", exact: true }).click();
    await page.getByRole("option", { name: "已禁用", exact: true }).click();
    const before = reads;
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(page.locator("tbody tr")).toHaveCount(1);
    await expect.poll(() => reads).toBe(before + 1);
    await expect(page.locator("tbody")).toContainText("支付宝");
    await page.getByRole("checkbox", { name: "全选", exact: true }).check();
    await expect(page.locator('[data-action="batch-delete"]')).toContainText(
      "1",
    );
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await expect(page.locator("tbody tr")).toHaveCount(2);
    await expect.poll(() => reads).toBe(before + 2);
    await expect(page.locator('[data-action="batch-delete"]')).toBeDisabled();
    await expect(
      page.getByRole("textbox", { name: "通道ID", exact: true }),
    ).toHaveValue("");
    await page.getByRole("textbox", { name: "通道ID", exact: true }).fill("01");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("通道ID");
    expect(reads).toBe(before + 2);
    expect(writes).toEqual([]);
  });

  test(`plugin extension editor loads only after expansion at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let extensionReads = 0;
    const writes = await stubConsole(page, async (route, path) => {
      if (path.includes("/resources/plugin-configs")) extensionReads++;
      const data = path.endsWith("/schema")
        ? {
            title: "本地扩展资料",
            fields: [{ name: "name", label: "名称", type: "string" }],
            permissions: { create: true, update: true, delete: true },
          }
        : { items: [], total: 0 };
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/app/plugin-list");
    const panel = page.locator("details.plugin-data-panel");
    await expect(panel.locator("summary")).toBeVisible();
    expect(extensionReads).toBe(0);
    await expect(panel).not.toHaveAttribute("open");
    await page
      .getByRole("button", { name: "刷新本地插件列表", exact: true })
      .click();
    expect(extensionReads).toBe(0);
    await panel.locator("summary").click();
    await expect(panel).toHaveAttribute("open", "");
    await expect(
      panel.getByRole("columnheader", { name: "名称", exact: true }),
    ).toBeVisible();
    expect(extensionReads).toBe(2);
    await panel.locator("summary").click();
    await expect(panel.locator("table")).toHaveCount(0);
    expect(writes).toEqual([]);
  });

  test(`administrator audit metadata stays readable at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const fields = [
      { name: "username", label: "账号", type: "string" },
      { name: "display_name", label: "名称", type: "string" },
      {
        name: "admin_class",
        label: "权限等级",
        type: "select",
        options: [{ value: "operator", label: "普通管理员" }],
      },
      { name: "permissions", label: "菜单权限", type: "permission_tree" },
      { name: "enabled", label: "启用", type: "boolean" },
      {
        name: "created_at",
        label: "添加时间",
        type: "string",
        read_only: true,
      },
      {
        name: "last_login_at",
        label: "上次登录",
        type: "string",
        read_only: true,
      },
      {
        name: "totp_enabled",
        label: "TOTP状态",
        type: "boolean",
        read_only: true,
      },
    ];
    const writes = await stubConsole(page, async (route, path) =>
      route.fulfill({
        json: {
          code: 0,
          data: path.endsWith("/schema")
            ? {
                title: "管理员列表",
                fields,
                permissions: { create: true, update: true, delete: false },
              }
            : {
                items: [
                  {
                    id: "9007199254740993",
                    username: "operator",
                    admin_class: "operator",
                    permissions: ["/trade/order"],
                    enabled: true,
                    created_at: "2026-11-01T01:02:03Z",
                    last_login_at: null,
                    totp_enabled: false,
                  },
                ],
                total: 1,
              },
        },
      }),
    );
    await page.goto("/admin/system/admin-user");
    await expect(page.getByRole("columnheader")).toHaveText([
      "UID",
      "用户名",
      "权限",
      "添加时间",
      "上次登录",
      "TOTP状态",
      "状态",
      "操作",
    ]);
    await expect(page.locator("tbody")).toContainText("2026-11-01 09:02:03");
    await expect(page.locator("tbody")).toContainText("未开启");
    await expect(page.locator("tbody")).not.toContainText("/trade/order");
    await expect
      .poll(
        async () =>
          (await page.locator("tbody tr").boundingBox())?.height ?? 999,
      )
      .toBeLessThan(100);
    expect(writes).toEqual([]);
  });
}
