import { readLocalSandboxEnv, localSandboxBase } from "./local-sandbox-env";
import { test, expect } from "@playwright/test";
test.use({ trace: "off" });

test("live Docker templates render in the protected iframe and both printing settings stay local", async ({
  page,
}) => {
  test.skip(
    process.env.GOEPAY_LIVE_TEMPLATE_PRINTING_E2E !== "1",
    "Explicit local sandbox verification only",
  );
  localSandboxBase();
  const password = readLocalSandboxEnv(import.meta.url)
    .split(/\r?\n/)
    .find((line) => line.startsWith("GOEPAY_SEED_PASSWORD="))
    ?.slice("GOEPAY_SEED_PASSWORD=".length);
  if (!password) throw new Error("Local seed credential unavailable");
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  async function login(role: string) {
    const response = await page.request.post(`/console/v1/${role}/login`, {
      data: { username: role, password },
    });
    expect(response.status()).toBe(200);
    expect((await response.json()).code).toBe(0);
  }
  await login("admin");
  const dashboard = await page.request.get("/console/v1/admin/dashboard");
  expect(dashboard.status()).toBe(200);
  expect((await dashboard.json()).data.sandbox).toBe(true);
  await page.goto("/admin/app/template");
  await expect(
    page.getByRole("navigation", { name: "模板类型", exact: true }),
  ).toBeVisible();
  // The page opens on 首页模板; the checks below are about the pay frame.
  await page.getByRole("button", { name: "支付页模板", exact: true }).click();
  await page
    .getByRole("button", { name: "预览已保存配置", exact: true })
    .click();
  const frame = page.frameLocator('iframe[title="内置模板预览"]');
  await expect(frame.locator("body")).toContainText("PREVIEW-ONLY");
  await expect(frame.locator("body")).toContainText("没有支付按钮，不创建订单");
  expect(
    await frame
      .locator("body")
      .evaluate((el) => getComputedStyle(el).backgroundColor),
  ).not.toBe("rgba(0, 0, 0, 0)");
  expect(await frame.locator("form,script,button").count()).toBe(0);
  const previewPath = await page.locator("iframe").getAttribute("src");
  const preview = await page.request.get(previewPath!);
  expect(preview.headers()["content-security-policy"]).toContain(
    "frame-ancestors 'self'",
  );
  expect(preview.headers()["content-security-policy"]).toContain(
    "script-src 'none'",
  );
  expect(preview.headers()["cache-control"]).toContain("no-store");
  await page.goto("/admin/system/printing");
  await expect(
    page.getByRole("region", { name: "平台打印配置", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("打印服务")).toBeVisible();
  await expect(page.locator("main")).toContainText("外部执行默认关闭");
  const platform = await page.request.get(
    "/console/v1/admin/printing/settings",
  );
  expect(platform.status()).toBe(200);
  // This advertises the installed fixed connector, not a running worker.
  expect((await platform.json()).data.external_execution_available).toBe(true);
  await login("merchant");
  await page.goto("/merchant/account/printing");
  await expect(
    page.getByRole("region", { name: "我的订单打印设置", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("自动打印范围")).toBeVisible();
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  expect(errors).toEqual([]);
});
