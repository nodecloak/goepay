import { expectNoConsoleErrors, logoutConsole } from "./live-console";
import { readLocalSandboxEnv } from "./local-sandbox-env";
import { test, expect } from "@playwright/test";
import fs from "node:fs";
test("live read audit visits every implemented original menu leaf without application errors", async ({
  page,
}) => {
  test.skip(process.env.GOEPAY_LIVE_E2E !== "1", "Local sandbox audit only");
  test.setTimeout(180000);
  const env = readLocalSandboxEnv(import.meta.url);
  const password = env
    .split(/\r?\n/)
    .find((x) => x.startsWith("GOEPAY_SEED_PASSWORD="))
    ?.slice("GOEPAY_SEED_PASSWORD=".length);
  if (!password) throw new Error("Missing sandbox password");
  const capabilities = JSON.parse(
    fs.readFileSync(new URL("../capabilities.json", import.meta.url), "utf8"),
  ) as { area: string; path: string; status: string }[];
  const failures: string[] = [];
  for (const area of ["admin", "merchant"]) {
    await page.goto("/" + area + "/dashboard");
    await page.getByLabel("用户名", { exact: true }).fill(area);
    await page.getByLabel("密码", { exact: true }).fill(password);
    await page.getByRole("button", { name: "登录工作台" }).click();
    await expect(page.locator(".stat")).toHaveCount(4);
    let complaintsEnabled = true;
    if (area === "admin") {
      const settings = await page.request.get(
        "/console/v1/admin/resources/settings?q=complain_open&limit=100",
      );
      expect(settings.ok()).toBeTruthy();
      const data = await settings.json();
      expect(data.code).toBe(0);
      complaintsEnabled = data.data.items.some(
        (v: { key: string; value: string }) =>
          v.key === "complain_open" && ["true", "1"].includes(v.value),
      );
    }
    for (const item of capabilities.filter(
      (x) => x.area === area && x.status === "partial",
    )) {
      await page.goto("/" + area + item.path);
      await page.waitForLoadState("networkidle");
      if (
        area === "admin" &&
        item.path.startsWith("/complain/") &&
        !complaintsEnabled
      ) {
        const closed = page.getByRole("region", {
          name: "交易投诉未启用",
          exact: true,
        });
        await expect(closed).toBeVisible();
        await expect(
          closed.getByRole("heading", {
            name: "交易投诉尚未启用",
            exact: true,
          }),
        ).toBeVisible();
        await expect(
          closed.getByRole("link", { name: "前往投诉设置" }),
        ).toHaveAttribute("href", "/admin/system/settings?tab=complaint");
        await expect(closed.getByRole("button")).toHaveText("重新检查功能状态");
        await expect(page.getByRole("alert")).toHaveCount(0);
        await expect(page.getByRole("table")).toHaveCount(0);
        test.info().annotations.push({
          type: "feature-disabled",
          description: area + item.path + ": checked closed-gate UX only",
        });
        continue;
      }
      await expectNoConsoleErrors(page);
      const alerts = await page
        .locator('[role="alert"]:not(.dashboard-tip)')
        .allTextContents();
      if (alerts.length)
        failures.push(area + item.path + ": " + alerts.join(" | "));
      if (await page.getByRole("heading", { name: "此功能尚未接入" }).count())
        failures.push(area + item.path + ": implementation route fell through");
    }
    await logoutConsole(page);
  }
  expect(failures).toEqual([]);
});
