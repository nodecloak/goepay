import { createServer } from "vite";
import { chromium, expect } from "@playwright/test";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const api = process.env.SCAN192_BROWSER_API,
  token = process.env.SCAN192_BROWSER_COOKIE,
  artifacts = process.env.SCAN192_BROWSER_ARTIFACTS;
if (!api?.startsWith("http://127.0.0.1:") || !token || !artifacts)
  throw new Error("explicit synthetic local fixture required");
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
  await fs.mkdir(artifacts, { recursive: true });
  await vite.listen();
  const origin = "http://127.0.0.1:" + vite.httpServer.address().port;
  browser = await chromium.launch({ headless: true });
  const results = [];
  for (const width of [375, 1440]) {
    const context = await browser.newContext({
      viewport: { width, height: 920 },
    });
    let external = 0;
    const errors = [];
    await context.route("**/*", async (route) => {
      if (
        !["127.0.0.1", "localhost"].includes(
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
    await page.goto(origin + "/merchant/scanpay");
    await expect(
      page.getByRole("heading", { name: "付款码收款", exact: true }),
    ).toBeVisible();
    for (const key of ["1", ".", "2", "3"])
      await page
        .getByRole("button", { name: "金额键 " + key, exact: true })
        .click();
    await page.screenshot({
      path: path.join(artifacts, "keypad-" + width + ".png"),
    });
    await page.getByRole("button", { name: "扫一扫收款", exact: true }).click();
    await page.getByLabel("付款码", { exact: true }).fill("134567890123456789");
    await page.getByRole("button", { name: "核对收款", exact: true }).click();
    const confirm = page.getByRole("button", {
      name: "确认收款 ¥1.23",
      exact: true,
    });
    await expect(confirm).toBeVisible();
    await page.screenshot({
      path: path.join(artifacts, "prepared-" + width + ".png"),
    });
    await confirm.click();
    await expect(
      page.getByRole("heading", { name: "收款成功", exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: path.join(artifacts, "paid-" + width + ".png"),
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(
      await page.evaluate(
        () =>
          Object.values(sessionStorage).join() +
          Object.values(localStorage).join(),
      ),
    ).not.toContain("134567890123456789");
    expect(external).toBe(0);
    expect(errors).toEqual([]);
    results.push({
      width,
      actual_http: true,
      signed_local_tls: true,
      paid: true,
      external_requests: external,
    });
    await context.close();
  }
  await fs.writeFile(
    path.join(artifacts, "result.json"),
    JSON.stringify({ production: false, real_funds: false, results }, null, 2) +
      "\n",
  );
} finally {
  await browser?.close();
  await vite.close();
}
