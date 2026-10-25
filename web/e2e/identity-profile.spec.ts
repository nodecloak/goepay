import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const provider of ["wxpay", "alipay"])
  test(`independent ${provider} mini profile uses SDK confirmation and single reveal with no payment or fake URL link`, async ({
    page,
  }) => {
    const id = "a".repeat(64),
      state = "b".repeat(64),
      expires = new Date(Date.now() + 240000).toISOString(),
      profile = {
        id: "11",
        revision: "2",
        channel_id: "7",
        provider,
        kind: "mini",
        app_id: "independent-app",
        path: "pages/identity/index",
      };
    let status = "pending",
      exchanges = 0,
      reveals = 0;
    const calls: string[] = [],
      writes: unknown[] = [];
    const request = () => ({
      request_id: id,
      merchant_id: "1000",
      channel_id: "7",
      profile_id: "11",
      profile_revision: "2",
      provider,
      kind: "mini",
      app_id: profile.app_id,
      path: profile.path,
      status,
      expires_at: expires,
    });
    await page.route("**/console/v1/**", (r) => {
      const req = r.request(),
        p = new URL(req.url()).pathname;
      calls.push(p);
      if (p.endsWith("/session"))
        return r.fulfill(
          ok({
            user: {
              id: "1",
              name: "Admin",
              role: "admin",
              admin_class: "operator",
            },
            csrf_token: "c",
          }),
        );
      if (p.endsWith("/identity-lookups/capabilities"))
        return r.fulfill(
          ok({
            merchant_id: "1000",
            available: false,
            reason: "legacy disabled",
            channels: [],
          }),
        );
      if (p.endsWith("/identity-profile-lookups/capabilities"))
        return r.fulfill(
          ok({
            merchant_id: "1000",
            available: true,
            reason: "",
            profiles: [profile],
            url_link_available: false,
          }),
        );
      if (p.endsWith("/identity-profile-lookups")) {
        writes.push(req.postDataJSON());
        return r.fulfill(
          ok(
            {
              request: request(),
              url_link_available: false,
              launch: {
                kind: "sdk",
                provider,
                app_id: profile.app_id,
                path: profile.path,
                method: "POST",
                completion_url: `https://pay.example.invalid/identity/profile/mini/${provider}/11/${id}/`,
                body: { state },
                expires_at: expires,
              },
            },
            201,
          ),
        );
      }
      if (p.endsWith("/complete")) {
        exchanges++;
        writes.push(req.postDataJSON());
        status = "verified";
        return r.fulfill(ok({ request_id: id, status }));
      }
      if (p.endsWith("/reveal")) {
        reveals++;
        status = "revealed";
        return r.fulfill(
          ok({
            provider,
            app_id: profile.app_id,
            label: provider === "wxpay" ? "微信OpenID" : "支付宝用户标识",
            subject_id: "private-independent-subject",
          }),
        );
      }
      if (p.endsWith("/" + id)) return r.fulfill(ok(request()));
      return r.fulfill(ok({}));
    });
    await page.goto("/admin/other/get-token");
    await page.getByRole("button", { name: "独立应用与小程序" }).click();
    await page.getByLabel("独立查询商户ID").fill("1000");
    await page
      .getByRole("button", { name: "读取独立应用", exact: true })
      .click();
    await page.getByLabel("授权应用档案").selectOption("11");
    await page
      .getByRole("button", { name: "创建独立授权请求", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "SDK对接资料" }),
    ).toBeVisible();
    await expect(page.getByText(/当前未提供厂商URL Link/)).toBeVisible();
    await expect(page.locator("img")).toHaveCount(0);
    expect(exchanges).toBe(0);
    await page.getByLabel("本次SDK临时code").fill("local-one-use-code");
    await page.getByRole("button", { name: "检查并兑换本次临时码" }).click();
    expect(exchanges).toBe(0);
    await page.getByRole("button", { name: "确认兑换标识" }).click();
    await expect(
      page.getByRole("button", { name: "单次查看独立标识" }),
    ).toBeVisible();
    expect(writes).toEqual([
      { merchant_id: "1000", profile_id: "11", profile_revision: "2" },
      { state, code: "local-one-use-code" },
    ]);
    await page.getByRole("button", { name: "单次查看独立标识" }).click();
    await expect(
      page.getByText("private-independent-subject", { exact: true }),
    ).toBeVisible();
    expect(reveals).toBe(1);
    expect(exchanges).toBe(1);
    expect(
      calls.some((p) => /orders|transfers|refunds|resources/.test(p)),
    ).toBe(false);
    expect(
      await page.evaluate(() =>
        JSON.stringify({ ...localStorage, ...sessionStorage }),
      ),
    ).not.toContain("private-independent-subject");
    expect(
      await page.evaluate(() =>
        JSON.stringify({ ...localStorage, ...sessionStorage }),
      ),
    ).not.toContain("local-one-use-code");
    await page.setViewportSize({ width: 375, height: 812 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "原收款应用网页授权" }).click();
    await expect(
      page.getByText("private-independent-subject", { exact: true }),
    ).toHaveCount(0);
  });
test("unknown independent SDK completion cannot resubmit and delegated sessions make no profile requests", async ({
  page,
}) => {
  const id = "c".repeat(64),
    state = "d".repeat(64),
    expires = new Date(Date.now() + 240000).toISOString();
  let delegated = false,
    exchanges = 0,
    reads = 0,
    profileCalls = 0;
  const p = {
    id: "11",
    revision: "1",
    channel_id: "7",
    provider: "wxpay",
    kind: "mini",
    app_id: "app",
    path: "pages/id",
  };
  const q = {
    request_id: id,
    merchant_id: "1000",
    channel_id: "7",
    profile_id: "11",
    profile_revision: "1",
    provider: p.provider,
    kind: p.kind,
    app_id: p.app_id,
    path: p.path,
    status: "processing",
    expires_at: expires,
  };
  await page.route("**/console/v1/**", (r) => {
    const path = new URL(r.request().url()).pathname;
    if (path.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
          ...(delegated ? { delegated_by: { id: "9", name: "Issuer" } } : {}),
        }),
      );
    if (path.includes("identity-profile-lookups")) profileCalls++;
    if (path.endsWith("/capabilities"))
      return r.fulfill(
        ok({
          merchant_id: "1000",
          available: true,
          reason: "",
          profiles: [p],
          url_link_available: false,
        }),
      );
    if (path.endsWith("/identity-profile-lookups"))
      return r.fulfill(
        ok(
          {
            request: { ...q, status: "pending" },
            url_link_available: false,
            launch: {
              kind: "sdk",
              provider: p.provider,
              app_id: p.app_id,
              path: p.path,
              method: "POST",
              completion_url: `https://pay.example.invalid/identity/profile/mini/wxpay/11/${id}/`,
              body: { state },
              expires_at: expires,
            },
          },
          201,
        ),
      );
    if (path.endsWith("/complete")) {
      exchanges++;
      return r.abort("failed");
    }
    if (path.endsWith("/" + id)) {
      reads++;
      return r.fulfill(ok(q));
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/admin/other/get-token");
  await page.getByRole("button", { name: "独立应用与小程序" }).click();
  await page.getByLabel("独立查询商户ID").fill("1000");
  await page.getByRole("button", { name: "读取独立应用", exact: true }).click();
  await page.getByLabel("授权应用档案").selectOption("11");
  await page
    .getByRole("button", { name: "创建独立授权请求", exact: true })
    .click();
  await page.getByLabel("本次SDK临时code").fill("once-code");
  await page.getByRole("button", { name: "检查并兑换本次临时码" }).click();
  await page.getByRole("button", { name: "确认兑换标识" }).click();
  await expect(page.getByText(/兑换处理中或响应未确定/)).toBeVisible();
  await expect(page.getByLabel("本次SDK临时code")).toHaveCount(0);
  await page.getByRole("button", { name: "查询独立授权状态" }).click();
  expect(exchanges).toBe(1);
  expect(reads).toBe(1);
  const before = profileCalls;
  delegated = true;
  await page.reload();
  await page.getByRole("button", { name: "独立应用与小程序" }).click();
  await expect(page.getByText(/代登录不可发起或读取授权/)).toBeVisible();
  expect(profileCalls).toBe(before);
});
