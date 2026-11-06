import { chromium, expect } from "@playwright/test";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const fixture = JSON.parse(process.env.WECHAT_BROWSER240_FIXTURE ?? "{}");
const artifacts = path.resolve(
  process.env.GOEPAY_WECHAT_BROWSER240_ARTIFACTS ?? "",
);
const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
);
const owned = path.join(root, ".tools", "wechat-checkout240", "owned-run");
const inside = (base, target) => {
  const rel = path.relative(base, target);
  return (
    rel !== ".." && !rel.startsWith(".." + path.sep) && !path.isAbsolute(rel)
  );
};
if (
  !process.env.GOEPAY_WECHAT_BROWSER240_ARTIFACTS ||
  !inside(owned, artifacts)
)
  throw Error("explicit owned browser240 artifact directory required");
if (
  !/^http:\/\/127\.0\.0\.1:\d+$/.test(fixture.api ?? "") ||
  !/^https:\/\/127\.0\.0\.1:\d+$/.test(fixture.peerLocal ?? "") ||
  !/^[a-f0-9]{64}$/.test(fixture.control ?? "")
)
  throw Error(
    "only explicit loopback fixture peers and synthetic control token permitted",
  );
for (const method of ["native", "h5", "jsapi", "app", "closed"]) {
  const order = fixture.orders?.[method];
  if (
    !/^\d+$/.test(order?.trade ?? "") ||
    !order.checkout?.startsWith(fixture.api + "/checkout/" + order.trade + "?")
  )
    throw Error("original signed synthetic checkout required: " + method);
}
await fs.mkdir(artifacts, { recursive: true });
if (!inside(await fs.realpath(owned), await fs.realpath(artifacts)))
  throw Error("artifact symlink escaped owned directory");

const h5Origin = "https://wx.tenpay.com";
const merchantOrigin = "https://merchant.example.invalid";
const report = {
  syntheticOnly: true,
  actualExternalNetworkRequests: 0,
  bridgeSimulation:
    "Only native WeixinJSBridge readiness/invoke/callback boundary; real Go HTML, DOM, signed SDK materials and callbacks",
  qrPayloadLimitation:
    "Native PNG bytes match original material; continuation source and same-order URL verified, payload decoding belongs to HTTP tests",
  virtualOrigins: [h5Origin, merchantOrigin],
  loopbackPeers: [fixture.api, fixture.peerLocal],
  scenarios: [],
  navigations: [],
  navigationRequests: [],
  statusReads: [],
  imageRequests: [],
  merchantReturns: [],
  errors: [],
  forbidden: [],
};
const port = new URL(fixture.peerLocal).port;
const browser = await chromium.launch({
  headless: true,
  args: [
    "--no-proxy-server",
    "--disable-background-networking",
    `--host-resolver-rules=MAP wx.tenpay.com:443 127.0.0.1:${port}, MAP merchant.example.invalid:443 127.0.0.1:${port}, MAP * ~NOTFOUND, EXCLUDE localhost, EXCLUDE 127.0.0.1`,
  ],
});
const contexts = [];
const desktopUA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0.0.0 Safari/537.36";
const mobileUA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1";
const md5 = (params) =>
  createHash("md5")
    .update(
      [...params.entries()]
        .filter(
          ([key, value]) =>
            !["sign", "sign_type"].includes(key) && value !== "",
        )
        .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
        .map(([key, value]) => `${key}=${value}`)
        .join("&") + "test-secret",
    )
    .digest("hex");
function verifyMerchant(target) {
  const q = target.searchParams;
  const order = Object.values(fixture.orders).find(
    (row) => row.trade === q.get("trade_no"),
  );
  if (
    target.origin !== merchantOrigin ||
    target.pathname !== "/return" ||
    !order ||
    q.get("out_trade_no") !== "actual-browser240-" + order.method ||
    q.get("pid") !== "1" ||
    q.get("money") !== "66.50" ||
    q.get("type") !== "wxpay" ||
    q.get("trade_status") !== "TRADE_SUCCESS" ||
    q.get("sign") !== md5(q)
  )
    throw Error("merchant return must authenticate original paid order");
  return order.trade;
}
async function newPage(
  name,
  {
    mobile = false,
    wechat = false,
    javaScriptEnabled = true,
    bridge = false,
  } = {},
) {
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: mobile ? 375 : 1280, height: mobile ? 812 : 900 },
    isMobile: mobile,
    hasTouch: mobile,
    serviceWorkers: "block",
    javaScriptEnabled,
    userAgent:
      (mobile ? mobileUA : desktopUA) +
      (wechat ? " MicroMessenger/8.0.50" : ""),
  });
  contexts.push(context);
  if (bridge)
    await context.addInitScript(() => {
      window.__wechat240 = { calls: [], callbacks: [] };
    });
  const page = await context.newPage();
  const session = await context.newCDPSession(page);
  await session.send("Page.enable");
  session.on("Page.frameRequestedNavigation", (event) => {
    report.navigationRequests.push({
      scenario: name,
      url: event.url,
      reason: event.reason,
    });
    if (!/^https?:/.test(event.url))
      report.errors.push({
        scenario: name,
        message: "unsupported automatic custom scheme navigation: " + event.url,
      });
  });
  page.on("pageerror", (error) =>
    report.errors.push({ scenario: name, message: error.message }),
  );
  page.on("console", (message) => {
    if (
      message.type() === "error" &&
      !message.text().includes("403 (Forbidden)")
    )
      report.errors.push({ scenario: name, message: message.text() });
  });
  context.on("requestfailed", (request) => {
    const reason = request.failure()?.errorText ?? "";
    if (!reason.includes("ERR_ABORTED"))
      report.errors.push({
        scenario: name,
        message: reason + " " + request.url(),
      });
  });
  context.on("response", (response) => {
    const request = response.request(),
      target = new URL(response.url());
    if (request.isNavigationRequest())
      report.navigations.push({
        scenario: name,
        origin: target.origin,
        path: target.pathname,
        status: response.status(),
      });
    if (target.origin === fixture.api && target.pathname.endsWith("/status"))
      report.statusReads.push({
        scenario: name,
        path: target.pathname,
        status: response.status(),
      });
    if (
      response.status() >= 400 &&
      !(name.startsWith("tampered-") && response.status() === 403)
    )
      report.errors.push({
        scenario: name,
        message: "HTTP " + response.status() + " " + target.pathname,
      });
    if (
      request.isNavigationRequest() &&
      target.origin === merchantOrigin &&
      response.status() === 200
    ) {
      try {
        report.merchantReturns.push({
          scenario: name,
          trade: verifyMerchant(target),
          signatureVerified: true,
        });
      } catch (error) {
        report.errors.push({ scenario: name, message: error.message });
      }
    }
  });
  await context.route("**/*", async (route) => {
    const request = route.request();
    try {
      const target = new URL(request.url());
      if (
        [fixture.api, h5Origin, merchantOrigin].includes(target.origin) &&
        target.pathname === "/favicon.ico"
      )
        return route.fulfill({ status: 204, body: "" });
      if (
        target.origin === fixture.api &&
        request.method() === "GET" &&
        /^\/checkout\/\d+(?:\/(?:status|return|qr\.png|browser-qr\.png))?$/.test(
          target.pathname,
        )
      ) {
        if (target.pathname.endsWith(".png"))
          report.imageRequests.push({ scenario: name, path: target.pathname });
        return route.continue();
      }
      if (
        target.origin === h5Origin &&
        request.method() === "GET" &&
        request.isNavigationRequest() &&
        request.url() === fixture.orders.h5.material
      ) {
        const headers = await request.allHeaders();
        if (headers.referer !== fixture.api + "/")
          throw Error(
            "mobile H5 navigation must send only checkout origin Referer",
          );
        return route.continue(); // Browser TCP reaches only the mapped loopback TLS peer.
      }
      if (
        target.origin === merchantOrigin &&
        request.method() === "GET" &&
        request.isNavigationRequest()
      ) {
        verifyMerchant(target);
        return route.continue();
      }
      report.forbidden.push({
        scenario: name,
        method: request.method(),
        url: request.url(),
      });
      return route.abort("blockedbyclient");
    } catch (error) {
      report.errors.push({ scenario: name, message: error.message });
      await route.abort("failed").catch(() => {});
    }
  });
  return page;
}
async function pending(page, order, name) {
  const response = await page.waitForResponse(
    (row) =>
      row
        .url()
        .startsWith(fixture.api + "/checkout/" + order.trade + "/status?") &&
      row.status() === 200,
    { timeout: 11000 },
  );
  expect((await response.json()).paid, name + " sees local pending state").toBe(
    false,
  );
}
async function checkout(page, order) {
  const response = await page.goto(order.checkout, {
    waitUntil: "domcontentloaded",
  });
  expect(response.status()).toBe(200);
  const csp = response.headers()["content-security-policy"] ?? "";
  expect(csp).toContain("connect-src 'self'");
  expect(csp).toMatch(/script-src 'nonce-[^']+'/);
  await expect(page.locator("main")).toContainText("66.50");
  return response;
}
async function qrLayout(page, order, kind, name) {
  const source =
    "/checkout/" +
    order.trade +
    "/" +
    (kind === "native" ? "qr.png" : "browser-qr.png");
  const img = page.locator(`img[src*="${source}"]`);
  await expect(img).toHaveCount(1);
  await expect(img).toBeVisible();
  await expect
    .poll(() =>
      img.evaluate(
        (node) =>
          node.complete && node.naturalWidth > 0 && node.naturalHeight > 0,
      ),
    )
    .toBe(true);
  await img.scrollIntoViewIfNeeded();
  const bounds = await img.evaluate((node) => {
    const rect = node.getBoundingClientRect(),
      card = node.closest("main").getBoundingClientRect();
    const canvas = document.createElement("canvas");
    canvas.width = node.naturalWidth;
    canvas.height = node.naturalHeight;
    const context = canvas.getContext("2d");
    context.drawImage(node, 0, 0);
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data;
    const white = (x, y) => {
      const i = (y * canvas.width + x) * 4;
      return (
        pixels[i] > 245 &&
        pixels[i + 1] > 245 &&
        pixels[i + 2] > 245 &&
        pixels[i + 3] > 245
      );
    };
    let quiet = true,
      dark = false;
    for (let x = 0; x < canvas.width; x++)
      for (let edge = 0; edge < 4; edge++)
        quiet &&= white(x, edge) && white(x, canvas.height - 1 - edge);
    for (let y = 0; y < canvas.height; y++)
      for (let edge = 0; edge < 4; edge++)
        quiet &&= white(edge, y) && white(canvas.width - 1 - edge, y);
    for (let i = 0; i < pixels.length; i += 4)
      if (pixels[i] < 100 && pixels[i + 1] < 100 && pixels[i + 2] < 100) {
        dark = true;
        break;
      }
    const clipped = [];
    for (
      let parent = node.parentElement;
      parent;
      parent = parent.parentElement
    ) {
      const style = getComputedStyle(parent),
        box = parent.getBoundingClientRect();
      if (
        /(hidden|clip|scroll|auto)/.test(
          style.overflow + style.overflowX + style.overflowY,
        ) &&
        (rect.left < box.left - 1 ||
          rect.right > box.right + 1 ||
          rect.top < box.top - 1 ||
          rect.bottom > box.bottom + 1)
      )
        clipped.push(parent.tagName);
    }
    return {
      x: rect.x,
      y: rect.y,
      width: rect.width,
      height: rect.height,
      cardX: card.x,
      cardWidth: card.width,
      viewport: innerWidth,
      viewportHeight: innerHeight,
      overflow: document.documentElement.scrollWidth > innerWidth,
      quiet,
      dark,
      clipped,
    };
  });
  expect(
    Math.abs(bounds.x + bounds.width / 2 - bounds.cardX - bounds.cardWidth / 2),
    "QR center matches card center",
  ).toBeLessThanOrEqual(1);
  expect(
    Math.abs(bounds.width - bounds.height),
    "QR is square",
  ).toBeLessThanOrEqual(1);
  expect(bounds.x).toBeGreaterThanOrEqual(0);
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(bounds.viewport + 1);
  expect(bounds.y).toBeGreaterThanOrEqual(0);
  expect(bounds.y + bounds.height).toBeLessThanOrEqual(
    bounds.viewportHeight + 1,
  );
  expect(bounds.overflow, "checkout has no horizontal overflow").toBe(false);
  expect(bounds.clipped, "all four QR quiet zones remain visible").toEqual([]);
  expect(
    bounds.quiet && bounds.dark,
    "actual source PNG preserves white quiet zones and QR modules",
  ).toBe(true);
  const src = new URL(await img.getAttribute("src"), page.url());
  expect(src.origin).toBe(fixture.api);
  expect(src.pathname).toBe(source);
  expect(src.searchParams.get("token")).toBe(
    new URL(order.checkout).searchParams.get("token"),
  );
  if (kind === "native") {
    const response = await page.request.get(src.href);
    expect(
      createHash("sha256")
        .update(await response.body())
        .digest("hex"),
      "native PNG encodes original SDK material",
    ).toBe(order.qrHash);
    await expect(page.locator("main")).toContainText("微信");
  }
  await page.screenshot({
    path: path.join(artifacts, name + ".png"),
    fullPage: true,
  });
  return bounds;
}
async function confirm(page, order) {
  const response = await page
    .context()
    .request.post(fixture.api + "/__wechat_browser240/confirm", {
      headers: { "X-Local-Wechat240": fixture.control },
      data: { trade: order.trade },
    });
  expect(response.status(), "real Go authenticated signed callback").toBe(204);
}
async function merchant(page, order) {
  await page.waitForURL(
    (url) => url.origin === merchantOrigin && url.pathname === "/return",
    { timeout: 16000 },
  );
  await expect(
    page.getByRole("heading", {
      name: "Verified synthetic merchant return240",
    }),
  ).toBeVisible();
  expect(verifyMerchant(new URL(page.url()))).toBe(order.trade);
}
const localReturnCount = (name) =>
  report.navigations.filter(
    (row) =>
      row.scenario === name &&
      row.origin === fixture.api &&
      row.path.endsWith("/return"),
  ).length;
const desktopPages = {};
try {
  for (const method of ["native", "h5", "jsapi", "app"]) {
    const order = fixture.orders[method],
      name = "desktop-" + method;
    const page = await newPage(name);
    desktopPages[method] = page;
    await checkout(page, order);
    const wide = await qrLayout(
      page,
      order,
      method === "native" ? "native" : "continuation",
      name + "-1280",
    );
    await page.setViewportSize({ width: 375, height: 812 });
    const narrow = await qrLayout(
      page,
      order,
      method === "native" ? "native" : "continuation",
      name + "-375",
    );
    await page.setViewportSize({ width: 1280, height: 900 });
    await pending(page, order, name);
    expect(page.url()).toBe(order.checkout);
    expect(localReturnCount(name), "unpaid desktop never returns early").toBe(
      0,
    );
    if (method !== "native") {
      const continuations = await page
        .locator("a[href]")
        .evaluateAll((nodes) => nodes.map((node) => node.href));
      expect(
        continuations.some((href) => {
          const target = new URL(href);
          return (
            target.origin === fixture.api &&
            target.pathname === "/checkout/" + order.trade
          );
        }),
        "same original order browser continuation",
      ).toBe(true);
    }
    report.scenarios.push({
      name,
      originalMethod: method,
      widths: [1280, 375],
      wide,
      narrow,
      pendingLocalOnly: true,
    });
  }

  const native = await newPage("mobile-native", { mobile: true });
  await checkout(native, fixture.orders.native);
  await qrLayout(native, fixture.orders.native, "native", "mobile-native-375");
  await pending(native, fixture.orders.native, "mobile-native");
  expect(native.url()).toBe(fixture.orders.native.checkout);
  expect(
    await native.locator('a[href^="weixin:"]').count(),
    "native is scan material, without unsupported app conversion",
  ).toBe(0);
  await confirm(native, fixture.orders.native);
  await Promise.all([
    merchant(native, fixture.orders.native),
    merchant(desktopPages.native, fixture.orders.native),
  ]);
  report.scenarios.push({
    name: "mobile-native",
    width: 375,
    originalNativePNG: true,
    signedCallbackAndMerchantReturn: true,
  });

  const insideH5 = await newPage("wechat-h5", { mobile: true, wechat: true });
  await checkout(insideH5, fixture.orders.h5);
  await expect(insideH5.locator("main")).toContainText(/浏览器|外部/);
  await pending(insideH5, fixture.orders.h5, "wechat-h5");
  expect(
    insideH5.url(),
    "inside WeChat H5 must guide opening in external browser",
  ).toBe(fixture.orders.h5.checkout);
  await insideH5.screenshot({
    path: path.join(artifacts, "wechat-h5-guide375.png"),
    fullPage: true,
  });

  const fallback = await newPage("mobile-h5-fallback", {
    mobile: true,
    javaScriptEnabled: false,
  });
  const fallbackResponse = await checkout(fallback, fixture.orders.h5);
  expect(fallbackResponse.headers()["referrer-policy"]).toBe(
    "strict-origin-when-cross-origin",
  );
  expect(
    await fallback
      .locator("a[href]")
      .evaluateAll((nodes) => nodes.map((node) => node.href)),
    "manual fallback retains complete original HTTPS material",
  ).toContain(fixture.orders.h5.material);
  const originalLink = fallback.locator(
    "a[href=" + JSON.stringify(fixture.orders.h5.material) + "]",
  );
  await expect(originalLink).toBeVisible();
  await fallback.screenshot({
    path: path.join(artifacts, "mobile-h5-manual375.png"),
    fullPage: true,
  });
  await originalLink.click();
  await expect(
    fallback.getByRole("heading", {
      name: "Original synthetic WeChat H5 order240",
    }),
  ).toBeVisible();

  const outsideH5 = await newPage("mobile-h5-auto", { mobile: true });
  const responseH5 = await outsideH5.goto(fixture.orders.h5.checkout, {
    waitUntil: "commit",
  });
  expect(responseH5.status()).toBe(200);
  expect(responseH5.headers()["referrer-policy"]).toBe(
    "strict-origin-when-cross-origin",
  );
  await outsideH5.waitForURL(fixture.orders.h5.material, { timeout: 12000 });
  await expect(
    outsideH5.getByRole("heading", {
      name: "Original synthetic WeChat H5 order240",
    }),
  ).toBeVisible();
  expect(
    report.navigationRequests.some(
      (row) =>
        row.scenario === "mobile-h5-auto" &&
        row.url === fixture.orders.h5.material,
    ),
    "actual browser performed automatic navigation to stored URL",
  ).toBe(true);
  await outsideH5.screenshot({
    path: path.join(artifacts, "mobile-h5-peer375.png"),
    fullPage: true,
  });
  await confirm(outsideH5, fixture.orders.h5);
  await Promise.all([
    merchant(desktopPages.h5, fixture.orders.h5),
    merchant(insideH5, fixture.orders.h5),
  ]);
  report.scenarios.push({
    name: "h5-mobile",
    width: 375,
    insideWechatExternalGuide: true,
    automaticStoredHTTPSNavigation: true,
    strictOriginRefererVerifiedByTLSPeer: true,
    manualFallbackVisitedSamePeer: true,
  });

  const outsideJS = await newPage("outside-jsapi", {
    mobile: true,
    bridge: true,
  });
  await checkout(outsideJS, fixture.orders.jsapi);
  await qrLayout(
    outsideJS,
    fixture.orders.jsapi,
    "continuation",
    "outside-jsapi375",
  );
  await outsideJS.evaluate(() => {
    window.WeixinJSBridge = {
      invoke() {
        window.__wechat240.calls.push("unexpected-outside-invoke");
      },
    };
    document.dispatchEvent(new Event("WeixinJSBridgeReady"));
  });
  await pending(outsideJS, fixture.orders.jsapi, "outside-jsapi");
  expect(await outsideJS.evaluate(() => window.__wechat240.calls)).toEqual([]);

  const jsName = "wechat-jsapi",
    jsapi = await newPage(jsName, { mobile: true, wechat: true, bridge: true });
  await checkout(jsapi, fixture.orders.jsapi);
  const pay = jsapi.getByRole("button", { name: /微信支付|重试|重新/ });
  await expect(pay).toBeVisible();
  expect(await jsapi.evaluate(() => window.__wechat240.calls)).toEqual([]);
  // Installing the native object alone does not signal browser readiness.
  await jsapi.evaluate(() => {
    window.WeixinJSBridge = {
      invoke(operation, params, callback) {
        window.__wechat240.calls.push({
          operation,
          params: JSON.parse(JSON.stringify(params)),
        });
        window.__wechat240.callbacks.push(callback);
      },
    };
  });
  expect(await jsapi.evaluate(() => window.__wechat240.calls)).toEqual([]);
  await jsapi.evaluate(() =>
    document.dispatchEvent(new Event("WeixinJSBridgeReady")),
  );
  await expect
    .poll(() => jsapi.evaluate(() => window.__wechat240.calls.length))
    .toBe(1);
  const expectedMaterial = JSON.parse(fixture.orders.jsapi.material);
  expect(await jsapi.evaluate(() => window.__wechat240.calls[0])).toEqual({
    operation: "getBrandWCPayRequest",
    params: expectedMaterial,
  });
  await jsapi.evaluate(() =>
    document.dispatchEvent(new Event("WeixinJSBridgeReady")),
  );
  expect(
    await jsapi.evaluate(() => window.__wechat240.calls.length),
    "repeated Ready must not duplicate native invocation",
  ).toBe(1);
  for (const [index, result] of ["cancel", "fail"].entries()) {
    await jsapi.evaluate(
      (value) =>
        window.__wechat240.callbacks.at(-1)({
          err_msg: "get_brand_wcpay_request:" + value,
        }),
      result,
    );
    await expect(pay).toBeEnabled();
    await expect(jsapi.locator("#wechat-feedback")).toContainText(
      result === "cancel" ? /取消|重试|重新/ : /失败|未确认|核对|重试|重新/,
    );
    await pending(jsapi, fixture.orders.jsapi, jsName);
    expect(jsapi.url()).toBe(fixture.orders.jsapi.checkout);
    expect(
      localReturnCount(jsName),
      "cancel/error must not trigger Go return/provider query",
    ).toBe(0);
    expect(await jsapi.evaluate(() => window.__wechat240.calls.length)).toBe(
      index + 1,
    );
    await pay.click();
    await expect
      .poll(() => jsapi.evaluate(() => window.__wechat240.calls.length))
      .toBe(index + 2);
    expect(await jsapi.evaluate(() => window.__wechat240.calls.at(-1))).toEqual(
      { operation: "getBrandWCPayRequest", params: expectedMaterial },
    );
  }
  await jsapi.evaluate(() =>
    window.__wechat240.callbacks.at(-1)({
      err_msg: "get_brand_wcpay_request:ok",
    }),
  );
  await pending(jsapi, fixture.orders.jsapi, jsName);
  expect(jsapi.url(), "native ok is not proof of locally paid order").toBe(
    fixture.orders.jsapi.checkout,
  );
  expect(localReturnCount(jsName)).toBe(0);
  await jsapi.screenshot({
    path: path.join(artifacts, "wechat-jsapi-ok-awaits-local375.png"),
    fullPage: true,
  });
  await confirm(jsapi, fixture.orders.jsapi);
  await Promise.all([
    merchant(jsapi, fixture.orders.jsapi),
    merchant(outsideJS, fixture.orders.jsapi),
    merchant(desktopPages.jsapi, fixture.orders.jsapi),
  ]);
  report.scenarios.push({
    name: jsName,
    width: 375,
    readyInvokesOnce: true,
    exactOriginalParams: true,
    cancelAndFailureExplicitRetry: true,
    okWaitsForRealSignedCallback: true,
  });

  const app = await newPage("mobile-app", { mobile: true });
  await checkout(app, fixture.orders.app);
  if (await app.locator('img[src*="/browser-qr.png"]').count())
    await qrLayout(app, fixture.orders.app, "continuation", "mobile-app375");
  const schemes = await app
    .locator('a[href^="weixin:"]')
    .evaluateAll((nodes) => nodes.map((node) => node.href));
  expect(
    schemes.length,
    "original APP launch is available as an explicit manual action",
  ).toBe(1);
  if (schemes.length) {
    const scheme = new URL(schemes[0]),
      original = JSON.parse(fixture.orders.app.material);
    expect(scheme.protocol).toBe("weixin:");
    expect(scheme.hostname).toBe("app");
    expect(scheme.pathname).toBe("/" + original.appid + "/pay/");
    for (const [key, value] of Object.entries({
      nonceStr: original.noncestr,
      package: original.package,
      partnerId: original.partnerid,
      prepayId: original.prepayid,
      timeStamp: original.timestamp,
      sign: original.sign,
    }))
      expect(scheme.searchParams.get(key)).toBe(value);
  }
  await pending(app, fixture.orders.app, "mobile-app");
  expect(app.url()).toBe(fixture.orders.app.checkout);
  await confirm(app, fixture.orders.app);
  await Promise.all([
    merchant(app, fixture.orders.app),
    merchant(desktopPages.app, fixture.orders.app),
  ]);
  report.scenarios.push({
    name: "mobile-app",
    width: 375,
    sameOriginalAPPOrderQR: true,
    noAutomaticSchemeLaunch: true,
    originalManualSchemeVerified: schemes.length === 1,
  });

  const closed = await newPage("closed-native", { mobile: true });
  await checkout(closed, fixture.orders.closed);
  await expect(closed.locator("main")).toContainText(/关闭|结束/);
  await expect(
    closed.locator(
      'img[src*="/qr.png"],img[src*="/browser-qr.png"],button,a[href^="weixin:"]',
    ),
  ).toHaveCount(0);
  for (const suffix of ["", "/browser-qr.png", "/qr.png"]) {
    const tampered = new URL(fixture.orders.h5.checkout);
    tampered.pathname += suffix;
    tampered.searchParams.set("token", "tampered240");
    const page = await newPage("tampered-" + (suffix || "checkout"));
    const response = await page.goto(tampered.href, {
      waitUntil: "domcontentloaded",
    });
    expect(response.status()).toBe(403);
    await expect(page.locator("img,button,a")).toHaveCount(0);
  }
  report.scenarios.push({
    name: "closed-and-tampered",
    rejectsCheckoutAndImages: true,
    noPaymentControls: true,
  });
  expect(report.errors, "no browser or boundary errors").toEqual([]);
  expect(
    report.forbidden,
    "all network requests remain at explicitly allowed local peers",
  ).toEqual([]);
  expect(report.statusReads.length).toBeGreaterThan(10);
  expect(report.merchantReturns.length).toBeGreaterThanOrEqual(9);
  report.passed = true;
  console.log(
    JSON.stringify({
      passed: true,
      scenarios: report.scenarios.map((row) => row.name),
      actualExternalNetworkRequests: 0,
      orders: 5,
      originalAttempts: 5,
      signedCallbacks: 4,
      providerQueries: 0,
    }),
  );
} catch (error) {
  report.passed = false;
  report.failure = error.stack ?? error.message;
  throw error;
} finally {
  await fs.writeFile(
    path.join(artifacts, "browser-results.json"),
    JSON.stringify(report, null, 2) + "\n",
  );
  for (const context of contexts) await context.close();
  await browser.close();
}
