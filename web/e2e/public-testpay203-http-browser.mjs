import { chromium, expect } from "@playwright/test";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createNativeScanDeploymentCSP202 } from "./native-scan202-csp-browser.mjs";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const api = process.env.PUBLIC203_BROWSER_API;
const controlKey = process.env.PUBLIC203_BROWSER_CONTROL;
const version = Number(process.env.PUBLIC203_BROWSER_VERSION);
const origin = process.env.PUBLIC203_BROWSER_ORIGIN;
const artifacts = process.env.PUBLIC203_BROWSER_ARTIFACTS;
if (
  !/^http:\/\/127\.0\.0\.1:\d+$/.test(api ?? "") ||
  !/^[a-f0-9]{64}$/.test(controlKey ?? "") ||
  ![0, 1].includes(version) ||
  origin !== "https://pay.example.invalid" ||
  !artifacts
)
  throw Error("Explicit synthetic public203 HTTP/browser fixture required");

const sdkURL =
  version === 1
    ? "https://static.geetest.com/v4/gt4.js"
    : "https://static.geetest.com/static/tools/gt.js";
const rejectedSDK = "https://unapproved-public203.example.invalid/sdk.js";
// This provides only the vendor SDK interface. All challenges, proof checking,
// browser cookies, CSRF, intents, orders and checkout responses come from Go.
const controlledSDK = `(() => {
  const bridge = window.__publicCaptchaBridge203 = { instances: [], shows: 0 };
  function initialize(options, done) {
    const c = { options, proof: null, success: null, errors: [], resets: 0, destroyed: false };
    const instance = {
      onReady(callback) { queueMicrotask(callback); return instance; },
      onSuccess(callback) { c.success = callback; return instance; },
      onError(callback) { c.errors.push(callback); return instance; },
      onClose(callback) { c.close = callback; return instance; },
      getValidate() { return c.proof; },
      showCaptcha() { bridge.shows++; },
      verify() { bridge.shows++; },
      reset() { c.resets++; },
      destroy() { c.destroyed = true; }
    };
    bridge.instances.push(c);
    done(instance);
  }
  bridge.complete = (proof, duplicate) => {
    const c = bridge.instances.at(-1);
    c.proof = proof;
    c.success();
    if (duplicate) c.success();
  };
  window[${JSON.stringify(version === 1 ? "initGeetest4" : "initGeetest")}] = initialize;
})();`;

// Reuse the stable deployment-policy reader and in-memory production builder.
// No development server or test-written replacement CSP is used in this bridge.
const productionCSP = await createNativeScanDeploymentCSP202({
  web,
  origin,
  artifacts,
  kind: "public203-v" + (version + 3),
});
async function control(action, body = {}) {
  const response = await fetch(api + "/__public203/" + action, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Synthetic-Public203-Control": controlKey,
    },
    body: JSON.stringify(body),
    redirect: "error",
    signal: AbortSignal.timeout(10000),
  });
  if (!response.ok)
    throw Error(
      "Synthetic public203 fixture control failed: " +
        action +
        " HTTP " +
        response.status,
    );
  return response.json();
}
async function json(page, pathname, body, credentials = "same-origin") {
  return page.evaluate(
    async ({ pathname, body, credentials }) => {
      const response = await fetch(pathname, {
        method: body === undefined ? "GET" : "POST",
        credentials,
        headers: {
          Accept: "application/json",
          ...(body === undefined ? {} : { "Content-Type": "application/json" }),
        },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      return { status: response.status, body: await response.json() };
    },
    { pathname, body, credentials },
  );
}
const publicPrefix = "/console/v1/public/testpay";
const isResponse = (suffix) => (response) =>
  new URL(response.url()).pathname === publicPrefix + suffix;
async function noStorage(page) {
  expect(
    await page.evaluate(() => ({
      local: localStorage.length,
      session: sessionStorage.length,
      writes: window.__publicStorage203,
    })),
  ).toEqual({ local: 0, session: 0, writes: [] });
}
let sequence = 0;
async function proofFor(page, width) {
  const options = await page.evaluate(
    () => window.__publicCaptchaBridge203.instances.at(-1).options,
  );
  expect(options.product).toBe("bind");
  if (version === 0) {
    expect(options.gt).toBe("a".repeat(32));
    expect(options.offline).toBe(false);
    return control("proof", { Challenge: options.challenge });
  }
  expect(options.captchaId).toBe("a".repeat(32));
  expect(options.protocol).toBe("https://");
  return {
    captcha_id: options.captchaId,
    lot_number: createHash("md5")
      .update("synthetic-public203:" + width + ":" + ++sequence)
      .digest("hex"),
    pass_token: "controlled-pass203",
    gen_time: String(Math.floor(Date.now() / 1000)),
    captcha_output: "controlled-output203",
  };
}

let browser;
let currentPage;
let sdkLoads = 0,
  external = 0,
  rejectedRequests = 0,
  websockets = 0;
const consolePaths = [],
  pageErrors = [],
  results = [],
  httpResponses = [];
const upstreamSnapshots = new WeakMap();
async function forwardGoResponse(route, target) {
  const request = route.request();
  const upstream = await route.fetch({
    url: api + target.pathname + target.search,
    headers: await request.allHeaders(),
    maxRedirects: 0,
  });
  // The application may consume this response and immediately navigate to its
  // genuine checkout. Capture the real upstream bytes before fulfilling, while
  // APIResponse owns them; Chromium can discard the old document's response.
  const bytes = await upstream.body();
  const headers = upstream.headers();
  let envelope;
  if (headers["content-type"]?.includes("application/json")) {
    try {
      envelope = JSON.parse(bytes.toString("utf8"));
    } catch {}
  }
  upstreamSnapshots.set(request, {
    status: upstream.status(),
    headers,
    envelope,
    requestBody: request.postData() ? request.postDataJSON() : undefined,
  });
  httpResponses.push({
    sequence: httpResponses.length + 1,
    method: request.method(),
    path: target.pathname,
    query_fields: [...target.searchParams.keys()],
    status: upstream.status(),
    bytes: bytes.length,
    body_sha256: createHash("sha256").update(bytes).digest("hex"),
    data_fields:
      envelope?.data && typeof envelope.data === "object"
        ? Object.keys(envelope.data)
        : [],
    captured_before_fulfill: true,
  });
  // Preserve the actual Go status, headers, cookies and bytes. The snapshot is
  // an assertion input only; it never replaces or manufactures an API reply.
  return route.fulfill({ response: upstream });
}
function capturedGoResponse(response) {
  const snapshot = upstreamSnapshots.get(response.request());
  if (!snapshot?.envelope)
    throw Error(
      "Missing real upstream JSON snapshot: " +
        new URL(response.url()).pathname +
        " HTTP " +
        response.status(),
    );
  expect(snapshot.status).toBe(response.status());
  expect(snapshot.headers["content-type"]).toBe(
    response.headers()["content-type"],
  );
  return snapshot;
}
async function contextFor(width) {
  const context = await browser.newContext({
    viewport: { width, height: 1000 },
    serviceWorkers: "block",
  });
  await productionCSP.observe(context);
  await context.addInitScript(() => {
    window.__publicStorage203 = [];
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      window.__publicStorage203.push({
        store: this === localStorage ? "local" : "session",
        key,
      });
      return original.call(this, key, value);
    };
  });
  await context.route("**/*", async (route) => {
    const request = route.request(),
      target = new URL(request.url());
    if (request.url() === sdkURL) {
      sdkLoads++;
      return route.fulfill({
        status: 200,
        contentType: "application/javascript",
        body: controlledSDK,
      });
    }
    if (request.url() === rejectedSDK) {
      rejectedRequests++;
      return route.fulfill({
        status: 200,
        contentType: "application/javascript",
        body: "window.__unapprovedPublic203 = true;",
      });
    }
    if (
      target.origin === origin &&
      (target.pathname.startsWith("/console/") ||
        target.pathname.startsWith("/static/img/") ||
        target.pathname === "/" ||
        target.pathname.startsWith("/checkout/"))
    ) {
      return forwardGoResponse(route, target);
    }
    if (await productionCSP.route(route, api)) return;
    external++;
    return route.abort();
  });
  context.on("page", (page) => {
    page.on("pageerror", (error) => pageErrors.push(error.message));
    page.on("websocket", () => websockets++);
    page.on("request", (request) => {
      if (new URL(request.url()).pathname.startsWith("/console/"))
        consolePaths.push(new URL(request.url()).pathname);
    });
  });
  return context;
}

try {
  await fs.mkdir(artifacts, { recursive: true });
  browser = await chromium.launch({ headless: true });
  for (const width of [375, 1440]) {
    const context = await contextFor(width);
    const page = (currentPage = await context.newPage());
    productionCSP.track(page, () => ({
      width,
      sdkLoads,
      external,
      websockets,
    }));
    const before = await control("metrics");
    const initialized = page.waitForResponse(isResponse("/init"));
    const forgedRead = page.waitForResponse(isResponse("/success"));
    await page.goto(origin + "/test?ok=1&trade_no=CALLER203&money=999.99");
    expect((await forgedRead).status()).toBe(403);
    const initialResponse = await initialized;
    expect(initialResponse.status()).toBe(200);
    expect(initialResponse.headers()["cache-control"]).toBe("no-store");
    const initial = capturedGoResponse(initialResponse).envelope.data;
    expect(initial.captcha_open).toBe(1);
    expect(initial.money).toBe("1.00");
    expect(initial.sitename).toBe("公开测试203");
    expect(initial.csrf_token).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(initial.trade_no).toMatch(/^TP[A-Za-z0-9_]+$/);
    const cookies = await context.cookies(origin + "/console/v1/public");
    expect(
      cookies.map((cookie) => ({
        name: cookie.name,
        path: cookie.path,
        httpOnly: cookie.httpOnly,
        secure: cookie.secure,
        sameSite: cookie.sameSite,
      })),
    ).toEqual([
      {
        name: "goepay_testpay203",
        path: "/console/v1/public",
        httpOnly: true,
        secure: true,
        sameSite: "Strict",
      },
    ]);
    const panel = page.getByRole("main", { name: "测试支付", exact: true });
    await expect(panel.getByLabel("商户订单号", { exact: true })).toHaveValue(
      initial.trade_no,
    );
    await expect(
      panel.getByLabel("商户订单号", { exact: true }),
    ).toHaveAttribute("readonly", "");
    await expect(panel.getByLabel("商品名称", { exact: true })).toHaveValue(
      "支付测试",
    );
    await expect(panel.locator('[name="money"]')).toHaveValue("1.00");
    await expect(panel).not.toContainText("订单已支付成功");
    const pay = panel.getByRole("button", { name: "支付宝203", exact: true });
    await expect(
      pay,
      "The fixed controlled CAPTCHA SDK must load under the deployed /test CSP",
    ).toBeEnabled();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const denied = await page.evaluate(
      (url) =>
        new Promise((resolve) => {
          const script = document.createElement("script");
          script.src = url;
          script.onload = () => {
            script.remove();
            resolve(false);
          };
          script.onerror = () => {
            script.remove();
            resolve(true);
          };
          document.head.appendChild(script);
        }),
      rejectedSDK,
    );
    expect(denied).toBe(true);
    expect(rejectedRequests).toBe(0);
    await expect
      .poll(() =>
        page.evaluate(
          (url) =>
            window.__nativeCSP202.violations.some(
              (value) =>
                value.disposition === "enforce" &&
                value.effectiveDirective.startsWith("script-src") &&
                (value.blockedURI === url ||
                  value.blockedURI === new URL(url).origin),
            ),
          rejectedSDK,
        ),
      )
      .toBe(true);
    await noStorage(page);
    const missingCSRF = await json(page, publicPrefix + "/submit", {
      money: "1.23",
      typeid: "700203",
      csrf_token: "",
    });
    expect(missingCSRF.status).toBe(403);
    expect((await control("metrics")).financial).toEqual(before.financial);
    await panel.locator('[name="money"]').fill("1.23");
    await panel.locator('[name="money"]').press("Tab");
    await expect(pay).toBeFocused();
    await panel.screenshot({
      path: path.join(artifacts, "initial-" + width + ".png"),
    });
    const proof = await proofFor(page, width);
    const rejected = {
      ...proof,
      ...(version === 0
        ? { geetest_seccode: "rejected-seccode203" }
        : { captcha_output: "rejected-output203" }),
    };
    await pay.press("Enter");
    await expect
      .poll(() => page.evaluate(() => window.__publicCaptchaBridge203.shows))
      .toBe(1);
    const rejectedResponse = page.waitForResponse(isResponse("/submit"));
    await page.evaluate(
      (proof) => window.__publicCaptchaBridge203.complete(proof, true),
      rejected,
    );
    expect((await rejectedResponse).status()).toBe(422);
    await expect(panel.getByRole("alert")).toContainText("验证码无效");
    const afterReject = await control("metrics");
    expect(afterReject.financial).toEqual(before.financial);
    expect(afterReject.sends).toBe(before.sends);
    expect(afterReject.verifies).toBe(before.verifies + 1);
    expect(afterReject.rejected).toBe(before.rejected + 1);
    await noStorage(page);
    await pay.click();
    await expect
      .poll(() => page.evaluate(() => window.__publicCaptchaBridge203.shows))
      .toBe(2);
    const submitted = page.waitForResponse(isResponse("/submit"));
    await page.evaluate(
      (proof) => window.__publicCaptchaBridge203.complete(proof, true),
      proof,
    );
    const submitResponse = await submitted;
    expect(submitResponse.status()).toBe(200);
    const submitSnapshot = capturedGoResponse(submitResponse);
    const body = submitSnapshot.requestBody;
    expect(body.money).toBe("1.23");
    expect(body.typeid).toBe("700203");
    expect(body.csrf_token).toBe(initial.csrf_token);
    expect(body.captcha_token).toMatch(/^[A-Za-z0-9_-]{43}$/);
    const checkout = new URL(submitSnapshot.envelope.data.url, origin);
    expect(checkout.origin).toBe(origin);
    expect(checkout.pathname).toMatch(/^\/checkout\/[A-Za-z0-9_-]+$/);
    expect(checkout.searchParams.get("token")).toMatch(/^[A-Za-z0-9_-]{43}$/);
    await page.waitForURL((url) => url.pathname === checkout.pathname);
    await expect(page.locator("body")).toContainText("1.23");
    await noStorage(page);
    const pendingRead = page.waitForResponse(isResponse("/success"));
    const blockedInit = page.waitForResponse(isResponse("/init"));
    await page.goto(
      origin + "/test?ok=1&trade_no=" + encodeURIComponent(initial.trade_no),
    );
    expect((await pendingRead).status()).toBe(409);
    expect((await blockedInit).status()).toBe(409);
    await expect(
      panel.getByRole("button", { name: "核对原测试订单", exact: true }),
    ).toBeVisible();
    await expect(panel).not.toContainText("订单已支付成功");
    const repeatedInit = await json(page, publicPrefix + "/init");
    expect(repeatedInit.status).toBe(409);
    expect(repeatedInit.body.data.trade_no).toBe(initial.trade_no);
    const replay = await json(page, publicPrefix + "/submit", body);
    expect(replay.status).toBe(200);
    expect(new URL(replay.body.data.url, origin).pathname).toBe(
      checkout.pathname,
    );
    const afterReplay = await control("metrics");
    expect(afterReplay.sends).toBe(before.sends + 1);
    expect(afterReplay.verifies).toBe(before.verifies + 2);
    expect(afterReplay.financial.orders).toBe(before.financial.orders + 1);
    expect(afterReplay.financial.attempts).toBe(before.financial.attempts + 1);
    expect(afterReplay.financial.paid).toBe(before.financial.paid);
    await panel.screenshot({
      path: path.join(artifacts, "pending-" + width + ".png"),
    });
    expect(await control("paid", { Trade: initial.trade_no })).toEqual({
      paid: true,
    });
    const paidRead = page.waitForResponse(isResponse("/success"));
    const initsBeforePaid = consolePaths.filter(
      (value) => value === publicPrefix + "/init",
    ).length;
    await page.goto(
      origin +
        "/test?ok=caller-controlled&trade_no=" +
        encodeURIComponent(initial.trade_no) +
        "&money=999.99",
    );
    const paidResponse = await paidRead;
    expect(paidResponse.status()).toBe(200);
    expect(capturedGoResponse(paidResponse).envelope.data).toEqual({
      money: "1.23",
      sitename: "公开测试203",
    });
    await expect(
      panel.getByRole("heading", { name: "订单已支付成功！", exact: true }),
    ).toBeVisible();
    await expect(panel).toContainText("支付金额：¥ 1.23");
    await expect(panel).not.toContainText("999.99");
    expect(
      consolePaths.filter((value) => value === publicPrefix + "/init").length,
    ).toBe(initsBeforePaid);
    await noStorage(page);
    await panel.screenshot({
      path: path.join(artifacts, "paid-" + width + ".png"),
    });

    const cancelContext = await contextFor(width);
    const cancelled = (currentPage = await cancelContext.newPage());
    productionCSP.track(cancelled, () => ({
      width,
      scope: "leave-cancels",
      sdkLoads,
      external,
      websockets,
    }));
    await cancelled.goto(origin + "/test");
    const cancelPanel = cancelled.getByRole("main", {
      name: "测试支付",
      exact: true,
    });
    const cancelPay = cancelPanel.getByRole("button", {
      name: "支付宝203",
      exact: true,
    });
    await expect(cancelPay).toBeEnabled();
    expect(
      (
        await json(
          cancelled,
          publicPrefix +
            "/success?trade_no=" +
            encodeURIComponent(initial.trade_no),
        )
      ).status,
    ).toBe(403);
    await expect(cancelPanel).not.toContainText("订单已支付成功");
    const lateProof = await proofFor(cancelled, width);
    await cancelPay.click();
    await expect
      .poll(() =>
        cancelled.evaluate(() => window.__publicCaptchaBridge203.shows),
      )
      .toBe(1);
    const beforeCancel = await control("metrics");
    const homeNavigations = [];
    cancelled.on("request", (request) => {
      if (
        request.isNavigationRequest() &&
        new URL(request.url()).pathname === "/"
      )
        homeNavigations.push(request.url());
    });
    await cancelled.evaluate((proof) => {
      window.__publicCaptchaBridge203.instances.at(-1).proof = proof;
    }, lateProof);
    const lateCallback = await cancelled.evaluateHandle(
      () => window.__publicCaptchaBridge203.instances.at(-1).success,
    );
    await cancelPanel
      .getByRole("link", { name: "返回首页", exact: true })
      .click();
    await cancelled.waitForURL((url) => url.pathname === "/");
    await expect
      .poll(() => homeNavigations.length, {
        message: "Leaving /test must restore the homepage document policy",
      })
      .toBe(1);
    // Attempt the captured old SDK callback after the new document commits.
    // Its destroyed execution context is the document-boundary cancellation;
    // this avoids racing an arbitrary timer against real HTTP/PG navigation.
    let oldContextDestroyed = false;
    try {
      await lateCallback.evaluate((callback) => {
        callback();
        callback();
      });
    } catch (error) {
      expect(String(error.message ?? error)).toMatch(
        /Execution context was destroyed|Cannot find context|context.*destroyed|JSHandle.*context|Cannot find object/i,
      );
      oldContextDestroyed = true;
    }
    expect(oldContextDestroyed).toBe(true);
    await lateCallback.dispose();
    const afterCancel = await control("metrics");
    expect(afterCancel.financial).toEqual(beforeCancel.financial);
    expect(afterCancel.sends).toBe(beforeCancel.sends);
    expect(afterCancel.verifies).toBe(beforeCancel.verifies);
    await noStorage(cancelled);
    expect(
      consolePaths.some((value) =>
        /^\/console\/v1\/(admin|merchant)\//.test(value),
      ),
    ).toBe(false);
    expect(afterCancel.console_reads).toBe(0);
    expect(afterCancel.console_cookies).toBe(0);
    expect(afterCancel.bad).toBe(0);
    expect(afterCancel.queries).toBe(0);
    expect(external).toBe(0);
    expect(websockets).toBe(0);
    expect(pageErrors).toEqual([]);
    results.push({
      version: version + 3,
      width,
      actual_http: true,
      actual_pg: true,
      production_csp: true,
      official_host_private_ca: true,
      vendor_sdk_fulfilled_locally: true,
      actual_vendor_sdk_executed: false,
      console_session_reads: 0,
      browser_cookie_issued_over_http: true,
      missing_csrf_rejected: true,
      captcha_remote_failure_has_no_payment_effect: true,
      duplicate_callback_has_one_first_send: true,
      pending_read_and_reinit_409: true,
      replay_does_not_send_again: true,
      paid_amount_read_from_original_order: true,
      another_browser_cannot_read_paid_order: true,
      leave_cancels_late_callback: true,
      storage_writes: 0,
      external_requests: external,
      financial: afterCancel.financial,
      controlled_provider_sends: afterCancel.sends,
    });
    await productionCSP.write();
    await cancelContext.close();
    await context.close();
  }
  await fs.writeFile(
    path.join(artifacts, "result.json"),
    JSON.stringify(
      {
        production: false,
        real_funds: false,
        real_vendor_sdk: false,
        response_capture: "real-loopback-upstream-before-fulfill",
        api_responses: httpResponses,
        results,
      },
      null,
      2,
    ) + "\n",
  );
} catch (error) {
  await productionCSP.write(error);
  if (currentPage && !currentPage.isClosed())
    await currentPage
      .screenshot({
        path: path.join(artifacts, "public203-failure.png"),
        fullPage: true,
      })
      .catch(() => {});
  await fs.writeFile(
    path.join(artifacts, "failure.json"),
    JSON.stringify(
      {
        production: false,
        real_funds: false,
        version: version + 3,
        error: String(error.message ?? error),
        sdk_loads: sdkLoads,
        external_requests: external,
        websocket_requests: websockets,
        console_paths: consolePaths,
        page_errors: pageErrors,
        api_responses: httpResponses,
        completed: results,
      },
      null,
      2,
    ) + "\n",
  );
  throw error;
} finally {
  await browser?.close();
}
