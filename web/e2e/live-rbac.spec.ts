import { readLocalSandboxEnv } from "./local-sandbox-env";
import { test, expect } from "@playwright/test";
test("live operator navigation and direct API access enforce server administrator class", async ({
  page,
  browser,
}) => {
  test.skip(process.env.GOEPAY_LIVE_E2E !== "1", "Local sandbox only");
  const data = readLocalSandboxEnv(import.meta.url);
  const seedPassword = data
    .split(/\r?\n/)
    .find((x) => x.startsWith("GOEPAY_SEED_PASSWORD="))
    ?.slice("GOEPAY_SEED_PASSWORD=".length);
  if (!seedPassword) throw new Error("Missing seed password");
  await page.goto("/admin/dashboard");
  await page.getByLabel("用户名", { exact: true }).fill("admin");
  await page.getByLabel("密码", { exact: true }).fill(seedPassword);
  await page.getByRole("button", { name: "登录工作台" }).click();
  await expect(page.locator(".stat")).toHaveCount(4);
  await page.goto("/admin/system/admin-user");
  await page.getByRole("button", { name: "新增管理员", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const username = "live-operator-" + Date.now(),
    password = "local-test-" + crypto.randomUUID();
  await dialog.getByLabel("账号", { exact: false }).fill(username);
  await dialog.getByLabel("名称", { exact: false }).fill("Local operator test");
  await dialog.getByLabel("密码", { exact: false }).fill(password);
  await expect(dialog.getByLabel("权限等级", { exact: false })).toHaveValue(
    "operator",
  );
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const row = page
    .getByRole("row")
    .filter({ has: page.getByRole("cell", { name: username, exact: true }) });
  await expect(row).toBeVisible();
  const context = await browser.newContext({
    baseURL: process.env.GOEPAY_WEB_URL,
  });
  try {
    const operator = await context.newPage();
    await operator.goto("/admin/dashboard");
    await operator.getByLabel("用户名", { exact: true }).fill(username);
    await operator.getByLabel("密码", { exact: true }).fill(password);
    await operator.getByRole("button", { name: "登录工作台" }).click();
    await expect(operator.locator(".stat")).toHaveCount(4);
    await expect(
      operator.getByRole("link", { name: "系统设置", exact: true }),
    ).toHaveCount(0);
    await operator.goto("/admin/system/settings");
    await expect(
      operator.getByRole("heading", { name: "您没有访问此页面的权限" }),
    ).toBeVisible();
    const denied = await operator.request.get(
      "/console/v1/admin/resources/settings",
    );
    expect(denied.status()).toBe(403);
  } finally {
    await context.close();
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await dialog.getByLabel("启用", { exact: true }).uncheck();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.locator(".alert.success")).toContainText("修改已保存");
  }
});
