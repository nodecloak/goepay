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
    for (const item of capabilities.filter(
      (x) => x.area === area && x.status === "partial",
    )) {
      await page.goto("/" + area + item.path);
      await page.waitForLoadState("networkidle");
      const alerts = await page.getByRole("alert").allTextContents();
      if (alerts.length)
        failures.push(area + item.path + ": " + alerts.join(" | "));
      if (await page.getByRole("heading", { name: "此功能尚未接入" }).count())
        failures.push(area + item.path + ": implementation route fell through");
    }
    await page.getByRole("button", { name: "退出登录" }).click();
  }
  expect(failures).toEqual([]);
});
