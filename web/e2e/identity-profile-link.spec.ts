import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const host of ["wxaurl.cn", "wxmpurl.cn"])
  test(`mini URL Link on ${host} requires explicit generation/open and preserves independent SDK completion`, async ({
    page,
    context,
    baseURL,
  }) => {
    const id = "a".repeat(64),
      state = "b".repeat(64),
      expiry = new Date(Date.now() + 240000).toISOString();
    const profile = {
      id: "11",
      revision: "2",
      channel_id: "7",
      provider: "wxpay",
      kind: "mini",
      app_id: "independent-mini-app",
      path: "pages/identity/index",
      url_link_available: true,
    };
    const request = {
      request_id: id,
      merchant_id: "1000",
      channel_id: "7",
      profile_id: "11",
      profile_revision: "2",
      provider: "wxpay",
      kind: "mini",
      app_id: profile.app_id,
      path: profile.path,
      status: "pending",
      expires_at: expiry,
    };
    const link = {
      purpose: "wechat_mini_identity_url_link",
      request_id: id,
      provider: "wxpay",
      app_id: profile.app_id,
      status: "ready",
      created_at: new Date().toISOString(),
      expires_at: expiry,
      requested_expires_at: new Date(
        Math.floor(Date.parse(expiry) / 1000) * 1000,
      ).toISOString(),
      url_link: `https://${host}/synthetic-ticket`,
    };
    let generates = 0,
      opens = 0,
      completes = 0;
    const calls: string[] = [],
      writes: unknown[] = [],
      external: string[] = [];
    await context.route("**/*", (r) => {
      if (
        !baseURL ||
        new URL(r.request().url()).origin !== new URL(baseURL).origin
      ) {
        external.push(r.request().url());
        return r.abort();
      }
      return r.continue();
    });
    await context.route(`https://${host}/synthetic-ticket`, (r) => {
      opens++;
      return r.fulfill({
        status: 200,
        contentType: "text/plain",
        body: "Synthetic mini link page",
      });
    });
    await page.route("**/console/v1/**", (r) => {
      const req = r.request(),
        path = new URL(req.url()).pathname;
      calls.push(path);
      if (path.endsWith("/session"))
        return r.fulfill(
          ok({
            user: {
              id: "1",
              role: "admin",
              name: "Local admin",
              admin_class: "operator",
            },
            csrf_token: "synthetic-csrf",
          }),
        );
      if (path.endsWith("/identity-profile-lookups/capabilities"))
        return r.fulfill(
          ok({
            merchant_id: "1000",
            available: true,
            reason: "",
            profiles: [profile],
            url_link_available: true,
          }),
        );
      if (path.endsWith("/identity-profile-lookups")) {
        writes.push(req.postDataJSON());
        return r.fulfill(
          ok(
            {
              request,
              url_link_available: true,
              launch: {
                kind: "sdk",
                provider: "wxpay",
                app_id: profile.app_id,
                path: profile.path,
                method: "POST",
                completion_url: `https://pay.example.invalid/identity/profile/mini/wxpay/11/${id}/`,
                body: { state },
                expires_at: expiry,
              },
            },
            201,
          ),
        );
      }
      if (path.endsWith("/url-link")) {
        if (req.method() === "POST") {
          generates++;
          writes.push(req.postDataJSON());
        }
        return r.fulfill(ok(link));
      }
      if (path.endsWith("/complete")) {
        completes++;
        writes.push(req.postDataJSON());
        return r.fulfill(ok({ request_id: id, status: "verified" }));
      }
      return r.fulfill(ok(request));
    });
    await page.goto("/admin/other/get-token");
    await page
      .getByRole("button", { name: "独立应用与小程序", exact: true })
      .click();
    await page.getByLabel("独立查询商户ID").fill("1000");
    await page
      .getByRole("button", { name: "读取独立应用", exact: true })
      .click();
    await page.getByLabel("授权应用档案").selectOption("11");
    await page
      .getByRole("button", { name: "创建独立授权请求", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "SDK对接资料", exact: true }),
    ).toBeVisible();
    expect(generates).toBe(0);
    expect(opens).toBe(0);
    await page
      .getByRole("button", { name: "生成本次小程序URL Link", exact: true })
      .click();
    const open = page.getByRole("link", {
      name: "打开本次微信小程序链接",
      exact: true,
    });
    await expect(open).toHaveAttribute("href", link.url_link);
    expect(generates).toBe(1);
    expect(opens).toBe(0);
    await expect(
      page.getByRole("button", { name: "生成本次小程序URL Link", exact: true }),
    ).toBeDisabled();
    const popupPromise = context.waitForEvent("page");
    await open.click();
    const popup = await popupPromise;
    await expect(popup.getByText("Synthetic mini link page")).toBeVisible();
    await popup.close();
    expect(opens).toBe(1);
    await page
      .getByRole("button", { name: "查询已有URL Link结果", exact: true })
      .click();
    expect(generates).toBe(1);
    expect(writes).toEqual([
      { profile_id: "11", profile_revision: "2", merchant_id: "1000" },
      { state },
    ]);
    await page
      .getByRole("button", { name: "显示本次身份查询二维码", exact: true })
      .click();
    const qr = page.getByRole("img", {
      name: "本次微信小程序身份查询二维码",
      exact: true,
    });
    await expect(qr).toHaveAttribute("src", /^data:image\/png;base64,/);
    expect(generates).toBe(1);
    expect(opens).toBe(1);
    await page.setViewportSize({ width: 375, height: 812 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    if (host === "wxaurl.cn")
      await page.screenshot({
        path: "../.tools/mini-link-ui-375.png",
        fullPage: true,
      });
    await page.getByLabel("本次SDK临时code").fill("one-use-sdk-code");
    await page
      .getByRole("button", { name: "检查并兑换本次临时码", exact: true })
      .click();
    await page
      .getByRole("button", { name: "确认兑换标识", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "单次查看独立标识", exact: true }),
    ).toBeVisible();
    await expect(open).toHaveCount(0);
    await expect(qr).toHaveCount(0);
    expect(completes).toBe(1);
    expect(writes[2]).toEqual({ state, code: "one-use-sdk-code" });
    expect(
      calls.some((p) => /orders|transfers|refunds|resources/.test(p)),
    ).toBe(false);
    expect(external).toEqual([]);
    expect(
      await page.evaluate(() =>
        JSON.stringify({ ...localStorage, ...sessionStorage }),
      ),
    ).not.toContain("synthetic-ticket");
  });

test("unknown URL Link generation never resends after a local ready GET and clears on context switch", async ({
  page,
}) => {
  const id = "c".repeat(64),
    state = "d".repeat(64),
    expiry = new Date(Date.now() + 240000).toISOString();
  const profile = {
    id: "11",
    revision: "1",
    channel_id: "7",
    provider: "wxpay",
    kind: "mini",
    app_id: "independent-mini",
    path: "pages/identity/index",
    url_link_available: true,
  };
  const request = {
    request_id: id,
    merchant_id: "1000",
    channel_id: "7",
    profile_id: "11",
    profile_revision: "1",
    provider: "wxpay",
    kind: "mini",
    app_id: profile.app_id,
    path: profile.path,
    status: "pending",
    expires_at: expiry,
  };
  let generates = 0,
    reads = 0;
  await page.route("**/console/v1/**", (r) => {
    const req = r.request(),
      path = new URL(req.url()).pathname;
    if (path.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (path.endsWith("/capabilities"))
      return r.fulfill(
        ok({
          merchant_id: "1000",
          available: true,
          reason: "",
          profiles: [profile],
          url_link_available: true,
        }),
      );
    if (path.endsWith("/identity-profile-lookups"))
      return r.fulfill(
        ok(
          {
            request,
            url_link_available: true,
            launch: {
              kind: "sdk",
              provider: "wxpay",
              app_id: profile.app_id,
              path: profile.path,
              method: "POST",
              completion_url: `https://pay.example.invalid/identity/profile/mini/wxpay/11/${id}/`,
              body: { state },
              expires_at: expiry,
            },
          },
          201,
        ),
      );
    if (path.endsWith("/url-link")) {
      if (req.method() === "POST") {
        generates++;
        return r.abort("failed");
      }
      reads++;
      expect(req.postData()).toBe(null);
      return r.fulfill(
        ok({
          purpose: "wechat_mini_identity_url_link",
          request_id: id,
          provider: "wxpay",
          app_id: profile.app_id,
          status: "ready",
          created_at: new Date().toISOString(),
          expires_at: expiry,
          requested_expires_at: new Date(
            Math.floor(Date.parse(expiry) / 1000) * 1000,
          ).toISOString(),
          url_link: "https://wxaurl.cn/synthetic-ticket",
        }),
      );
    }
    return r.fulfill(ok(request));
  });
  await page.goto("/admin/other/get-token");
  await page
    .getByRole("button", { name: "独立应用与小程序", exact: true })
    .click();
  await page.getByLabel("独立查询商户ID").fill("1000");
  await page.getByRole("button", { name: "读取独立应用", exact: true }).click();
  await page.getByLabel("授权应用档案").selectOption("11");
  await page
    .getByRole("button", { name: "创建独立授权请求", exact: true })
    .click();
  await page
    .getByRole("button", { name: "生成本次小程序URL Link", exact: true })
    .click();
  await expect(
    page.getByText(
      "URL Link生成结果不确定，本次请求不会再次生成；仅可手动查询已有结果。",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "生成本次小程序URL Link", exact: true }),
  ).toBeDisabled();
  await expect(page.getByLabel("本次SDK临时code")).toBeVisible();
  await page
    .getByRole("button", { name: "查询已有URL Link结果", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: "打开本次微信小程序链接", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "生成本次小程序URL Link", exact: true }),
  ).toBeDisabled();
  expect(generates).toBe(1);
  expect(reads).toBe(1);
  await page.getByLabel("独立查询商户ID").fill("1001");
  await expect(
    page.getByRole("link", { name: "打开本次微信小程序链接", exact: true }),
  ).toHaveCount(0);
});
