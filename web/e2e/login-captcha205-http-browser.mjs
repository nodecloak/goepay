import { chromium, expect } from "@playwright/test";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createNativeScanDeploymentCSP202 } from "./native-scan202-csp-browser.mjs";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const api = process.env.LOGIN205_BROWSER_API;
const controlKey = process.env.LOGIN205_BROWSER_CONTROL;
const version = Number(process.env.LOGIN205_BROWSER_VERSION);
const width = Number(process.env.LOGIN205_BROWSER_WIDTH);
const origin = process.env.LOGIN205_BROWSER_ORIGIN;
const artifacts = process.env.LOGIN205_BROWSER_ARTIFACTS;
if (
  !/^http:\/\/127\.0\.0\.1:\d+$/.test(api ?? "") ||
  !/^[a-f0-9]{64}$/.test(controlKey ?? "") ||
  ![0, 1].includes(version) ||
  ![375, 1440].includes(width) ||
  origin !== "https://console.example.invalid" ||
  !artifacts
)
  throw Error("Explicit synthetic login205 HTTP/browser fixture required");
const username = "captcha205",
  password = "synthetic-login205-password";
const loginPath = "/console/v1/merchant/login";
const initPath = "/console/v1/public/login-captcha/init";
const metadataPath = "/console/v1/public/login-captcha";
const sdkURL =
  version === 1
    ? "https://static.geetest.com/v4/gt4.js"
    : "https://static.geetest.com/static/tools/gt.js";
const rejectedSDK = "https://unapproved-login205.example.invalid/sdk.js";
const controlledSDK = `(() => {
  const bridge = window.__loginBridge205 = { instances: [] };
  function initialize(options, done) {
    const c = { options, proof: null, shown: false, destroyed: false };
    const widget = {
      onReady(callback) { queueMicrotask(callback); return widget; },
      onSuccess(callback) { c.success = callback; return widget; },
      onError(callback) { c.error = callback; return widget; },
      onClose(callback) { c.close = callback; return widget; },
      getValidate() { return c.proof; },
      showCaptcha() { c.shown = true; },
      verify() { c.shown = true; },
      destroy() { c.destroyed = true; }
    };
    bridge.instances.push(c);
    done(widget);
  }
  bridge.complete = (index, proof) => {
    const c = bridge.instances[index]; c.proof = proof;
    c.success(); c.success();
  };
  bridge.close = index => bridge.instances[index].close();
  window[${JSON.stringify(version === 1 ? "initGeetest4" : "initGeetest")}] = initialize;
})();`;

const productionCSP = await createNativeScanDeploymentCSP202({
  web,
  origin,
  artifacts,
  kind: "login205-v" + (version + 3),
});
const snapshots = new WeakMap(),
  requests = [],
  diagnostics = [],
  documentNavigations = [],
  results = [];
let browser,
  currentPage,
  stage = "setup",
  external = 0,
  websockets = 0,
  rejectedRequests = 0,
  proofSequence = 0;
const pageErrors = [];
async function control(action, body = {}) {
  const response = await fetch(api + "/__login205/" + action, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Synthetic-Login205-Control": controlKey,
    },
    body: JSON.stringify(body),
    redirect: "error",
    signal: AbortSignal.timeout(10000),
  });
  if (!response.ok)
    throw Error(
      "Synthetic login205 control failed: " +
        action +
        " HTTP " +
        response.status,
    );
  return response.json();
}
async function forwardGo(route, target, scope) {
  const request = route.request();
  requests.push({ scope, path: target.pathname, method: request.method() });
  const requestHeaders = await request.allHeaders();
  const upstream = await route.fetch({
    url: api + target.pathname + target.search,
    headers: requestHeaders,
    maxRedirects: 0,
  });
  const bytes = await upstream.body(),
    headers = upstream.headers();
  let envelope;
  if (headers["content-type"]?.includes("application/json")) {
    try {
      envelope = JSON.parse(bytes.toString("utf8"));
    } catch {}
  }
  const record = {
    scope,
    method: request.method(),
    path: target.pathname,
    status: upstream.status(),
    requestHeaders,
    requestBody: request.postData() ? request.postDataJSON() : undefined,
    envelope,
  };
  snapshots.set(request, record);
  diagnostics.push({
    scope,
    method: record.method,
    path: record.path,
    query_fields: [...target.searchParams.keys()],
    status: record.status,
    request_fields: record.requestBody ? Object.keys(record.requestBody) : [],
    data_fields:
      envelope?.data && typeof envelope.data === "object"
        ? Object.keys(envelope.data)
        : [],
    bytes: bytes.length,
    body_sha256: createHash("sha256").update(bytes).digest("hex"),
    captured_before_fulfill: true,
  });
  return route.fulfill({ response: upstream });
}
function captured(response) {
  const snapshot = snapshots.get(response.request());
  if (!snapshot?.envelope)
    throw Error(
      "Missing genuine Go JSON snapshot: " + new URL(response.url()).pathname,
    );
  expect(snapshot.status).toBe(response.status());
  return snapshot;
}
const isResponse = (pathname, method) => (response) =>
  new URL(response.url()).pathname === pathname &&
  (!method || response.request().method() === method);
const count = (scope, pathname, method = "POST") =>
  requests.filter(
    (record) =>
      record.scope === scope &&
      record.path === pathname &&
      record.method === method,
  ).length;
async function json(page, pathname, body) {
  return page.evaluate(
    async ({ pathname, body }) => {
      const response = await fetch(pathname, {
        method: body === undefined ? "GET" : "POST",
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          ...(body === undefined ? {} : { "Content-Type": "application/json" }),
        },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      return { status: response.status, body: await response.json() };
    },
    { pathname, body },
  );
}
async function cleanStorage(page) {
  expect(
    await page.evaluate(() => ({
      local: localStorage.length,
      session: sessionStorage.length,
      writes: window.__loginStorage205,
    })),
  ).toEqual({ local: 0, session: 0, writes: [] });
  expect(
    await page.evaluate(
      ({ password }) =>
        location.href.includes(password) ||
        /captcha_token|geetest_validate|pass_token|api_key/.test(location.href),
      { password },
    ),
  ).toBe(false);
}
async function blockedScript(page, source) {
  expect(
    await page.evaluate(
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
      source,
    ),
  ).toBe(true);
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
        source,
      ),
    )
    .toBe(true);
}
function documentCount(scope, pathname) {
  return documentNavigations.filter(
    (entry) => entry.scope === scope && entry.path === pathname,
  ).length;
}
async function contextFor(scope) {
  const context = await browser.newContext({
    viewport: { width, height: 1000 },
    serviceWorkers: "block",
  });
  const counters = { scope, width, sdkLoads: 0 };
  await productionCSP.observe(context);
  await context.addInitScript(() => {
    window.__loginStorage205 = [];
    const setItem = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      window.__loginStorage205.push({
        store: this === localStorage ? "local" : "session",
        key,
      });
      return setItem.call(this, key, value);
    };
    // One explicit observation fault after an actual successful HTTP response
    // and its browser-applied cookie. It never supplies a substitute response.
    const originalFetch = window.fetch.bind(window);
    const fault = (window.__loginFault205 = { armed: false, fired: 0 });
    window.fetch = async (input, init) => {
      const response = await originalFetch(input, init);
      const url = new URL(
        typeof input === "string" ? input : input.url,
        location.href,
      );
      if (
        fault.armed &&
        url.pathname === "/console/v1/merchant/login" &&
        (init?.method ?? "GET") === "POST" &&
        response.ok
      ) {
        fault.armed = false;
        await response.arrayBuffer();
        fault.fired++;
        throw new TypeError(
          "Synthetic login205 observation lost after real 200/cookie",
        );
      }
      return response;
    };
  });
  await context.route("**/*", async (route) => {
    const request = route.request(),
      target = new URL(request.url());
    if (request.url() === sdkURL) {
      counters.sdkLoads++;
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
        body: "window.__unapprovedLogin205 = true;",
      });
    }
    if (
      target.origin === origin &&
      (target.pathname.startsWith("/console/") ||
        target.pathname.startsWith("/static/img/") ||
        target.pathname === "/")
    )
      return forwardGo(route, target, scope);
    if (await productionCSP.route(route, api)) return;
    external++;
    return route.abort();
  });
  const page = (currentPage = await context.newPage());
  page.on("response", (response) => {
    if (response.request().isNavigationRequest()) {
      const target = new URL(response.url());
      documentNavigations.push({
        scope,
        path: target.pathname,
        query_fields: [...target.searchParams.keys()],
        status: response.status(),
        csp: response.headers()["content-security-policy"] ?? "",
      });
    }
  });
  page.on("pageerror", (error) => pageErrors.push(error.message));
  page.on("websocket", () => websockets++);
  productionCSP.track(page, () => ({ ...counters, external, websockets }));
  return { context, page, counters, scope };
}
async function fill(page, name = username, secret = password) {
  await page.locator('[name="username"]').fill(name);
  await page.locator('[name="password"]').fill(secret);
}
function loginButton(page) {
  return page.getByRole("button", { name: "登录工作台", exact: true });
}
async function gateReady(page) {
  await expect(
    page.getByRole("group", { name: "登录验证码", exact: true }),
  ).toContainText("登录时需要完成人机验证");
}
async function begin(page, secret = password) {
  await fill(page, username, secret);
  await gateReady(page);
  const previous = await page.evaluate(
    () => window.__loginBridge205?.instances.length ?? 0,
  );
  const initialized = page.waitForResponse(isResponse(initPath, "POST"));
  await loginButton(page).click();
  const response = await initialized;
  expect(response.status()).toBe(200);
  const snapshot = captured(response);
  expect(snapshot.requestBody).toEqual({ username });
  expect(response.headers()["cache-control"]).toBe("no-store");
  expect(snapshot.envelope.data.captcha_token).toMatch(/^[A-Za-z0-9_-]{43}$/);
  await expect
    .poll(
      () => page.evaluate(() => window.__loginBridge205?.instances.length ?? 0),
      {
        message:
          "Fixed CAPTCHA SDK must load under deployed merchant-login CSP",
      },
    )
    .toBe(previous + 1);
  await expect
    .poll(() =>
      page.evaluate(
        (index) => window.__loginBridge205.instances[index].shown,
        previous,
      ),
    )
    .toBe(true);
  const options = await page.evaluate(
    (index) => window.__loginBridge205.instances[index].options,
    previous,
  );
  expect(options.product).toBe("bind");
  let proof;
  if (version === 0) {
    expect(options.gt).toBe("a".repeat(32));
    expect(options.challenge).toBe(snapshot.envelope.data.challenge);
    expect(options.offline).toBe(false);
    proof = await control("proof", { Challenge: options.challenge });
  } else {
    expect(options.captchaId).toBe("a".repeat(32));
    expect(options.protocol).toBe("https://");
    proof = {
      captcha_id: options.captchaId,
      lot_number: createHash("md5")
        .update("login205:" + width + ":" + ++proofSequence)
        .digest("hex"),
      pass_token: "controlled-pass203",
      gen_time: String(Math.floor(Date.now() / 1000)),
      captcha_output: "controlled-output203",
    };
  }
  return { index: previous, proof, challenge: snapshot.envelope.data };
}
async function complete(page, attempt, proof = attempt.proof) {
  await page.evaluate(
    ({ index, proof }) => window.__loginBridge205.complete(index, proof),
    { index: attempt.index, proof },
  );
}
async function assertNoEffects(before) {
  const after = await control("metrics");
  expect(after.effects).toEqual(before.effects);
  expect(Object.values(after.effects)).toEqual(
    Object.values(after.effects).map(() => 0),
  );
  expect(after.sends).toBe(0);
  expect(after.queries).toBe(0);
  expect(after.bad).toBe(0);
  expect(after.unrelated_writes).toBe(0);
  expect(after.public_payment_requests).toBe(0);
  return after;
}

try {
  await fs.mkdir(artifacts, { recursive: true });
  browser = await chromium.launch({ headless: true });
  const baseline = await control("metrics");
  stage = "configured-origin-boundary";
  for (const supplied of [
    undefined,
    "https://untrusted-login205.example.invalid",
  ]) {
    const response = await fetch(api + initPath, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Host: new URL(origin).host,
        ...(supplied ? { Origin: supplied } : {}),
      },
      body: JSON.stringify({ username }),
      redirect: "error",
      signal: AbortSignal.timeout(10000),
    });
    expect(
      response.status,
      "A matching Host must never authorize a missing or foreign Origin",
    ).toBe(403);
  }
  expect((await control("metrics")).registers).toBe(baseline.registers);
  const merchant = await contextFor("merchant"),
    { page } = merchant;
  stage = "merchant-login-metadata";
  const metadata = page.waitForResponse(isResponse(metadataPath, "GET"));
  await page.goto(origin + "/merchant/dashboard");
  await page.waitForURL((url) => url.pathname === "/merchant/login");
  expect(new URL(page.url()).searchParams.get("return")).toBe(
    "/merchant/dashboard",
  );
  await expect(
    page.getByRole("heading", { name: "商户登录", exact: true }),
  ).toBeVisible();
  expect(captured(await metadata).envelope.data).toEqual({ enabled: true });
  await gateReady(page);
  expect(documentCount("merchant", "/merchant/dashboard")).toBe(1);
  expect(documentCount("merchant", "/merchant/login")).toBe(1);
  expect(count("merchant", initPath)).toBe(0);
  expect(count("merchant", loginPath)).toBe(0);
  await cleanStorage(page);
  await blockedScript(page, rejectedSDK);
  expect(rejectedRequests).toBe(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: path.join(artifacts, "merchant-login.png"),
    fullPage: true,
  });

  stage = "missing-proof";
  expect((await json(page, loginPath, { username, password })).status).toBe(
    422,
  );
  expect((await assertNoEffects(baseline)).merchant_sessions).toBe(
    baseline.merchant_sessions,
  );
  stage = "wrong-proof";
  const wrong = await begin(page);
  const cookies = await merchant.context.cookies(origin + "/");
  expect(
    cookies.map((cookie) => ({
      name: cookie.name,
      httpOnly: cookie.httpOnly,
      secure: cookie.secure,
      sameSite: cookie.sameSite,
      path: cookie.path,
    })),
  ).toEqual([
    {
      name: "goepay_login205",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
      path: "/",
    },
  ]);
  const invalidProof = {
    ...wrong.proof,
    ...(version === 0
      ? { geetest_seccode: "rejected-seccode203" }
      : { captcha_output: "rejected-output203" }),
  };
  const invalidResponse = page.waitForResponse(isResponse(loginPath, "POST"));
  const postsBeforeInvalid = count("merchant", loginPath);
  await complete(page, wrong, invalidProof);
  expect((await invalidResponse).status()).toBe(422);
  expect(count("merchant", loginPath)).toBe(postsBeforeInvalid + 1);
  await expect(
    page.getByRole("alert").filter({ hasText: "登录验证码无效" }),
  ).toBeVisible();
  let state = await assertNoEffects(baseline);
  expect(state.merchant_sessions).toBe(baseline.merchant_sessions);
  expect(state.claims).toBe(0);
  expect(state.verifies).toBe(1);

  stage = "wrong-password-burns-proof";
  const badPassword = await begin(page, "controlled-wrong-password205");
  const deniedPassword = page.waitForResponse(isResponse(loginPath, "POST"));
  const postsBeforePassword = count("merchant", loginPath);
  await complete(page, badPassword);
  const passwordReply = await deniedPassword;
  expect(passwordReply.status()).toBe(401);
  expect(count("merchant", loginPath)).toBe(postsBeforePassword + 1);
  const burnedBody = captured(passwordReply).requestBody;
  expect(burnedBody.password).toBe("controlled-wrong-password205");
  expect(burnedBody.captcha_token).toBe(badPassword.challenge.captcha_token);
  expect(
    (await json(page, loginPath, { ...burnedBody, password })).status,
  ).toBe(422);
  state = await assertNoEffects(baseline);
  expect(state.merchant_sessions).toBe(baseline.merchant_sessions);
  expect(state.claims).toBe(1);
  expect(state.consumed).toBe(1);
  expect(state.issued).toBe(0);
  expect(state.verifies).toBe(2);

  stage = "cancelled-widget";
  const cancelled = await begin(page);
  const postsBeforeCancel = count("merchant", loginPath);
  await page.evaluate(
    (index) => window.__loginBridge205.close(index),
    cancelled.index,
  );
  await expect(loginButton(page)).toBeEnabled();
  expect(count("merchant", loginPath)).toBe(postsBeforeCancel);
  await expect
    .poll(() =>
      page.evaluate(
        (index) => window.__loginBridge205.instances[index].destroyed,
        cancelled.index,
      ),
    )
    .toBe(true);

  stage = "late-username-context";
  const stale = await begin(page);
  const postsBeforeStale = count("merchant", loginPath);
  // A real queued v-model input event can arrive after verification disables
  // the control. Exercise the same event path rather than changing Vue state.
  await page.locator('[name="username"]').evaluate((field) => {
    field.value = "queued-account205";
    field.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await expect(loginButton(page)).toBeEnabled();
  await complete(page, stale);
  expect(count("merchant", loginPath)).toBe(postsBeforeStale);
  await expect
    .poll(() =>
      page.evaluate(
        (index) => window.__loginBridge205.instances[index].destroyed,
        stale.index,
      ),
    )
    .toBe(true);

  stage = "independent-key-login";
  const initsBeforeKey = count("merchant", initPath);
  await page.getByRole("button", { name: "密钥登录", exact: true }).click();
  await complete(page, cancelled);
  expect(count("merchant", loginPath)).toBe(postsBeforeStale);
  await page.locator('[name="merchant_id"]').fill("1");
  await page.locator('[name="api_key"]').fill("test-secret");
  const keyLogin = page.waitForResponse(
    isResponse("/console/v1/merchant/key-login", "POST"),
  );
  await loginButton(page).click();
  const keyResponse = await keyLogin,
    keySnapshot = captured(keyResponse);
  expect(keyResponse.status()).toBe(200);
  expect(Object.keys(keySnapshot.requestBody).sort()).toEqual([
    "api_key",
    "code",
    "merchant_id",
  ]);
  expect(keySnapshot.envelope.data.user.id).toBe("1");
  expect(keySnapshot.envelope.data.user.role).toBe("merchant");
  expect(count("merchant", initPath)).toBe(initsBeforeKey);
  await page.waitForURL((url) => url.pathname === "/merchant/dashboard");
  expect(documentCount("merchant", "/merchant/dashboard")).toBe(2);
  await expect(
    page.getByRole("button", { name: "退出登录", exact: true }),
  ).toBeVisible();
  state = await assertNoEffects(baseline);
  expect(state.merchant_sessions).toBe(baseline.merchant_sessions + 1);
  expect(state.claims).toBe(1);
  const loggedOut = page.waitForResponse(
    isResponse("/console/v1/merchant/logout", "POST"),
  );
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  const logoutResponse = await loggedOut;
  expect(logoutResponse.status()).toBe(200);
  expect(captured(logoutResponse).requestHeaders["x-csrf-token"]).toBe(
    keySnapshot.envelope.data.csrf_token,
  );
  await page.waitForURL((url) => url.pathname === "/merchant/login");
  expect(documentCount("merchant", "/merchant/login")).toBe(2);
  await expect(
    page.getByRole("heading", { name: "商户登录", exact: true }),
  ).toBeVisible();
  await gateReady(page);
  expect((await control("metrics")).merchant_sessions).toBe(
    baseline.merchant_sessions,
  );

  stage = "unknown-after-real-committed-session";
  const accepted = await begin(page);
  await page.evaluate(() => {
    window.__loginFault205.armed = true;
  });
  const acceptedLogin = page.waitForResponse(isResponse(loginPath, "POST"));
  const postsBeforeAccepted = count("merchant", loginPath);
  await complete(page, accepted);
  const acceptedResponse = await acceptedLogin,
    acceptedSnapshot = captured(acceptedResponse);
  expect(acceptedResponse.status()).toBe(200);
  expect(count("merchant", loginPath)).toBe(postsBeforeAccepted + 1);
  expect(acceptedSnapshot.requestBody.username).toBe(username);
  expect(acceptedSnapshot.requestBody.captcha_token).toBe(
    accepted.challenge.captcha_token,
  );
  expect(Object.keys(acceptedSnapshot.requestBody).sort()).toEqual(
    [
      "username",
      "password",
      "captcha_token",
      ...(version === 0
        ? ["geetest_challenge", "geetest_validate", "geetest_seccode"]
        : [
            "captcha_id",
            "lot_number",
            "pass_token",
            "gen_time",
            "captcha_output",
          ]),
    ].sort(),
  );
  const check = page.getByRole("button", {
    name: "重新检查登录状态",
    exact: true,
  });
  await expect(check).toBeVisible();
  expect(await page.evaluate(() => window.__loginFault205.fired)).toBe(1);
  await expect(loginButton(page)).toBeDisabled();
  const postsAtUnknown = count("merchant", loginPath),
    initsAtUnknown = count("merchant", initPath);
  await page
    .locator("form")
    .evaluate((form) =>
      form.dispatchEvent(
        new Event("submit", { bubbles: true, cancelable: true }),
      ),
    );
  expect(count("merchant", loginPath)).toBe(postsAtUnknown);
  expect(count("merchant", initPath)).toBe(initsAtUnknown);
  state = await assertNoEffects(baseline);
  expect(state.merchant_sessions).toBe(baseline.merchant_sessions + 1);
  expect(state.claims).toBe(2);
  expect(state.issued).toBe(1);
  const actualCookie = (
    await merchant.context.cookies(origin + "/console/v1/merchant")
  ).find((cookie) => cookie.name === "goepay_merchant");
  expect(actualCookie).toMatchObject({
    httpOnly: true,
    secure: true,
    sameSite: "Strict",
    path: "/console/v1/merchant",
  });
  await page.screenshot({
    path: path.join(artifacts, "unknown-real-session.png"),
    fullPage: true,
  });
  const sessionRead = page.waitForResponse(
    isResponse("/console/v1/merchant/session", "GET"),
  );
  await check.click();
  const restoredResponse = await sessionRead;
  expect(restoredResponse.status()).toBe(200);
  const restored = captured(restoredResponse).envelope.data;
  expect(restored.csrf_token).toBe(acceptedSnapshot.envelope.data.csrf_token);
  expect(restored.user).toEqual(acceptedSnapshot.envelope.data.user);
  await page.waitForURL((url) => url.pathname === "/merchant/dashboard");
  expect(documentCount("merchant", "/merchant/dashboard")).toBe(3);
  await expect(
    page.getByRole("button", { name: "退出登录", exact: true }),
  ).toBeVisible();
  expect(count("merchant", loginPath)).toBe(postsAtUnknown);
  expect(count("merchant", initPath)).toBe(initsAtUnknown);
  expect(
    (await json(page, "/console/v1/merchant/session")).body.data.csrf_token,
  ).toBe(restored.csrf_token);
  expect(
    (await json(page, loginPath, acceptedSnapshot.requestBody)).status,
  ).toBe(422);
  state = await assertNoEffects(baseline);
  expect(state.merchant_sessions).toBe(baseline.merchant_sessions + 1);
  expect(state.verifies).toBe(3);
  await cleanStorage(page);
  await page.screenshot({
    path: path.join(artifacts, "restored-merchant.png"),
    fullPage: true,
  });

  stage = "independent-admin-password";
  const admin = await contextFor("admin");
  await admin.page.goto(origin + "/admin/login");
  await expect(
    admin.page.getByRole("heading", { name: "管理员登录", exact: true }),
  ).toBeVisible();
  await expect(
    admin.page.getByRole("group", { name: "登录验证码", exact: true }),
  ).toHaveCount(0);
  await fill(admin.page, "admin168", password);
  const adminLogin = admin.page.waitForResponse(
    isResponse("/console/v1/admin/login", "POST"),
  );
  await loginButton(admin.page).click();
  const adminResponse = await adminLogin,
    adminSnapshot = captured(adminResponse);
  expect(adminResponse.status()).toBe(200);
  expect(Object.keys(adminSnapshot.requestBody).sort()).toEqual([
    "password",
    "username",
  ]);
  expect(adminSnapshot.envelope.data.user).toMatchObject({
    id: "168",
    role: "admin",
    admin_class: "super",
  });
  await expect(
    admin.page.getByRole("button", { name: "退出登录", exact: true }),
  ).toBeVisible();
  const adminRead = await json(admin.page, "/console/v1/admin/session");
  expect(adminRead.status).toBe(200);
  expect(adminRead.body.data.csrf_token).toBe(
    adminSnapshot.envelope.data.csrf_token,
  );
  expect(count("admin", initPath)).toBe(0);
  expect(count("admin", metadataPath, "GET")).toBe(0);
  expect(admin.counters.sdkLoads).toBe(0);
  await cleanStorage(admin.page);
  await admin.page.screenshot({
    path: path.join(artifacts, "original-admin.png"),
    fullPage: true,
  });

  stage = "deployed-csp-and-final-boundaries";
  await blockedScript(admin.page, rejectedSDK);
  await blockedScript(admin.page, sdkURL);
  await blockedScript(page, sdkURL);
  expect(rejectedRequests).toBe(0);
  expect(admin.counters.sdkLoads).toBe(0);
  state = await assertNoEffects(baseline);
  expect(state.admin_sessions).toBe(baseline.admin_sessions + 1);
  expect(state.merchant_sessions).toBe(baseline.merchant_sessions + 1);
  expect(state.claims).toBe(2);
  expect(state.consumed).toBe(2);
  expect(state.issued).toBe(1);
  expect(state.verifies).toBe(3);
  expect(state.rejected).toBe(1);
  expect(external).toBe(0);
  expect(websockets).toBe(0);
  expect(pageErrors).toEqual([]);
  results.push({
    version: version + 3,
    width,
    actual_http: true,
    actual_pg: true,
    persistent_sessions: true,
    production_csp: true,
    anonymous_dashboard_enters_fresh_canonical_login_document: true,
    successful_login_and_session_recovery_restore_strict_dashboard_document: true,
    official_host_private_ca: true,
    vendor_sdk_fulfilled_locally: true,
    actual_vendor_sdk_executed: false,
    configured_origin_rejects_matching_host_spoof: true,
    missing_and_wrong_proof_rejected: true,
    wrong_password_burns_proof: true,
    cancelled_and_late_context_do_not_send_credentials: true,
    independent_key_and_admin_login: true,
    unknown_fault: "browser-discard-after-real-200-cookie",
    unknown_blocks_init_and_credentials: true,
    recovery_uses_only_real_session_read: true,
    committed_session_cookie_received_by_browser: true,
    storage_writes: 0,
    external_requests: external,
    effects: state.effects,
  });
  await productionCSP.write();
  await fs.writeFile(
    path.join(artifacts, "result.json"),
    JSON.stringify(
      {
        production: false,
        real_funds: false,
        real_vendor_sdk: false,
        results,
        api_responses: diagnostics,
        document_navigations: documentNavigations,
      },
      null,
      2,
    ) + "\n",
  );
  await admin.context.close();
  await merchant.context.close();
} catch (error) {
  await productionCSP.write(error);
  if (currentPage && !currentPage.isClosed())
    await currentPage
      .screenshot({ path: path.join(artifacts, "failure.png"), fullPage: true })
      .catch(() => {});
  await fs.writeFile(
    path.join(artifacts, "failure.json"),
    JSON.stringify(
      {
        production: false,
        real_funds: false,
        stage,
        error: String(error.message ?? error),
        external_requests: external,
        websocket_requests: websockets,
        page_errors: pageErrors,
        api_responses: diagnostics,
        document_navigations: documentNavigations,
      },
      null,
      2,
    ) + "\n",
  );
  throw error;
} finally {
  await browser?.close();
}
