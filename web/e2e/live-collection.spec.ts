import { readLocalSandboxEnv } from "./local-sandbox-env";
import { test, expect } from "@playwright/test";
test("live aggregate collection link renders a public bound payment form and revokes on disable", async ({
  page,
}) => {
  test.skip(process.env.GOEPAY_LIVE_E2E !== "1", "Local sandbox only");
  test.setTimeout(60000);
  const env = readLocalSandboxEnv(import.meta.url);
  const password = env
    .split(/\r?\n/)
    .find((x) => x.startsWith("GOEPAY_SEED_PASSWORD="))
    ?.slice("GOEPAY_SEED_PASSWORD=".length);
  if (!password) throw new Error("Missing seed password");
  await page.goto("/admin/dashboard");
  await page.getByLabel("用户名", { exact: true }).fill("admin");
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录工作台" }).click();
  await expect(page.locator(".stat")).toHaveCount(4);
  await page.goto("/admin/other/onecode");
  await page
    .getByRole("button", { name: "新增收款码样式", exact: true })
    .click();
  const dialog = page.getByRole("dialog"),
    name = "live-collection-" + Date.now();
  await dialog.getByLabel("名称", { exact: false }).fill(name);
  await dialog
    .getByLabel("展示标题", { exact: false })
    .fill("Local collection test");
  await dialog.getByLabel("背景颜色", { exact: false }).fill("#172e40");
  await dialog.getByLabel("文字颜色", { exact: false }).fill("#ffffff");
  const styleResponse = page.waitForResponse(
    (r) =>
      r.url().split("?")[0].endsWith("/resources/collection-styles") &&
      r.request().method() === "POST",
  );
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const style = await (await styleResponse).json();
  expect(style.code).toBe(0);
  const styleID = String(style.data.id);
  await page.getByRole("button", { name: "退出登录" }).click();
  await page.goto("/merchant/dashboard");
  await page.getByLabel("用户名", { exact: true }).fill("merchant");
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录工作台" }).click();
  await expect(page.locator(".stat")).toHaveCount(4);
  await page.goto("/merchant/onecode");
  await page
    .getByRole("button", { name: "新增收款码资料", exact: true })
    .click();
  await dialog.getByLabel("名称", { exact: false }).fill(name);
  await dialog.getByLabel("样式 ID", { exact: false }).fill(styleID);
  await dialog
    .getByLabel("收款标题", { exact: false })
    .fill("Live bound collection");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  let row = page
    .getByRole("row")
    .filter({ has: page.getByRole("cell", { name, exact: true }) });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "详情", exact: true }).click();
  await dialog.getByRole("button", { name: "生成收款链接" }).click();
  await expect(dialog.getByAltText("聚合收款二维码")).toBeVisible();
  const url = await dialog.getByLabel("收款链接", { exact: true }).inputValue();
  const publicPage = await page.context().newPage();
  await publicPage.goto(url);
  await expect(
    publicPage.getByRole("heading", { name: "Live bound collection" }),
  ).toBeVisible();
  await expect(publicPage.getByLabel("金额（元）")).toBeVisible();
  await publicPage.close();
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await dialog.getByLabel("启用", { exact: true }).uncheck();
  const disabledResponse = page.waitForResponse(
    (response) =>
      /^\/console\/v1\/merchant\/resources\/collection-codes\/[1-9][0-9]*$/.test(
        new URL(response.url()).pathname,
      ) && response.request().method() === "PATCH",
  );
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const disabled = await disabledResponse;
  expect(disabled.ok()).toBe(true);
  expect((await disabled.json()).code).toBe(0);
  await expect(dialog).toBeHidden();
  const revoked = await page.request.get(url);
  expect(revoked.status()).toBeGreaterThanOrEqual(400);
  await row.getByRole("button", { name: "删除", exact: true }).click();
  await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
  await page.getByRole("button", { name: "退出登录" }).click();
  await page.goto("/admin/dashboard");
  await page.getByLabel("用户名", { exact: true }).fill("admin");
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录工作台" }).click();
  await expect(page.locator(".stat")).toHaveCount(4);
  await page.goto("/admin/other/onecode");
  const styleRow = page
    .getByRole("row")
    .filter({ has: page.getByRole("cell", { name, exact: true }) });
  await styleRow.getByRole("button", { name: "删除", exact: true }).click();
  await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
});
