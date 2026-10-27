import { createServer } from "vite";
import { chromium, expect } from "@playwright/test";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createNativeScanDeploymentCSP202 } from "./native-scan202-csp-browser.mjs";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const api = process.env.NATIVE202_BROWSER_API;
const token = process.env.NATIVE202_BROWSER_COOKIE;
const kind = process.env.NATIVE202_BROWSER_KIND;
const origin = process.env.NATIVE202_BROWSER_ORIGIN;
const artifacts = process.env.NATIVE202_BROWSER_ARTIFACTS;
const cspMode = process.env.NATIVE202_BROWSER_CSP;
if (
  !api?.startsWith("http://127.0.0.1:") ||
  !token ||
  !["wechat", "alipay"].includes(kind) ||
  origin !== "https://cashier.example.invalid" ||
  !artifacts
) {
  throw new Error("Explicit synthetic native202 browser fixture required");
}
if (cspMode && cspMode !== "deployed")
  throw new Error("Unsupported native202 browser CSP mode");
const deploymentCSP = cspMode
  ? await createNativeScanDeploymentCSP202({ web, origin, artifacts, kind })
  : null;

const sdkURL =
  kind === "wechat"
    ? "https://res.wx.qq.com/open/js/jweixin-1.6.3.js"
    : "https://gw.alipayobjects.com/as/g/h5-lib/alipayjsapi/3.1.1/alipayjsapi.min.js";
const userAgent =
  kind === "wechat"
    ? "Mozilla/5.0 SyntheticNative202 MicroMessenger/8.0"
    : "Mozilla/5.0 SyntheticNative202 AlipayClient/10.0";
const authCode =
  kind === "wechat" ? "134567890123456789" : "281234567890123456";

// This controls only the native bridge. Every console response still comes
// from the actual authenticated loopback Go handler (through Vite by default).
const controlledSDK = `(() => {
  const c = window.__nativeBridge202 = {
    calls: [], scans: [], ready: false, currentNonce: '', waiting: [], errors: []
  };
  if (${JSON.stringify(kind)} === 'wechat') {
    window.wx = {
      error(callback) { c.errors.push(callback); },
      config(config) {
        c.ready = false;
        c.calls.push({kind: 'config', nonce: config.nonceStr});
        c.currentNonce = config.nonceStr;
        c.ready = true;
        c.waiting.splice(0).forEach(callback => callback());
      },
      ready(callback) { if (c.ready) callback(); else c.waiting.push(callback); },
      scanQRCode(options) {
        c.calls.push({kind: 'scan', nonce: c.currentNonce});
        c.scans.push(options);
      }
    };
  } else {
    window.ap = {scan(options) {
      c.calls.push({kind: 'scan', type: options.type});
      c.scans.push(options);
    }};
  }
})();`;

const vite = deploymentCSP
  ? null
  : await createServer({
      root: web,
      configFile: path.join(web, "vite.config.ts"),
      logLevel: "silent",
      server: {
        host: "127.0.0.1",
        port: 0,
        strictPort: true,
        hmr: false,
        allowedHosts: ["cashier.example.invalid", "localhost", "127.0.0.1"],
        proxy: {
          "/console": { target: api, changeOrigin: false },
          "/static/img": { target: api, changeOrigin: false },
        },
      },
    });

let browser;
try {
  await fs.mkdir(artifacts, { recursive: true });
  await vite?.listen();
  const local = vite
    ? "http://127.0.0.1:" + vite.httpServer.address().port
    : null;
  browser = await chromium.launch({ headless: true });
  const results = [];

  for (const width of [375, 1440]) {
    const context = await browser.newContext({
      viewport: { width, height: 920 },
      userAgent,
      serviceWorkers: "block",
    });
    await deploymentCSP?.observe(context);
    let sdkLoads = 0;
    let external = 0;
    let websockets = 0;
    const errors = [];
    const posts = [];

    // A reserved virtual HTTPS origin keeps the genuine browser href aligned
    // with the server's configured signing origin. Responses use loopback Vite
    // or production assets plus the Go API; POST bodies/cookies are preserved.
    await context.route("**/*", async (route) => {
      const request = route.request();
      const target = new URL(request.url());
      if (request.url() === sdkURL) {
        sdkLoads++;
        await route.fulfill({
          status: 200,
          contentType: "application/javascript",
          body: controlledSDK,
        });
        return;
      }
      if (deploymentCSP && (await deploymentCSP.route(route, api))) return;
      if (target.origin === origin) {
        const upstream = await route.fetch({
          url: local + target.pathname + target.search,
          headers: await request.allHeaders(),
          maxRedirects: 0,
        });
        await route.fulfill({ response: upstream });
        return;
      }
      external++;
      await route.abort();
    });
    await context.addCookies([
      {
        name: "goepay_merchant",
        value: token,
        url: origin,
        httpOnly: true,
        sameSite: "Lax",
        secure: true,
      },
    ]);
    const page = await context.newPage();
    deploymentCSP?.track(page, () => ({
      width,
      sdkLoads,
      external,
      websockets,
    }));
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("websocket", () => websockets++);
    page.on("request", (request) => {
      const target = new URL(request.url());
      if (
        request.method() === "POST" &&
        target.pathname.startsWith("/console/")
      ) {
        posts.push(target.pathname);
      }
    });
    await page.goto(origin + "/merchant/scanpay");
    await expect(
      page.getByRole("heading", { name: "付款码收款", exact: true }),
    ).toBeVisible();
    await deploymentCSP?.checkScope(context, page);
    for (const key of ["1", ".", "2", "3"]) {
      await page
        .getByRole("button", { name: "金额键 " + key, exact: true })
        .click();
    }

    const configs = [];
    const start = async () => {
      const response =
        kind === "wechat"
          ? page.waitForResponse(
              (value) =>
                new URL(value.url()).pathname ===
                  "/console/v1/merchant/scanpay/jsconfig" &&
                value.request().method() === "POST",
            )
          : null;
      // If CSP rejects the script before the config request, keep this response
      // promise handled while the browser-CSP assertion reports the real cause.
      if (deploymentCSP && response) void response.catch(() => {});
      const cspCheckpoint = await deploymentCSP?.checkpoint(page);
      await page
        .getByRole("button", { name: "扫一扫收款", exact: true })
        .click();
      await deploymentCSP?.expectSDK(page, sdkURL, cspCheckpoint);
      if (response) {
        const reply = await response;
        expect(reply.status()).toBe(200);
        const body = await reply.json();
        expect(body.code).toBe(0);
        const config = body.data;
        expect(config.appId).toBe("wx-browser202");
        expect(config.debug).toBe(false);
        expect(config.jsApiList).toEqual(["scanQRCode"]);
        expect(config.timestamp).toBeGreaterThan(0);
        const signedPage = reply.request().postDataJSON().url;
        expect(signedPage).toBe(origin + "/merchant/scanpay");
        expect(
          (await reply.request().allHeaders())["x-csrf-token"]?.length > 0,
        ).toBe(true);
        const digest = createHash("sha1")
          .update(
            "jsapi_ticket=synthetic-browser202-ticket&noncestr=" +
              config.nonceStr +
              "&timestamp=" +
              config.timestamp +
              "&url=" +
              signedPage,
          )
          .digest("hex");
        expect(config.signature).toBe(digest);
        const serialized = JSON.stringify(body);
        expect(
          [
            "synthetic-browser202-ticket",
            "synthetic-browser202-token",
            "synthetic-browser202-secret",
            "access_token",
            "jsapi_ticket",
            "appsecret",
          ].some((value) => serialized.includes(value)),
        ).toBe(false);
        configs.push(config);
      }
    };

    await start();
    await expect
      .poll(() => page.evaluate(() => window.__nativeBridge202?.scans.length))
      .toBe(1);
    await page.getByRole("button", { name: "取消扫码", exact: true }).click();
    await start();
    await expect
      .poll(() => page.evaluate(() => window.__nativeBridge202?.scans.length))
      .toBe(2);
    const trace = await page.evaluate(() => window.__nativeBridge202.calls);
    if (kind === "wechat") {
      expect(trace).toEqual([
        { kind: "config", nonce: configs[0].nonceStr },
        { kind: "scan", nonce: configs[0].nonceStr },
        { kind: "config", nonce: configs[1].nonceStr },
        { kind: "scan", nonce: configs[1].nonceStr },
      ]);
      expect(configs[0].nonceStr === configs[1].nonceStr).toBe(false);
      expect(
        await page.evaluate(() =>
          window.__nativeBridge202.scans.map((value) => ({
            needResult: value.needResult,
            scanType: value.scanType,
          })),
        ),
      ).toEqual([
        { needResult: 1, scanType: ["qrCode", "barCode"] },
        { needResult: 1, scanType: ["qrCode", "barCode"] },
      ]);
    } else {
      expect(trace).toEqual([
        { kind: "scan", type: "bar" },
        { kind: "scan", type: "bar" },
      ]);
    }

    // A cancelled first round's late result is ignored. Duplicate successful
    // callbacks from the current round still create only one preparation.
    await page.evaluate(
      ({ kind, authCode }) => {
        const bridge = window.__nativeBridge202;
        const result =
          kind === "wechat" ? { resultStr: authCode } : { barCode: authCode };
        bridge.scans[0].success(result);
      },
      { kind, authCode },
    );
    const preparedResponse = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname ===
          "/console/v1/merchant/scanpay/prepare" &&
        response.request().method() === "POST",
    );
    await page.evaluate(
      ({ kind, authCode }) => {
        const bridge = window.__nativeBridge202;
        const result =
          kind === "wechat" ? { resultStr: authCode } : { barCode: authCode };
        bridge.scans[1].success(result);
        bridge.scans[1].success(result);
      },
      { kind, authCode },
    );
    const response = await preparedResponse;
    expect(response.status()).toBe(200);
    const prepared = (await response.json()).data;
    expect(prepared.state).toBe("prepared");
    expect(prepared.type).toBe(kind === "wechat" ? "wxpay" : "alipay");
    expect(prepared.can_apply).toBe(true);
    expect(prepared.can_query).toBe(false);
    await expect(
      page.getByRole("button", {
        name: "确认收款 ¥" + prepared.charge_money,
        exact: true,
      }),
    ).toBeVisible();
    // Deliberately stop at the explicit confirmation button.
    expect(posts.filter((value) => value.endsWith("/prepare")).length).toBe(1);
    expect(posts.some((value) => /\/(apply|query)$/.test(value))).toBe(false);
    expect(
      await page.evaluate(
        (code) =>
          location.href.includes(code) ||
          Object.values(sessionStorage).join().includes(code) ||
          Object.values(localStorage).join().includes(code) ||
          document.body.innerText.includes(code),
        authCode,
      ),
    ).toBe(false);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(sdkLoads).toBe(1);
    expect(external).toBe(0);
    expect(websockets).toBe(0);
    expect(errors.length).toBe(0);
    await deploymentCSP?.checkTransitions(context);
    expect(external).toBe(0);
    await page.screenshot({
      path: path.join(artifacts, "prepared-" + width + ".png"),
      fullPage: true,
    });
    results.push({
      kind,
      width,
      actual_http: true,
      persistent_session: true,
      official_host_private_ca: kind === "wechat",
      signature_verified: kind === "wechat",
      sdk_asset_fulfilled_locally: true,
      second_scan_uses_current_configuration: true,
      prepared: true,
      confirmed: false,
      duplicate_preparations: false,
      stored_bearer: false,
      external_requests: external,
      websocket_requests: websockets,
    });
    await deploymentCSP?.write();
    await context.close();
  }
  await fs.writeFile(
    path.join(artifacts, "result.json"),
    JSON.stringify({ production: false, real_funds: false, results }, null, 2) +
      "\n",
  );
} catch (error) {
  await deploymentCSP?.write(error);
  throw error;
} finally {
  await browser?.close();
  await vite?.close();
}
