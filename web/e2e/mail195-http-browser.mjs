import { createServer } from "vite";
import { chromium, expect } from "@playwright/test";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), ".."),
  api = process.env.MAIL195_API,
  token = process.env.MAIL195_COOKIE,
  artifacts = process.env.MAIL195_ARTIFACTS;
if (!api?.startsWith("http://127.0.0.1:") || !token || !artifacts)
  throw Error("explicit local synthetic fixture required");
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
  const results = [];
  for (const width of [375, 1440]) {
    const context = await browser.newContext({
      viewport: { width, height: 1000 },
    });
    let external = 0;
    const errors = [];
    await context.route("**/*", async (route) => {
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
        name: "goepay_admin",
        value: token,
        url: origin,
        httpOnly: true,
        sameSite: "Lax",
      },
    ]);
    const page = await context.newPage();
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(origin + "/admin/system/settings");
    await page.getByRole("tab", { name: "邮箱配置", exact: true }).click();
    await expect(
      page.getByRole("tab", { name: "查看全部", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await page.getByRole("tab", { name: "邮箱渠道配置", exact: true }).click();
    await expect(
      page.getByRole("tab", { name: "邮箱渠道配置", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await expect(page.getByText("SMTP发信", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "账号设置", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "SMTP发信账号设置",
      exact: true,
    });
    await expect(dialog).toBeVisible();
    await page.getByLabel("SMTP服务器", { exact: true }).fill("127.0.0.1");
    await page
      .getByLabel("SMTP端口", { exact: true })
      .fill(process.env.MAIL195_SMTP_PORT);
    await page
      .getByLabel("邮箱账号", { exact: true })
      .fill("sender195@example.invalid");
    await page
      .getByLabel("邮箱密码", { exact: true })
      .fill("synthetic195-tls-password");
    await page.getByLabel("启用状态", { exact: true }).check();
    await expect(
      page.getByRole("tab", { name: "发送频率配置", exact: true }),
    ).toBeDisabled();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByText("保存成功", { exact: true })).toBeVisible();
    await expect(page.getByText("已开启", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "账号设置", exact: true }).click();
    await expect(page.getByLabel("SMTP服务器", { exact: true })).toHaveValue(
      "127.0.0.1",
    );
    await expect(page.getByLabel("邮箱密码", { exact: true })).toHaveValue("");
    await expect(page.getByLabel("邮箱密码", { exact: true })).toHaveAttribute(
      "placeholder",
      "已设置，留空保留原密码",
    );
    await page
      .getByLabel("邮箱密码", { exact: true })
      .fill("synthetic195-discarded");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await page.getByRole("button", { name: "账号设置", exact: true }).click();
    await expect(page.getByLabel("邮箱密码", { exact: true })).toHaveValue("");
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await page.getByRole("tab", { name: "发送频率配置", exact: true }).click();
    await expect(
      page.getByLabel("同一邮箱发送间隔(秒)", { exact: true }),
    ).toBeVisible();
    await page.getByRole("tab", { name: "邮箱渠道配置", exact: true }).click();
    await page
      .getByRole("button", { name: "发送测试邮件", exact: true })
      .click();
    const testDialog = page.getByRole("dialog", {
      name: "发送测试邮件",
      exact: true,
    });
    await testDialog
      .getByLabel("收件邮箱地址", { exact: true })
      .fill("recipient195@example.test");
    await testDialog.getByRole("button", { name: "发送", exact: true }).click();
    await expect(
      testDialog.getByText("SMTP已接受，实际送达以收件箱为准。", {
        exact: true,
      }),
    ).toBeVisible();
    await testDialog
      .getByRole("button", { name: "查询原发送结果", exact: true })
      .click();
    await expect(
      testDialog.getByText("SMTP已接受，实际送达以收件箱为准。", {
        exact: true,
      }),
    ).toBeVisible();
    await testDialog.getByRole("button", { name: "关闭", exact: true }).click();
    await page.screenshot({
      path: path.join(artifacts, "smtp-" + width + ".png"),
      fullPage: true,
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
    expect(external).toBe(0);
    results.push({
      width,
      actual_http: true,
      password_not_echoed: true,
      original_dialog_and_tabs: true,
      external_requests: external,
    });
    await context.close();
  }
  await fs.writeFile(
    path.join(artifacts, "result.json"),
    JSON.stringify(
      { production: false, real_funds: false, real_mail: false, results },
      null,
      2,
    ) + "\n",
  );
} finally {
  await browser?.close();
  await vite.close();
}
