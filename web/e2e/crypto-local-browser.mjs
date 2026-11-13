import { chromium, expect } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";

const origin = process.env.CRYPTO_BROWSER_ORIGIN;
const token = process.env.CRYPTO_BROWSER_SESSION;
const evidence = process.env.CRYPTO_BROWSER_EVIDENCE;
if (
  !/^http:\/\/127\.0\.0\.1:\d+$/.test(origin || "") ||
  !/^[-_A-Za-z0-9]{43}$/.test(token || "") ||
  !evidence
)
  throw Error("explicit isolated local crypto fixture required");
await mkdir(evidence, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  args: ["--no-proxy-server"],
});
try {
  for (const width of [375, 1440]) {
    const context = await browser.newContext({
      viewport: { width, height: 1000 },
    });
    if (width === 1440)
      await context.addInitScript(() =>
        localStorage.setItem("goepay-console-theme", "light"),
      );
    const outside = [],
      errors = [];
    await context.route("**/*", async (route) => {
      if (new URL(route.request().url()).origin === origin)
        return route.continue();
      outside.push(new URL(route.request().url()).origin);
      return route.abort();
    });
    await context.addCookies([
      {
        name: "goepay_merchant",
        value: token,
        url: origin + "/console/v1/merchant",
        httpOnly: true,
        sameSite: "Strict",
      },
    ]);
    const page = await context.newPage();
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(origin + "/merchant/account/crypto");
    await expect(
      page.locator("[data-form=invoice] button[type=submit]"),
    ).toBeEnabled();
    await expect(
      page.getByText(
        "本地模拟环境：仅使用合成资金及本地链数据，请勿转入真实资产。",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(page.locator("[data-form=address]")).toHaveCount(0);
    await expect(page.locator(".toast--danger")).toHaveCount(0);
    for (const network of ["tron", "ethereum"]) {
      const form = page.locator("[data-form=invoice]");
      await form
        .locator("[name=out_trade_no]")
        .fill(`browser-${network}-${width}`);
      await form.locator("[name=network]").selectOption(network);
      await form.locator("[name=amount]").fill("10.123456");
      const [created] = await Promise.all([
        page.waitForResponse(
          (r) =>
            new URL(r.url()).pathname ===
              "/console/v1/merchant/crypto/invoices" &&
            r.request().method() === "POST",
        ),
        form.locator("button[type=submit]").click(),
      ]);
      expect(created.status()).toBe(200);
      const payload = await created.json();
      const link = page.locator("a[data-checkout]");
      await expect(link).toHaveAttribute("href", payload.data.checkout_url);
      const checkoutURL = new URL(await link.getAttribute("href"), origin).href;
      const checkout = await context.newPage();
      checkout.on("pageerror", (e) => errors.push(e.message));
      const delayedFinality = network === "ethereum" && width === 375;
      if (delayedFinality) {
        await checkout.clock.install();
        const statusURL = new URL(checkoutURL);
        statusURL.pathname += "/status";
        await checkout.route(
          statusURL.href,
          (route) =>
            route.fulfill({
              json: { code: 0, data: { state: "expired", received: "0" } },
            }),
          { times: 1 },
        );
      }
      await checkout.goto(checkoutURL);
      await expect(checkout.locator("#state")).toHaveText(
        "等待付款或链上最终确认",
      );
      await expect(checkout.locator(".warning").first()).toContainText(
        "本地模拟",
      );
      await checkout.locator(".qr").evaluate((img) => img.decode());
      const rects = await checkout.evaluate(() => {
        const qr = document.querySelector(".qr").getBoundingClientRect();
        const main = document.querySelector("main").getBoundingClientRect();
        return {
          delta: Math.abs(qr.x + qr.width / 2 - main.x - main.width / 2),
          overflow: document.documentElement.scrollWidth > innerWidth,
        };
      });
      if (rects.delta > 1 || rects.overflow)
        throw Error(
          "checkout QR not centered or viewport overflows: " +
            JSON.stringify(rects),
        );
      await checkout.screenshot({
        path: path.join(evidence, `checkout-${network}-${width}.png`),
        fullPage: true,
        animations: "disabled",
      });
      const id = new URL(checkoutURL).pathname.split("/").at(-1);
      if (delayedFinality) {
        await checkout.clock.fastForward(3100);
        await expect(checkout.locator("#state")).toContainText("收款单已过期");
        await checkout.screenshot({
          path: path.join(evidence, "checkout-expired-ethereum-375.png"),
          fullPage: true,
          animations: "disabled",
        });
      }
      const res = await context.request.post(
        origin + "/__crypto_browser/deposit",
        { headers: { "X-Synthetic-Control": token }, data: { id } },
      );
      expect(res.status()).toBe(204);
      if (delayedFinality) await checkout.clock.fastForward(16000);
      await expect(checkout.locator("#state")).toHaveText("支付已到账", {
        timeout: 10000,
      });
      await expect(checkout.locator("#received")).toHaveText("10.123456");
      await checkout.close();
    }
    if (width === 375) {
      const recipient = await context.request.post(
        origin + "/__crypto_browser/recipient",
        { headers: { "X-Synthetic-Control": token } },
      );
      expect(recipient.status()).toBe(204);
      await page.getByRole("button", { name: "刷新记录", exact: true }).click();
      const form = page.locator("[data-form=withdrawal]");
      await expect(form.locator("button[type=submit]")).toBeEnabled();
      const requestID = await form.locator("[name=request_id]").inputValue();
      await form.locator("[name=network]").selectOption("tron");
      await form
        .locator("[name=address]")
        .fill("TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE");
      await form.locator("[name=amount]").fill("1.000000");
      await form
        .locator("[name=current_password]")
        .fill("local-email197-password");
      let financialPosts = 0;
      await context.route(
        "**/console/v1/merchant/crypto/withdrawals",
        async (route) => {
          if (route.request().method() !== "POST") return route.continue();
          financialPosts++;
          const accepted = await route.fetch();
          expect(accepted.status()).toBe(200);
          await route.abort("failed");
        },
      );
      await form.locator("button[type=submit]").click();
      await expect(
        page.getByRole("heading", { name: "原资金请求待核对" }),
      ).toBeVisible();
      await page.reload();
      await expect(
        page.getByText(new RegExp("已找回原请求 " + requestID)),
      ).toBeVisible();
      await expect(
        page.locator("[data-form=withdrawal] input[name=request_id]"),
      ).toHaveValue(requestID);
      expect(financialPosts).toBe(1);
      const persisted = await page.evaluate(() =>
        Object.keys(localStorage)
          .filter((k) => k.startsWith("goepay.crypto-intent."))
          .map((k) => localStorage.getItem(k))
          .join(""),
      );
      expect(persisted).not.toContain("local-email197-password");
      await page.screenshot({
        path: path.join(evidence, "financial-request-recovered-375.png"),
        fullPage: true,
        animations: "disabled",
      });
    }
    await page.getByRole("button", { name: "刷新记录", exact: true }).click();
    await expect(page.getByText("支付已到账", { exact: true })).toHaveCount(0);
    await page.screenshot({
      path: path.join(evidence, `console-${width}.png`),
      fullPage: true,
      animations: "disabled",
    });
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    );
    if (overflow) throw Error("console viewport overflows at " + width);
    if (outside.length || errors.length)
      throw Error(JSON.stringify({ outside, errors }));
    await context.close();
  }
  console.log(
    JSON.stringify({
      ok: true,
      widths: [375, 1440],
      networks: ["tron", "ethereum"],
      confirmed: 4,
    }),
  );
} finally {
  await browser.close();
}
