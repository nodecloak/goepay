import { createServer } from "vite";
import { chromium, expect } from "@playwright/test";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), ".."),
  api = process.env.CREDENTIALS199_API,
  token = process.env.CREDENTIALS199_COOKIE,
  width = Number(process.env.CREDENTIALS199_WIDTH),
  artifacts = process.env.CREDENTIALS199_ARTIFACTS;
if (
  !api?.startsWith("http://127.0.0.1:") ||
  !token ||
  ![375, 1440].includes(width) ||
  !artifacts
)
  throw new Error("explicit synthetic loopback credentials fixture required");
const vite = await createServer({
  root: web,
  configFile: path.join(web, "vite.config.ts"),
  logLevel: "silent",
  server: {
    host: "127.0.0.1",
    port: 0,
    strictPort: true,
    proxy: { "/console": { target: api, changeOrigin: false } },
  },
});
let browser;
try {
  await vite.listen();
  await fs.mkdir(artifacts, { recursive: true });
  const origin = "http://127.0.0.1:" + vite.httpServer.address().port;
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width, height: 1000 },
  });
  let external = 0;
  const errors = [];
  await context.route("**/*", (route) => {
    if (
      !["localhost", "127.0.0.1"].includes(
        new URL(route.request().url()).hostname,
      )
    ) {
      external++;
      return route.abort();
    }
    return route.continue();
  });
  await context.addCookies([
    {
      name: "goepay_merchant",
      value: token,
      url: origin,
      httpOnly: true,
      sameSite: "Lax",
    },
  ]);
  const page = await context.newPage();
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(origin + "/merchant/account/api-info");
  const reset = page.getByRole("button", {
    name: "重置 API 密钥",
    exact: true,
  });
  await expect(reset).toBeEnabled();
  await reset.click();
  let dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("登录密码", { exact: true })
    .fill("incorrect-local-browser-password199");
  await dialog
    .getByRole("button", { name: "确认重置密钥", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("当前登录密码不正确");
  await expect(dialog).toBeVisible();
  await dialog
    .getByLabel("登录密码", { exact: true })
    .fill("local-email197-password");
  const rotated = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname.endsWith("/account/api-key/rotate") &&
      response.status() === 200,
  );
  await dialog
    .getByRole("button", { name: "确认重置密钥", exact: true })
    .click();
  const rotation = await rotated;
  expect(rotation.headers()["cache-control"]).toBe("no-store");
  const newKey = (await rotation.json()).data.api_key;
  expect(typeof newKey).toBe("string");
  expect(newKey.length).toBeGreaterThanOrEqual(24);
  await expect(page.getByLabel("新 API 密钥", { exact: true })).toHaveValue(
    newKey,
  );
  await page
    .getByRole("button", { name: "已保存，隐藏密钥", exact: true })
    .click();
  await expect(page.getByLabel("新 API 密钥", { exact: true })).toHaveCount(0);
  await page.goto(origin + "/merchant/account/settings");
  const account = page.getByRole("region", {
    name: "账户资料与安全设置",
    exact: true,
  });
  await expect(account.getByText("登录密码", { exact: true })).toBeVisible();
  await expect(account.getByText("已设置", { exact: true })).toBeVisible();
  const modify = account
    .getByRole("group", { name: "登录密码", exact: true })
    .getByRole("button", { name: "修改", exact: true });
  await modify.click();
  dialog = page.getByRole("dialog", { name: "修改登录密码", exact: true });
  const current = dialog.getByLabel("当前密码", { exact: true });
  await expect(current).toBeFocused();
  await current.fill("discarded-secret199");
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(modify).toBeFocused();
  await modify.click();
  await expect(current).toHaveValue("");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: path.join(artifacts, "password199-" + width + ".png"),
    fullPage: false,
  });
  await current.fill("incorrect-local-browser-password199");
  await dialog
    .getByLabel("新密码", { exact: true })
    .fill("local-credentials199-browser-next-password");
  await dialog
    .getByLabel("确认密码", { exact: true })
    .fill("local-credentials199-browser-next-password");
  await dialog.getByRole("button", { name: "修改密码", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("当前登录密码不正确");
  await expect(dialog).toBeVisible();
  await current.fill("local-email197-password");
  await dialog.getByRole("button", { name: "修改密码", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "登录工作台", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/merchant\/login\?[^#]*reauth=password/);
  await expect(
    page.getByText("请使用当前有效密码重新登录", { exact: true }),
  ).toBeVisible();
  expect(
    (
      await context.request.get(origin + "/console/v1/merchant/session")
    ).status(),
  ).toBe(401);
  expect(
    await page.evaluate(
      () =>
        Object.keys(localStorage).length + Object.keys(sessionStorage).length,
    ),
  ).toBe(0);
  expect(errors).toEqual([]);
  expect(external).toBe(0);
  console.log(
    JSON.stringify({
      width,
      actual_api: true,
      password_changed: true,
      api_key_rotated: true,
      external_requests: external,
    }),
  );
} finally {
  await browser?.close();
  await vite.close();
}
