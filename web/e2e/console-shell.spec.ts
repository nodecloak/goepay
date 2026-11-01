import { test, expect, type Page } from "@playwright/test";

const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
async function mockConsole(
  page: Page,
  baseURL: string | undefined,
  area: "admin" | "merchant",
  login = false,
) {
  if (!baseURL) throw Error("A local test baseURL is required");
  const origin = new URL(baseURL).origin;
  if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(origin))
    throw Error("Loopback fixture only");
  const foreign: string[] = [],
    posts: string[] = [];
  await page.context().route("**/*", (route) => {
    if (new URL(route.request().url()).origin === origin)
      return route.continue();
    foreign.push(new URL(route.request().url()).origin);
    return route.abort();
  });
  await page.route("**/console/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() !== "GET") posts.push(path);
    if (path.endsWith("/session"))
      return route.fulfill(
        login
          ? {
              status: 401,
              contentType: "application/json",
              body: JSON.stringify({ code: 401, message: "请登录" }),
            }
          : ok({
              user: {
                id: area === "admin" ? "1" : "1000",
                name: area === "admin" ? "平台管理员" : "演示商户",
                role: area,
                admin_class: "super",
              },
              csrf_token: "synthetic-shell-csrf",
            }),
      );
    if (path.endsWith("/dashboard"))
      return route.fulfill(
        ok({
          orders: 128,
          paid_orders: 112,
          available_money: "1280.00",
          frozen_money: "30.00",
        }),
      );
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  return { foreign, posts };
}

test("Pro admin shell folds, tracks tabs and keeps route navigation available on375px", async ({
  page,
  baseURL,
}) => {
  const observed = await mockConsole(page, baseURL, "admin");
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/admin/dashboard");
  await expect(page.locator(".stat")).toHaveCount(4);
  await page.screenshot({
    path: "../.tools/pro-shell-admin-desktop.png",
    fullPage: true,
    animations: "disabled",
  });
  const before = (await page.locator(".main-content").boundingBox())!.width;
  await page.getByRole("button", { name: "收起侧栏", exact: true }).click();
  await expect(page.locator(".workspace")).toHaveClass(/sidebar-collapsed/);
  await expect
    .poll(
      async () => (await page.locator(".main-content").boundingBox())!.width,
    )
    .toBeGreaterThan(before);
  await page.getByRole("button", { name: "展开侧栏", exact: true }).click();
  await page.locator("summary").filter({ hasText: "交易管理" }).click();
  await page
    .getByRole("navigation", { name: "后台菜单", exact: true })
    .getByRole("link", { name: "订单管理", exact: true })
    .click();
  await expect(
    page.getByRole("tab", { name: "订单管理", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("navigation", { name: "面包屑" })).toContainText(
    "订单管理",
  );
  await page
    .getByRole("button", { name: "关闭页签：订单管理", exact: true })
    .click();
  await expect(page).toHaveURL(/\/admin\/dashboard$/);
  await page.getByRole("button", { name: "收起侧栏", exact: true }).click();
  await page.setViewportSize({ width: 375, height: 812 });
  await page.getByRole("button", { name: "展开菜单", exact: true }).click();
  await expect(
    page.getByRole("navigation", { name: "后台菜单" }),
  ).toBeVisible();
  await page.locator("summary").filter({ hasText: "交易管理" }).click();
  await expect(
    page
      .getByRole("navigation", { name: "后台菜单", exact: true })
      .getByRole("link", { name: "订单管理", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "展开菜单", exact: true }),
  ).toHaveAttribute("aria-expanded", "false");
  await page.screenshot({
    path: "../.tools/pro-shell-admin-375.png",
    fullPage: true,
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  expect(observed.foreign).toEqual([]);
  expect(observed.posts).toEqual([]);
});

test("Pro merchant light shell has no default work tabs and supports mobile drawer navigation", async ({
  page,
  baseURL,
}) => {
  const observed = await mockConsole(page, baseURL, "merchant");
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/merchant/dashboard");
  await expect(page.locator(".stat")).toHaveCount(4);
  await expect(page.locator(".workspace")).toHaveAttribute(
    "data-area",
    "merchant",
  );
  await expect(page.getByRole("tablist", { name: "工作页签" })).toHaveCount(0);
  await page.screenshot({
    path: "../.tools/pro-shell-merchant-desktop.png",
    fullPage: true,
    animations: "disabled",
  });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.getByRole("button", { name: "展开菜单", exact: true }).click();
  await expect(
    page.getByRole("navigation", { name: "后台菜单" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "关闭菜单", exact: true })
    .click({ position: { x: 350, y: 400 } });
  await expect(
    page.getByRole("button", { name: "展开菜单", exact: true }),
  ).toHaveAttribute("aria-expanded", "false");
  await page.screenshot({
    path: "../.tools/pro-shell-merchant-375.png",
    fullPage: true,
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  expect(observed.foreign).toEqual([]);
  expect(observed.posts).toEqual([]);
});

for (const area of ["admin", "merchant"] as const)
  test(`Pro ${area} login retains all current credentials and fits desktop/mobile`, async ({
    page,
    baseURL,
  }) => {
    const observed = await mockConsole(page, baseURL, area, true);
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`/${area}/dashboard`);
    await expect(page.getByLabel("用户名", { exact: true })).toBeVisible();
    await expect(page.getByLabel("密码", { exact: true })).toBeVisible();
    await expect(
      page.getByLabel("动态验证码（已绑定验证器时填写）", { exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: `../.tools/pro-shell-login-${area}-desktop.png`,
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 375, height: 812 });
    await expect(
      page.getByRole("button", { name: "登录工作台", exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: `../.tools/pro-shell-login-${area}-375.png`,
      fullPage: true,
      animations: "disabled",
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(observed.foreign).toEqual([]);
    expect(observed.posts).toEqual([]);
  });

for (const sample of ["printing", "account", "schedule"] as const)
  test(`Pro ${sample} settings keep native controls usable and have responsive read-only layouts`, async ({
    page,
    baseURL,
  }) => {
    const observed = await mockConsole(page, baseURL, "merchant");
    const recipient = {
      state: "configured",
      config_id: "42",
      type: "alipay",
      app_id: "",
      account_masked: "***1234",
      name_masked: "***",
    };
    const fixtures: Record<string, unknown> = {
      "/account/printing": {
        revision: "1",
        mode: 0,
        copies: 1,
        voice: false,
        device_masked: "",
        device_present: false,
      },
      "/account/profile": {
        name: "本地演示商户",
        email: "sandbox@example.invalid",
      },
      "/security/totp": { enabled: false },
      "/account/settlement-schedule": {
        enabled: false,
        revision: "1",
        recipient_config_id: "42",
        recipient,
      },
    };
    for (const [suffix, data] of Object.entries(fixtures))
      await page.route(`**/console/v1/merchant${suffix}`, (route) => {
        if (route.request().method() !== "GET") {
          observed.posts.push(suffix);
          return route.abort();
        }
        return route.fulfill(ok(data));
      });
    const route =
      sample === "printing"
        ? "/account/printing"
        : sample === "account"
          ? "/account/settings"
          : "/account/settlement-schedule";
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`/merchant${route}`);
    if (sample === "account")
      await expect(page.getByLabel("商户名称", { exact: true })).toHaveValue(
        "本地演示商户",
      );
    else await expect(page.getByRole("switch")).toBeVisible();
    await page.screenshot({
      path: `../.tools/pro-settings-${sample}-desktop.png`,
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 375, height: 812 });
    if (sample !== "account") {
      await page.getByRole("switch").check();
      await expect(page.getByRole("switch")).toBeChecked();
    }
    await page.screenshot({
      path: `../.tools/pro-settings-${sample}-375.png`,
      fullPage: true,
      animations: "disabled",
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(observed.posts).toEqual([]);
    expect(observed.foreign).toEqual([]);
  });

test("Pro resource form dialog fits mobile without submitting synthetic settings", async ({
  page,
  baseURL,
}) => {
  const observed = await mockConsole(page, baseURL, "admin");
  await page.route("**/console/v1/admin/resources/domains/schema", (route) =>
    route.fulfill(
      ok({
        title: "授权域名",
        fields: [
          {
            name: "domain",
            label: "域名",
            type: "string",
            required: true,
            max_length: 253,
          },
          { name: "enabled", label: "启用", type: "boolean" },
        ],
        permissions: { create: true, update: true, delete: true },
      }),
    ),
  );
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/admin/merchant/domain");
  await page.getByRole("button", { name: "新增授权域名", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("域名", { exact: false })).toBeVisible();
  await dialog.getByLabel("启用", { exact: true }).check();
  await page.screenshot({
    path: "../.tools/pro-settings-resource-desktop.png",
    fullPage: true,
    animations: "disabled",
  });
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(
    dialog.getByRole("button", { name: "保存", exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: "../.tools/pro-settings-resource-375.png",
    fullPage: true,
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(observed.posts).toEqual([]);
  expect(observed.foreign).toEqual([]);
});

test("Pro settlement recipient edit remains local until explicit save and fits mobile", async ({
  page,
  baseURL,
}) => {
  const observed = await mockConsole(page, baseURL, "merchant");
  await page.route(
    "**/console/v1/merchant/workflows/settlement-recipient/allowed-types",
    (route) =>
      route.fulfill(
        ok({ settlement: ["alipay", "wxpay", "bank"], can_write: true }),
      ),
  );
  await page.route(
    "**/console/v1/merchant/workflows/settlement-recipient",
    (route) => {
      if (route.request().method() !== "GET") {
        observed.posts.push("recipient");
        return route.abort();
      }
      return route.fulfill(
        ok({
          can_write: true,
          state: "configured",
          config_id: "42",
          type: "alipay",
          app_id: "",
          account_masked: "***1234",
          name_masked: "***",
        }),
      );
    },
  );
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/merchant/settle/settings");
  await page.getByRole("button", { name: "修改收款资料", exact: true }).click();
  await expect(page.getByLabel("收款账号", { exact: true })).toBeVisible();
  await page.screenshot({
    path: "../.tools/pro-settings-recipient-desktop.png",
    fullPage: true,
    animations: "disabled",
  });
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(
    page.getByRole("button", { name: "保存收款版本", exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: "../.tools/pro-settings-recipient-375.png",
    fullPage: true,
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page
    .getByRole("button", { name: "取消修改收款资料", exact: true })
    .click();
  expect(observed.posts).toEqual([]);
  expect(observed.foreign).toEqual([]);
});
