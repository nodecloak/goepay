import { pageReload248 } from "./fixtures/page-reload248";
import { closedLoginCaptchaFixture205 } from "./fixtures/login-captcha205";
import { publicSiteFixture191 } from "./fixtures/site191";
import { test, expect, type Page } from "@playwright/test";
import actual from "./globalaccess179-http-fixture.json" with { type: "json" };
const csrf = "g".repeat(43);
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, message: "", data }),
});
async function settings(page: Page, mode = "normal") {
  let state = actual.admin_closed,
    reads = 0;
  const writes: unknown[] = [],
    errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.route("**/console/v1/**", async (r) => {
    const captcha205Reply = closedLoginCaptchaFixture205(r);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(r);
    if (siteReply191) return siteReply191;
    const q = r.request(),
      path = new URL(q.url()).pathname;
    if (
      q.method() === "GET" &&
      path === "/console/v1/admin/settings/branding" &&
      !new URL(q.url()).search &&
      !q.url().endsWith("?")
    )
      return r.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({
          code: -1,
          data: null,
          message: "synthetic independent branding setting unavailable",
        }),
      });
    // This newer independent setting is unavailable in the179 fixture. The
    // original gates must still work; every other unknown request stays strict.
    if (
      q.method() === "GET" &&
      (path === "/console/v1/admin/settings/scanpay" ||
        path === "/console/v1/admin/settings/scanpay-js" ||
        path === "/console/v1/admin/settings/captcha") &&
      !new URL(q.url()).search
    )
      return r.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({
          code: -1,
          data: null,
          message: "synthetic independent setting unavailable",
        }),
      });
    if (path.endsWith("/session"))
      return r.fulfill(
        ok({
          user: {
            id: "9007199254740993",
            name: "合成原super",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: csrf,
        }),
      );
    if (path === "/console/v1/public/layout")
      return r.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    if (path.endsWith("/resources/settings/schema"))
      return r.fulfill(
        ok({
          fields: [{ name: "key", type: "select", options: ["site_name"] }],
          permissions: { create: true, update: true, delete: false },
        }),
      );
    if (path.endsWith("/resources/settings"))
      return r.fulfill(ok({ items: [], total: 0 }));
    if (path.endsWith("/settings/merchant-access")) {
      if (q.method() === "PATCH") {
        const b = q.postDataJSON();
        writes.push(b);
        expect(q.headers()["x-csrf-token"]).toBe(csrf);
        expect(b.expected_fingerprint).toBe(state.fingerprint);
        if (b.close_keylogin === "0" && b.user_refund === "0")
          state = actual.admin_key_open_refund_closed;
        else if (b.close_keylogin === "1" && b.user_refund === "1")
          state = actual.admin_key_closed_refund_open;
        else if (b.close_keylogin === "0" && b.user_refund === "1")
          state = actual.admin_default;
        else throw Error("unexpected flags");
        if (mode === "lost") return r.abort("failed");
        if (mode === "malformed")
          return r.fulfill({
            status: 200,
            contentType: "application/json",
            body: "{",
          });
      } else reads++;
      return r.fulfill(ok(state));
    }
    throw Error(`unexpected ${q.method()} ${path}`);
  });
  await page.goto("/admin/system/settings");
  await expect(
    page.getByRole("region", { name: "网站图片设置", exact: true }),
  ).toContainText("品牌图片设置无法确认，请刷新页面后核对");
  return { writes, errors, reads: () => reads };
}
for (const width of [1440, 375])
  test(`original platform tabs preserve hidden gate at ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await settings(page);
    const key = page.getByRole("region", { name: "密钥登录", exact: true });
    await expect(key.getByRole("switch")).not.toBeChecked();
    await key.getByRole("switch").focus();
    await page.keyboard.press("Space");
    await key
      .getByRole("button", { name: "保存平台功能", exact: true })
      .click();
    await expect(
      key.getByRole("button", { name: "确认保存平台功能" }),
    ).toBeFocused();
    await page.keyboard.press("Escape");
    expect(f.writes).toHaveLength(0);
    await key
      .getByRole("button", { name: "保存平台功能", exact: true })
      .click();
    await key.getByRole("button", { name: "确认保存平台功能" }).click();
    await expect(key).toContainText("平台功能设置已保存");
    expect(f.writes[0]).toEqual({
      close_keylogin: "0",
      user_refund: "0",
      expected_fingerprint: actual.admin_closed.fingerprint,
    });
    await page.getByRole("tab", { name: "商户相关", exact: true }).click();
    await expect(
      page.getByRole("region", { name: "付款码支付设置", exact: true }),
    ).toContainText("付款码支付设置无法确认，请刷新页面后核对");
    const refund = page.getByRole("region", {
      name: "开启商户自助退款",
      exact: true,
    });
    await expect(refund.getByRole("switch")).not.toBeChecked();
    await refund.getByRole("switch").check();
    await refund
      .getByRole("button", { name: "保存平台功能", exact: true })
      .click();
    await refund.getByRole("button", { name: "确认保存平台功能" }).click();
    await expect(refund).toContainText("平台功能设置已保存");
    expect(f.writes[1]).toEqual({
      close_keylogin: "0",
      user_refund: "1",
      expected_fingerprint: actual.admin_key_open_refund_closed.fingerprint,
    });
    const bounds = await refund.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    const image = info.outputPath(`platform-access-${width}.png`);
    await page.screenshot({ path: image, fullPage: true });
    await info.attach(`platform-access-${width}`, {
      path: image,
      contentType: "image/png",
    });
    expect(f.errors).toEqual([]);
  });
for (const mode of ["lost", "malformed"])
  test(`${mode} committed response requires read before any new write`, async ({
    page,
  }) => {
    const f = await settings(page, mode),
      panel = page.getByRole("region", { name: "密钥登录", exact: true });
    await panel.getByRole("switch").check();
    await panel
      .getByRole("button", { name: "保存平台功能", exact: true })
      .click();
    await panel.getByRole("button", { name: "确认保存平台功能" }).click();
    await expect(panel).toContainText("保存结果无法确认");
    await expect(panel.getByRole("switch")).toHaveCount(0);
    expect(f.writes).toHaveLength(1);
    await expect(
      panel.getByRole("button", { name: "重新读取平台功能" }),
    ).toHaveCount(0);
    expect(f.reads()).toBe(1);
    await pageReload248(page);
    await expect(panel.getByRole("switch")).toBeChecked();
    expect(f.reads()).toBe(2);
    expect(f.writes).toHaveLength(1);
  });
test("closed and unknown public key-login capability retain password entry", async ({
  page,
}) => {
  let known = true;
  await page.route("**/console/v1/**", async (r) => {
    const captcha205Reply = closedLoginCaptchaFixture205(r);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(r);
    if (siteReply191) return siteReply191;
    const p = new URL(r.request().url()).pathname;
    if (p.endsWith("/session"))
      return r.fulfill({
        status: 401,
        body: JSON.stringify({ code: -1, message: "login" }),
      });
    if (p === "/console/v1/public/merchant-access")
      return known
        ? r.fulfill(ok(actual.public_closed))
        : r.fulfill({ status: 503, body: "{}" });
    if (p === "/console/v1/public/layout")
      return r.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    if (p === "/console/v1/public/connect/options")
      return r.fulfill(ok({ items: [] }));
    throw Error(`unexpected ${p}`);
  });
  await page.goto("/merchant/dashboard");
  await expect(
    page.getByText("平台已关闭密钥登录，请使用密码登录。"),
  ).toBeVisible();
  await expect(page.locator('[data-login-mode="key"]')).toHaveCount(0);
  await expect(page.locator('[name="password"]')).toBeVisible();
  known = false;
  await page.reload();
  await expect(
    page.getByText(
      "平台登录功能状态无法确认，请刷新页面后核对；密码登录仍可使用。",
    ),
  ).toBeVisible();
  await expect(page.locator('[data-login-mode="key"]')).toHaveCount(0);
  await expect(page.locator('[name="password"]')).toBeVisible();
});
