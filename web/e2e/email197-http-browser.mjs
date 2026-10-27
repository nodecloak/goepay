import { createServer } from "vite";
import { chromium, expect } from "@playwright/test";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), ".."),
  api = process.env.EMAIL197_API,
  token = process.env.EMAIL197_COOKIE,
  width = Number(process.env.EMAIL197_WIDTH),
  artifacts = process.env.EMAIL197_ARTIFACTS;
const existing = process.env.EMAIL197_MODE === "existing",
  artifactPrefix = existing ? "existing-" : "";
if (
  !api?.startsWith("http://127.0.0.1:") ||
  !token ||
  ![375, 1440].includes(width) ||
  !artifacts
)
  throw Error("explicit synthetic loopback fixture required");
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
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(origin + "/merchant/account/settings");
  if (process.env.EMAIL198_UID) {
    const response = await context.request.get(
      origin + "/console/v1/merchant/account/profile",
    );
    expect(response.ok()).toBe(true);
    const data = (await response.json()).data;
    expect(data.uid).toBe(process.env.EMAIL198_UID);
    expect(data.registration_time_known).toBe(true);
    expect(data.addtime).toMatch(
      /^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$/,
    );
    const facts = page.locator('dl[aria-label="商户基本资料"]');
    await expect(facts).toContainText("商户ID");
    await expect(facts).toContainText(data.uid);
    await expect(facts).toContainText("注册时间");
    await expect(facts).toContainText(data.addtime);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: path.join(
        artifacts,
        "profile198-" + artifactPrefix + width + ".png",
      ),
      fullPage: false,
    });
  }
  const change = page.locator('[data-email197="change"]');
  await expect(change).toBeEnabled();
  await change.click();
  let dialog = page.getByRole("dialog", { name: "安全验证", exact: true });
  await expect(dialog).toBeVisible();
  if (existing)
    await expect(dialog.getByLabel("原绑定邮箱", { exact: true })).toHaveValue(
      "old197@example.test",
    );
  else
    await dialog
      .getByLabel("当前登录密码", { exact: true })
      .fill("discarded197-local-secret");
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(change).toBeFocused();
  await change.click();
  if (!existing) {
    await dialog
      .getByLabel("当前登录密码", { exact: true })
      .fill("incorrect-local-password197");
    await dialog.getByRole("button", { name: "下一步", exact: true }).click();
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("alert")).toContainText("当前登录密码不正确");
    await expect(
      page.getByRole("heading", { name: "商户登录", exact: true }),
    ).toHaveCount(0);
  }
  if (existing) {
    await expect(dialog.getByLabel("原绑定邮箱", { exact: true })).toHaveValue(
      "old197@example.test",
    );
    await expect(dialog.getByLabel("新邮箱", { exact: true })).toHaveCount(0);
    await dialog
      .getByRole("button", { name: "获取验证码", exact: true })
      .click();
    await expect(dialog.locator('[data-email197="send"]')).toContainText("秒");
    await expect(dialog.locator('[data-email197="send"]')).toBeDisabled();
    const proof = await context.request.get(api + "/__email197/code", {
      headers: { "X-Synthetic-Fixture": token },
    });
    expect(proof.ok()).toBe(true);
    const received = await proof.json();
    expect(received.code).toMatch(/^[1-9][0-9]{6}$/);
    await dialog.getByLabel("验证码", { exact: true }).fill(received.code);
    await page.screenshot({
      path: path.join(artifacts, "existing-old-" + width + ".png"),
      fullPage: true,
    });
  } else
    await dialog
      .getByLabel("当前登录密码", { exact: true })
      .fill("local-email197-password");
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "修改绑定邮箱", exact: true });
  await expect(dialog).toBeVisible();
  await dialog
    .getByLabel("新邮箱", { exact: true })
    .fill("recipient195@example.test");
  await dialog.getByRole("button", { name: "获取验证码", exact: true }).click();
  const send = dialog.locator('[data-email197="send"]');
  await expect(send).toBeDisabled();
  await expect(send).toContainText("秒");
  const response = await context.request.get(api + "/__email197/code", {
    headers: { "X-Synthetic-Fixture": token },
  });
  expect(response.ok()).toBe(true);
  const received = await response.json();
  expect(received.code).toMatch(/^[1-9][0-9]{6}$/);
  await dialog
    .getByRole("button", { name: "查询验证状态", exact: true })
    .click();
  await expect(send).toBeDisabled();
  await dialog.getByLabel("验证码", { exact: true }).fill(received.code);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: path.join(
      artifacts,
      artifactPrefix + "verified-target-" + width + ".png",
    ),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "确认修改", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page).toHaveURL(/\/merchant\/login\?[^#]*reauth=security/);
  await expect(
    page.getByText("请重新登录以继续操作", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "商户登录", exact: true }),
  ).toBeVisible();
  expect(errors).toEqual([]);
  expect(external).toBe(0);
  await page.screenshot({
    path: path.join(
      artifacts,
      artifactPrefix + "reauthentication-" + width + ".png",
    ),
    fullPage: true,
  });
  await fs.writeFile(
    path.join(artifacts, artifactPrefix + "result-" + width + ".json"),
    JSON.stringify(
      {
        width,
        existing_binding: existing,
        actual_http: true,
        actual_private_tls: true,
        external_requests: external,
        production: false,
        real_funds: false,
      },
      null,
      2,
    ) + "\n",
  );
  await context.close();
} finally {
  await browser?.close();
  await vite.close();
}
