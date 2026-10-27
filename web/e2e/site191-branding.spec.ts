import { test, expect, type Page } from "@playwright/test";
import platform179 from "./globalaccess179-http-fixture.json" with { type: "json" };

const site = {
  sitename: "原网站191 <b>",
  title: "原网站标题191",
  keywords: "支付,191",
  description: "原网站说明",
  orgname: "公司191",
  email: "contact191@example.test",
  kfqq: "123456",
  kfqq_url: "https://qq.example.invalid/",
  wework_url: "",
  qqqun: "",
  login_subtitle: "原登录标语191",
  homepage: "0",
  homepage_url: "",
};
async function fixture(
  page: Page,
  baseURL: string | undefined,
  area: "admin" | "merchant",
  loggedIn: boolean,
) {
  if (!baseURL || !/^http:\/\/127\.0\.0\.1:\d+$/.test(new URL(baseURL).origin))
    throw Error("local synthetic fixture required");
  const origin = new URL(baseURL).origin,
    foreign: string[] = [],
    writes: string[] = [];
  await page.context().route("**/*", (r) => {
    if (new URL(r.request().url()).origin === origin) return r.continue();
    foreign.push(r.request().url());
    return r.abort();
  });
  const ok = (data: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify({ code: 0, data }),
  });
  await page.route("**/console/v1/**", (r) => {
    const p = new URL(r.request().url()).pathname;
    if (r.request().method() !== "GET") writes.push(p);
    if (p === "/console/v1/public/site") return r.fulfill(ok(site));
    if (p.endsWith("/session"))
      return r.fulfill(
        loggedIn
          ? ok({
              user: {
                id: "1",
                name: "合成191",
                role: area,
                admin_class: "super",
              },
              csrf_token: "synthetic191",
            })
          : {
              status: 401,
              contentType: "application/json",
              body: JSON.stringify({ code: 401, message: "请登录" }),
            },
      );
    if (p.endsWith("/merchant-access"))
      return r.fulfill(
        ok({
          login_open: true,
          registration_open: true,
          key_login_open: false,
        }),
      );
    if (p.endsWith("/layout"))
      return r.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    if (p.endsWith("/dashboard"))
      return r.fulfill(
        ok({
          orders: 0,
          paid_orders: 0,
          available_money: "0.00",
          frozen_money: "0.00",
        }),
      );
    return r.fulfill(ok({ items: [], total: 0 }));
  });
  return { foreign, writes };
}

for (const width of [375, 1440]) {
  test(`original website fields clear and refresh saved branding ${width}`, async ({
    page,
    baseURL,
  }) => {
    await page.setViewportSize({ width, height: 960 });
    const traffic = await fixture(page, baseURL, "admin", true);
    const rows = Object.entries(site).map(([key, value], index) => ({
      id: String(index + 1),
      key,
      value,
    }));
    rows.push(
      { id: "101", key: "site_name", value: "旧兼容值" },
      { id: "102", key: "support_email", value: "old@example.test" },
    );
    const writes: unknown[] = [];
    let publicReads = 0;
    await page.route("**/console/v1/**", async (route) => {
      const request = route.request(),
        path = new URL(request.url()).pathname;
      const ok = (data: unknown) =>
        route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      if (path === "/console/v1/public/site") {
        publicReads++;
        const state = Object.fromEntries(
          rows.map((row) => [row.key, row.value]),
        );
        return ok({ ...site, ...state, title: state.title || state.sitename });
      }
      if (path.endsWith("/settings/merchant-access"))
        return ok(platform179.admin_default);
      if (path.endsWith("/resources/settings/schema"))
        return ok({
          title: "系统设置",
          permissions: { create: true, update: true, delete: false },
          fields: [
            {
              name: "key",
              type: "select",
              options: rows.map((row) => ({ label: row.key, value: row.key })),
            },
            { name: "value", type: "string" },
          ],
        });
      if (path.endsWith("/resources/settings"))
        return ok({ items: rows, total: rows.length });
      const match = path.match(/\/resources\/settings\/([1-9]\d*)$/);
      if (match) {
        const row = rows.find((x) => x.id === match[1]);
        if (!row) throw Error("Unknown setting fixture ID");
        if (request.method() === "PUT") {
          expect(request.headers()["x-csrf-token"]).toBe("synthetic191");
          writes.push(request.postDataJSON());
          row.value = request.postDataJSON().value;
        } else expect(request.method()).toBe("GET");
        return ok(row);
      }
      return route.fallback();
    });
    await page.goto("/admin/system/settings");
    await expect(page.getByLabel("网站名称", { exact: true })).toHaveValue(
      site.sitename,
    );
    await expect(page.getByLabel("联系邮箱", { exact: true })).toHaveValue(
      site.email,
    );
    await expect(page.locator("#setting-site_name")).toHaveCount(0);
    await expect(page.locator("#setting-support_email")).toHaveCount(0);
    await page.getByLabel("网站标题", { exact: true }).fill("");
    await page.getByLabel("网站首页", { exact: true }).selectOption("3");
    await page
      .getByRole("button", { name: "保存本页设置", exact: true })
      .click();
    await expect(
      page.getByRole("status").filter({ hasText: "已保存2项设置" }),
    ).toBeVisible();
    await expect(page).toHaveTitle(
      new RegExp(site.sitename.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")),
    );
    expect(writes).toEqual(
      expect.arrayContaining([{ value: "" }, { value: "3" }]),
    );
    expect(writes).toHaveLength(2);
    expect(publicReads).toBeGreaterThanOrEqual(2);
    expect(traffic.foreign).toEqual([]);
  });
  for (const area of ["admin", "merchant"] as const) {
    test(`original site login ${area} ${width}`, async ({ page, baseURL }) => {
      await page.setViewportSize({ width, height: 960 });
      const traffic = await fixture(page, baseURL, area, false);
      await page.goto(`/${area}/dashboard`);
      await expect(
        page.getByRole("heading", {
          name: area === "admin" ? "管理员登录" : "商户登录",
        }),
      ).toBeVisible();
      await expect(page.locator(".login-intro .brand")).toContainText(
        site.sitename,
      );
      await expect(
        page.getByText(site.login_subtitle, { exact: true }),
      ).toBeVisible();
      await expect(page).toHaveTitle(new RegExp(site.title));
      expect(await page.locator(".brand b").count()).toBe(0);
      expect(traffic.foreign).toEqual([]);
      expect(traffic.writes).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }
  test(`original site workspace branding ${width}`, async ({
    page,
    baseURL,
  }) => {
    await page.setViewportSize({ width, height: 960 });
    const traffic = await fixture(page, baseURL, "merchant", true);
    await page.goto("/merchant/dashboard");
    if (width === 375)
      await page.getByRole("button", { name: "展开菜单" }).click();
    await expect(page.locator(".brand-name")).toHaveText(site.sitename);
    await expect(page).toHaveTitle(new RegExp(site.title));
    expect(traffic.foreign).toEqual([]);
    expect(traffic.writes).toEqual([]);
  });
}
