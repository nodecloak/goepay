import { test, expect } from "@playwright/test";

const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const id = "a".repeat(64),
  authorizationID = "b".repeat(64);
for (const mode of ["basic-browser", "assigned-browser", "assigned-wallet"]) {
  test(`${mode} application authorization confirms one launch and reveals only a masked memory credential at 375px`, async ({
    page,
    context,
    baseURL,
  }) => {
    if (!baseURL) throw Error("Configured browser baseURL is required");
    const consoleOrigin = new URL(baseURL).origin;
    const assigned = mode.startsWith("assigned"),
      wallet = mode.endsWith("wallet");
    const profile = {
      id: "21",
      revision: "2",
      issuer_app_id: "2021000000000001",
      expected_auth_app_id: "2021000000000002",
      launch_kind: assigned ? "assigned" : "basic",
      ...(assigned ? { app_types: ["WEBAPP", "TINYAPP"] } : {}),
    };
    const expires = new Date(Date.now() + 240000).toISOString();
    let status = "pending",
      reveals = 0;
    const calls: string[] = [],
      writes: unknown[] = [],
      launches: string[] = [],
      external: string[] = [];
    await context.route("**/*", (route) => {
      const u = new URL(route.request().url());
      if (u.origin !== consoleOrigin) {
        external.push(u.origin);
        return route.abort();
      }
      return route.continue();
    });
    await context.route(
      "https://pay.example.invalid/identity/application/**",
      (route) => {
        launches.push(new URL(route.request().url()).pathname);
        return route.fulfill({
          status: 200,
          contentType: "text/plain",
          body: "Synthetic local authorization page",
        });
      },
    );
    const metadata = () => ({
      request_id: id,
      purpose: "alipay_app_authorization",
      merchant_id: "1000",
      profile_id: "21",
      profile_revision: "2",
      issuer_app_id: profile.issuer_app_id,
      expected_auth_app_id: profile.expected_auth_app_id,
      launch_kind: profile.launch_kind,
      app_types: assigned ? ["WEBAPP", "TINYAPP"] : null,
      status,
      expires_at: expires,
    });
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      calls.push(p);
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "Admin",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "synthetic-csrf",
          }),
        );
      if (p.endsWith("/application-authorizations/capabilities")) {
        expect(new URL(req.url()).search).toBe("?merchant_id=1000");
        return route.fulfill(
          ok({
            merchant_id: "1000",
            available: true,
            reason: "",
            authorization_origin: "https://pay.example.invalid",
            profiles: [profile],
          }),
        );
      }
      if (p.endsWith("/application-authorizations")) {
        expect(req.method()).toBe("POST");
        writes.push(req.postDataJSON());
        return route.fulfill(
          ok(
            {
              request: metadata(),
              authorization_url: `https://pay.example.invalid/identity/application/authorize/${authorizationID}`,
              ...(assigned
                ? {
                    wallet_authorization_url: `https://pay.example.invalid/identity/application/wallet/${authorizationID}`,
                  }
                : {}),
            },
            201,
          ),
        );
      }
      if (p.endsWith(`/${id}/reveal`)) {
        reveals++;
        writes.push(req.postDataJSON());
        status = "revealed";
        return route.fulfill(
          ok({
            provider: "alipay",
            issuer_app_id: profile.issuer_app_id,
            auth_app_id: profile.expected_auth_app_id,
            user_id: "2088000000000000",
            app_auth_token: "synthetic-private-application-token",
            token_expires_in_seconds: "0",
          }),
        );
      }
      if (p.endsWith(`/${id}`)) {
        status = "verified";
        return route.fulfill(ok(metadata()));
      }
      return route.fulfill(ok({}));
    });
    await page.goto("/admin/other/get-token");
    await page
      .getByRole("button", { name: "支付宝应用授权", exact: true })
      .click();
    expect(
      calls.filter((p) => p.includes("application-authorizations")),
    ).toHaveLength(0);
    await page.getByLabel("应用授权商户ID", { exact: true }).fill("1000");
    await page
      .getByRole("button", { name: "读取应用授权档案", exact: true })
      .click();
    await page.getByLabel("应用授权档案", { exact: true }).selectOption("21");
    if (wallet)
      await page.getByLabel("应用授权打开方式").selectOption("wallet");
    await page
      .getByRole("button", { name: "核对并创建应用授权", exact: true })
      .click();
    expect(writes).toHaveLength(0);
    await expect(
      page.getByRole("dialog", { name: "确认创建应用授权" }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "确认创建应用授权", exact: true })
      .click();
    const launch = page.locator('[data-action="app-launch"]');
    await expect(launch).toHaveAttribute(
      "href",
      `https://pay.example.invalid/identity/application/${wallet ? "wallet" : "authorize"}/${authorizationID}`,
    );
    expect(writes).toEqual([
      { merchant_id: "1000", profile_id: "21", profile_revision: "2" },
    ]);
    expect(launches).toHaveLength(0);
    const popupPromise = context.waitForEvent("page");
    await launch.click();
    const popup = await popupPromise;
    await expect(
      popup.getByText("Synthetic local authorization page"),
    ).toBeVisible();
    await popup.close();
    expect(launches).toEqual([
      `/identity/application/${wallet ? "wallet" : "authorize"}/${authorizationID}`,
    ]);
    await expect(launch).toHaveCount(0);
    await page.getByRole("button", { name: "查询本次应用授权状态" }).click();
    expect(reveals).toBe(0);
    await page.getByRole("button", { name: "单次查看应用授权Token" }).click();
    const token = page.getByLabel("应用授权Token", { exact: true });
    await expect(token).toHaveAttribute("type", "password");
    await expect(token).toHaveValue("synthetic-private-application-token");
    await expect(
      page.getByRole("button", { name: "单次查看应用授权Token" }),
    ).toHaveCount(0);
    await expect(page.getByText(/厂商有效期未知/)).toBeVisible();
    expect(reveals).toBe(1);
    expect(writes).toEqual([
      { merchant_id: "1000", profile_id: "21", profile_revision: "2" },
      {},
    ]);
    expect(await page.locator("body").innerText()).not.toContain(
      "synthetic-private-application-token",
    );
    expect(
      await page.evaluate(() =>
        JSON.stringify({ ...localStorage, ...sessionStorage }),
      ),
    ).not.toContain("synthetic-private-application-token");
    expect(page.url()).not.toContain("synthetic-private-application-token");
    expect(new URL(page.url()).search).toBe("");
    expect(
      calls.some((p) => /orders|transfers|refunds|resources/.test(p)),
    ).toBe(false);
    expect(external).toEqual([]);
    await page.setViewportSize({ width: 375, height: 812 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    if (mode === "assigned-wallet")
      await page.screenshot({
        path: "../.tools/app-auth-ui-375.png",
        fullPage: true,
      });
    await page
      .getByRole("button", { name: "原收款应用网页授权", exact: true })
      .click();
    await expect(token).toHaveCount(0);
  });
}

test("unknown creation requires explicit reset and unknown reveal remains consumed after manual GET", async ({
  page,
}) => {
  let createFailure = true,
    creates = 0,
    reveals = 0;
  const profile = {
    id: "21",
    revision: "1",
    issuer_app_id: "2021000000000001",
    expected_auth_app_id: "2021000000000002",
    launch_kind: "basic",
  };
  const expires = new Date(Date.now() + 120000).toISOString();
  const metadata = (status = "pending") => ({
    request_id: id,
    purpose: "alipay_app_authorization",
    merchant_id: "1000",
    profile_id: "21",
    profile_revision: "1",
    issuer_app_id: profile.issuer_app_id,
    expected_auth_app_id: profile.expected_auth_app_id,
    launch_kind: "basic",
    app_types: null,
    status,
    expires_at: expires,
  });
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/capabilities"))
      return route.fulfill(
        ok({
          merchant_id: "1000",
          available: true,
          reason: "",
          authorization_origin: "https://pay.example.invalid",
          profiles: [profile],
        }),
      );
    if (p.endsWith("/application-authorizations")) {
      creates++;
      return createFailure
        ? route.abort()
        : route.fulfill(
            ok(
              {
                request: metadata(),
                authorization_url: `https://pay.example.invalid/identity/application/authorize/${authorizationID}`,
              },
              201,
            ),
          );
    }
    if (p.endsWith("/reveal")) {
      reveals++;
      return route.abort();
    }
    if (p.endsWith(`/${id}`)) return route.fulfill(ok(metadata("verified")));
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/other/get-token");
  await page
    .getByRole("button", { name: "支付宝应用授权", exact: true })
    .click();
  const select = async () => {
    await page.getByLabel("应用授权商户ID", { exact: true }).fill("1000");
    await page
      .getByRole("button", { name: "读取应用授权档案", exact: true })
      .click();
    await page.getByLabel("应用授权档案", { exact: true }).selectOption("21");
  };
  const create = async () => {
    await page
      .getByRole("button", { name: "核对并创建应用授权", exact: true })
      .click();
    await page
      .getByRole("button", { name: "确认创建应用授权", exact: true })
      .click();
  };
  await select();
  await create();
  await expect(
    page.getByRole("button", { name: "核对并创建应用授权", exact: true }),
  ).toBeDisabled();
  expect(creates).toBe(1);
  await page
    .getByRole("button", { name: "清除并重新选择", exact: true })
    .click();
  createFailure = false;
  await select();
  await create();
  await page.getByRole("button", { name: "查询本次应用授权状态" }).click();
  await page.getByRole("button", { name: "单次查看应用授权Token" }).click();
  await expect(
    page.getByText(
      "读取结果不确定，本次领取已按消耗处理，查询状态不会恢复领取。",
    ),
  ).toBeVisible();
  await page.getByRole("button", { name: "查询本次应用授权状态" }).click();
  await expect(
    page.getByRole("button", { name: "单次查看应用授权Token" }),
  ).toHaveCount(0);
  expect(reveals).toBe(1);
  expect(creates).toBe(2);
});

test("operator, delegated and merchant sessions cannot access the application authorization tool", async ({
  page,
}) => {
  for (const role of ["operator", "delegated", "merchant"]) {
    let calls = 0;
    await page.route("**/console/v1/**", (route) => {
      const p = new URL(route.request().url()).pathname;
      if (p.includes("application-authorizations")) calls++;
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "Local",
              role: role === "merchant" ? "merchant" : "admin",
              admin_class: role === "operator" ? "operator" : "super",
            },
            csrf_token: "c",
            ...(role === "delegated"
              ? { delegated_by: { id: "9", name: "Issuer" } }
              : {}),
          }),
        );
      return route.fulfill(
        ok({
          merchant_id: "1000",
          available: false,
          reason: "disabled",
          channels: [],
        }),
      );
    });
    await page.goto(
      `/${role === "merchant" ? "merchant" : "admin"}/other/get-token`,
    );
    if (role === "merchant")
      await expect(
        page.getByRole("heading", { name: "未找到该页面", exact: true }),
      ).toBeVisible();
    else
      await expect(
        page.getByRole("button", { name: "独立应用与小程序", exact: true }),
      ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "支付宝应用授权", exact: true }),
    ).toHaveCount(0);
    expect(calls).toBe(0);
    await page.unroute("**/console/v1/**");
  }
});

test("disabled capability reports no configured app-auth route", async ({
  page,
}) => {
  let creates = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/application-authorizations")) creates++;
    return route.fulfill(
      ok({
        merchant_id: "1000",
        available: false,
        reason: "application authorization is disabled",
        authorization_origin: "",
        profiles: [],
      }),
    );
  });
  await page.goto("/admin/other/get-token");
  await page
    .getByRole("button", { name: "支付宝应用授权", exact: true })
    .click();
  await page.getByLabel("应用授权商户ID", { exact: true }).fill("1000");
  await page
    .getByRole("button", { name: "读取应用授权档案", exact: true })
    .click();
  await expect(
    page.getByText("application authorization is disabled", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "核对并创建应用授权", exact: true }),
  ).toBeDisabled();
  expect(creates).toBe(0);
});
