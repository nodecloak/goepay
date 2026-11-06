import { chromium } from "@playwright/test";

const api = process.env.CHECKOUT_BROWSER_API;
const checkout = process.env.CHECKOUT_BROWSER_URL;
const key = process.env.CHECKOUT_BROWSER_CONTROL;
const trade = process.env.CHECKOUT_BROWSER_TRADE;
const merchantPort = process.env.CHECKOUT_BROWSER_MERCHANT_PORT;
const expired = process.env.CHECKOUT_BROWSER_EXPIRED === "1";
if (
  !/^http:\/\/127\.0\.0\.1:\d+$/.test(api ?? "") ||
  !checkout?.startsWith(api + "/checkout/") ||
  !/^[0-9]+$/.test(trade ?? "") ||
  !/^[a-f0-9]{64}$/.test(key ?? "") ||
  !/^\d{1,5}$/.test(merchantPort ?? "") ||
  Number(merchantPort) > 65535
)
  throw Error("explicit isolated local checkout fixture required");

const browser = await chromium.launch({
  headless: true,
  args: [
    "--no-proxy-server",
    `--host-resolver-rules=MAP merchant.example.invalid:443 127.0.0.1:${merchantPort}, MAP * 127.0.0.1, EXCLUDE localhost`,
  ],
});
try {
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  const page = await context.newPage();
  const errors = [];
  const forbidden = [];
  const navigations = [];
  let statusRequests = 0;
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (
      message.type() === "error" &&
      !(expired && message.text().includes("403"))
    )
      errors.push(message.text());
  });
  page.on("request", (request) => {
    if (request.url().startsWith(api + "/checkout/" + trade + "/status?"))
      statusRequests++;
  });
  page.on("response", (response) => {
    if (response.request().isNavigationRequest())
      navigations.push({ url: response.url(), status: response.status() });
  });
  await context.route("**/*", async (route) => {
    const target = new URL(route.request().url());
    if (target.origin === api) return route.continue();
    if (
      target.origin === "https://merchant.example.invalid" &&
      target.pathname === "/return" &&
      route.request().isNavigationRequest()
    ) {
      return route.continue(); // DNS maps this hostname only to the local TLS fixture.
    }
    forbidden.push(target.origin + target.pathname);
    return route.abort();
  });
  const initial = await page.goto(checkout, { waitUntil: "domcontentloaded" });
  const csp = initial.headers()["content-security-policy"] ?? "";
  if (
    initial.status() !== 200 ||
    !csp.includes("connect-src 'self'") ||
    !/script-src 'nonce-[^']+'/.test(csp)
  )
    throw Error("checkout must use nonce CSP and allow same-origin polling");
  if (expired) {
    await page.waitForResponse(
      (r) =>
        r.url().startsWith(api + "/checkout/" + trade + "/status?") &&
        r.status() === 403,
      { timeout: 12000 },
    );
    await page
      .getByText("页面验证已过期，请回到商户订单页重新打开；已付款请勿重复支付")
      .waitFor({ timeout: 3000 });
    await page.waitForTimeout(4000);
    if (statusRequests !== 1 || errors.length || forbidden.length)
      throw Error(
        "expired token must stop polling safely: " +
          JSON.stringify({ statusRequests, errors, forbidden }),
      );
    await context.close();
    process.exitCode = 0;
  } else {
    const pending = await page.waitForResponse(
      (r) =>
        r.url().startsWith(api + "/checkout/" + trade + "/status?") &&
        r.status() === 200,
      { timeout: 12000 },
    );
    if ((await pending.json()).paid !== false)
      throw Error("first real browser status was not pending");
    const confirmed = await fetch(api + "/__checkout_browser/confirm", {
      method: "POST",
      headers: { "X-Local-Checkout-Test": key },
      signal: AbortSignal.timeout(10000),
    });
    if (confirmed.status !== 204)
      throw Error("local provider confirmation failed");
    await page.waitForURL(/^https:\/\/merchant\.example\.invalid\/return\?/, {
      timeout: 12000,
    });
    const returned = new URL(page.url());
    if (
      returned.searchParams.get("trade_no") !== trade ||
      returned.searchParams.get("trade_status") !== "TRADE_SUCCESS" ||
      !returned.searchParams.get("sign")
    )
      throw Error(
        "merchant return is not the signed successful original order",
      );
    if (
      !navigations.some(
        (n) =>
          n.url.startsWith(api + "/checkout/" + trade + "/return?") &&
          n.status === 302,
      )
    )
      throw Error("browser did not visit the signed local return endpoint");
    if (errors.length || forbidden.length)
      throw Error(
        "browser errors or blocked external traffic: " +
          JSON.stringify({ errors, forbidden }),
      );
    await context.close();
  }
} finally {
  await browser.close();
}
