import { readLocalSandboxEnv } from "./local-sandbox-env";
import { test, expect } from "@playwright/test";
test.use({ trace: "off" });
test("live Docker admin and merchant sessions, dashboard, lists, and logout", async ({
  page,
}) => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1",
    "Enable only against seeded local sandbox",
  );
  const content = readLocalSandboxEnv(import.meta.url);
  const password = content
    .split(/\r?\n/)
    .find((x) => x.startsWith("GOEPAY_SEED_PASSWORD="))
    ?.slice("GOEPAY_SEED_PASSWORD=".length);
  if (!password) throw new Error("Missing local sandbox seed password");
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  for (const area of ["admin", "merchant"]) {
    await page.goto(`/${area}/dashboard`);
    await page.getByLabel("用户名").fill(area);
    await page.getByLabel("密码", { exact: true }).fill(password);
    await page.getByRole("button", { name: "登录工作台" }).click();
    // A failed login must not leave the seed credential in failure snapshots.
    const passwordField = page.getByLabel("密码", { exact: true });
    if (await passwordField.isVisible()) await passwordField.fill("");
    await expect(page.locator(".stat")).toHaveCount(4);
    await expect(page.getByRole("alert")).toHaveCount(0);
    await page.getByRole("link", { name: "订单管理", exact: true }).click();
    await expect(page.getByRole("heading", { name: "订单管理" })).toBeVisible();
    await expect(page.getByRole("table")).toBeVisible();
    await page.setViewportSize({ width: 375, height: 812 });
    await expect(page.getByRole("button", { name: "展开菜单" })).toBeVisible();
    await expect
      .poll(async () => {
        const box = await page.locator(".console-sidebar").boundingBox();
        return box ? box.x + box.width : Infinity;
      })
      .toBeLessThanOrEqual(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: `test-results/live-${area}-mobile.png`,
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.getByRole("button", { name: "退出登录" }).click();
    await expect(
      page.getByRole("button", { name: "登录工作台" }),
    ).toBeVisible();
  }
  expect(errors).toEqual([]);
});
