import { readLocalSandboxEnv } from "./local-sandbox-env";
import { test, expect } from "@playwright/test";
function password() {
  const content = readLocalSandboxEnv(import.meta.url);
  const value = content
    .split(/\r?\n/)
    .find((x) => x.startsWith("GOEPAY_SEED_PASSWORD="))
    ?.slice("GOEPAY_SEED_PASSWORD=".length);
  if (!value) throw new Error("Missing local seed password");
  return value;
}
async function login(page: import("@playwright/test").Page, area: string) {
  await page.goto(`/${area}/dashboard`);
  await page.getByLabel("用户名", { exact: true }).fill(area);
  await page.getByLabel("密码", { exact: true }).fill(password());
  await page.getByRole("button", { name: "登录工作台" }).click();
  await expect(page.locator(".stat")).toHaveCount(4);
}
test("live persisted domain CRUD, scoped reports, account and operational reads", async ({
  page,
}) => {
  test.skip(process.env.GOEPAY_LIVE_E2E !== "1", "Local seeded sandbox only");
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await login(page, "admin");
  await page.goto("/admin/merchant/domain");
  await page.getByRole("button", { name: "新增授权域名" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("商户 ID", { exact: false }).fill("1000");
  const name = `live-${Date.now()}.example.test`;
  await dialog.getByLabel("域名", { exact: false }).fill(name);
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("cell", { name, exact: true })).toBeVisible();
  const row = page
    .getByRole("row")
    .filter({ has: page.getByRole("cell", { name, exact: true }) });
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await dialog.getByLabel("域名", { exact: false }).fill("updated-" + name);
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const updated = page.getByRole("row").filter({
    has: page.getByRole("cell", { name: "updated-" + name, exact: true }),
  });
  await expect(updated).toBeVisible();
  await updated.getByRole("button", { name: "详情", exact: true }).click();
  await expect(dialog).toContainText("updated-" + name);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await updated.getByRole("button", { name: "删除", exact: true }).click();
  await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(
    page.getByRole("cell", { name: "updated-" + name, exact: true }),
  ).toHaveCount(0);
  for (const path of [
    "/admin/trade/stat/daily",
    "/admin/system/log",
    "/admin/system/runtime-log",
    "/admin/system/cron",
    "/admin/system/update",
  ]) {
    await page.goto(path);
    await expect(page.getByRole("table").first()).toBeVisible();
    await expect(page.getByRole("alert")).toHaveCount(0);
  }
  await page.getByRole("button", { name: "退出登录" }).click();
  await login(page, "merchant");
  for (const path of [
    "/merchant/account/settings",
    "/merchant/account/api-info",
    "/merchant/trade/daily-stat",
  ]) {
    await page.goto(path);
    await expect(page.getByRole("alert")).toHaveCount(0);
    if (path.endsWith("/settings"))
      await expect(page.getByLabel("商户名称")).not.toHaveValue("");
    if (path.endsWith("/api-info"))
      await expect(page.getByText("API 密钥（已隐藏）")).toBeVisible();
  }
  expect(errors).toEqual([]);
});
test("live settlement reserve is visible and admin rejection restores its terminal state", async ({
  page,
}) => {
  test.skip(process.env.GOEPAY_LIVE_E2E !== "1", "Local seeded sandbox only");
  await login(page, "merchant");
  await page.goto("/merchant/settle/apply");
  await expect(
    page.getByRole("heading", { name: "申请提现", exact: true, level: 2 }),
  ).toBeVisible();
  const recipientPanel = page.getByRole("group", { name: "结算收款资料" });
  await expect(recipientPanel).not.toContainText("正在读取收款配置");
  if (
    await recipientPanel
      .getByRole("button", { name: "配置收款资料", exact: true })
      .count()
  ) {
    await recipientPanel
      .getByRole("button", { name: "配置收款资料", exact: true })
      .click();
    await recipientPanel
      .getByLabel("新收款账号", { exact: true })
      .fill("local-settlement@example.invalid");
    await recipientPanel
      .getByLabel("新收款姓名", { exact: true })
      .fill("Local Settlement Fixture");
    await recipientPanel
      .getByRole("button", { name: "保存收款版本", exact: true })
      .click();
  }
  await expect(
    recipientPanel.getByTestId("settlement-recipient-current"),
  ).toBeVisible();
  await page.getByLabel("金额（元）").fill("0.01");
  const requestNo = "live-settlement-" + Date.now();
  await page.getByLabel("业务请求号").fill(requestNo);
  const result = page.waitForResponse(
    (r) =>
      r.url().includes("/workflows/settlements") &&
      r.request().method() === "POST",
  );
  await page.getByRole("button", { name: "确认提交", exact: true }).click();
  await page
    .getByRole("button", { name: "按已核对费用提交", exact: true })
    .click();
  const response = await result;
  const body = await response.json();
  expect(response.status(), body.message).toBe(201);
  await expect(page.locator(".alert.success")).toContainText("申请已提交");
  await page.getByRole("button", { name: "退出登录" }).click();
  await login(page, "admin");
  await page.goto("/admin/settle/list");
  const row = page.locator(`[data-settlement-id="${body.data.id}"]`);
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "拒绝", exact: true }).click();
  await page.getByRole("button", { name: "确认拒绝", exact: true }).click();
  await expect(row.locator(".settlement-status")).toHaveText("结算失败");
  await expect(row).toContainText("申请已拒绝");
  const original = await page.request.get(
    `/console/v1/admin/workflows/settlements?limit=100`,
  );
  expect(original.status()).toBe(200);
  expect(
    (await original.json()).data.items.find(
      (r: { id: string }) => r.id === String(body.data.id),
    )?.status,
  ).toBe("rejected");
});
