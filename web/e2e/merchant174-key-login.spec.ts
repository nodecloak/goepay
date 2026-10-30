import { closedLoginCaptchaFixture205 } from "./fixtures/login-captcha205";
import { test, expect, type Page } from "@playwright/test";
import actualKeyLogin from "./merchant174-key-login-http-fixture.json" with { type: "json" };
import actualPlatform from "./globalaccess179-http-fixture.json" with { type: "json" };
const mid = "9007199254740993",
  csrf = "c".repeat(43),
  ok = (data: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify({ code: 0, data }),
  });
async function fixture(page: Page, mode = "normal") {
  expect(actualKeyLogin.synthetic_only).toBe(true);
  expect(actualKeyLogin.schema_disposed).toBe(true);
  expect(actualKeyLogin.session_invalid_after_cleanup).toBe(true);
  expect(actualKeyLogin.cookie_or_bearer_exported).toBe(false);
  expect(actualKeyLogin.response.data.user.id).toBe(mid);
  const writes: { url: string; body: Record<string, unknown> }[] = [],
    errors: string[] = [];
  let logged = false,
    sessions = 0;
  await page.emulateMedia({ reducedMotion: "reduce" });
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (r) => {
    const captcha205Reply = closedLoginCaptchaFixture205(r);
    if (captcha205Reply) return captcha205Reply;
    const q = r.request(),
      p = new URL(q.url()).pathname;
    if (p === "/console/v1/public/merchant-access")
      return r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          code: 0,
          message: "",
          data: actualPlatform.public_default,
        }),
      });
    if (p.endsWith("/session")) {
      sessions++;
      return logged
        ? r.fulfill(
            ok({
              user: { id: mid, name: "本地密钥商户", role: "merchant" },
              csrf_token: csrf,
            }),
          )
        : r.fulfill({
            status: 401,
            contentType: "application/json",
            body: JSON.stringify({ code: -1, message: "请登录" }),
          });
    }
    if (p.endsWith("/key-login")) {
      writes.push({ url: q.url(), body: q.postDataJSON() });
      expect(q.method()).toBe("POST");
      expect(new URL(q.url()).search).toBe("");
      expect(q.postDataJSON()).toEqual({
        merchant_id: mid,
        api_key: "local-only-key",
        code: "123456",
      });
      logged = true;
      if (mode === "lost") return r.abort("failed");
      if (mode === "malformed")
        return r.fulfill({
          status: 200,
          contentType: "application/json",
          body: "{",
        });
      if (mode === "rejected") {
        logged = false;
        return r.fulfill({
          status: 401,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "invalid credentials" }),
        });
      }
      return r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(actualKeyLogin.response),
      });
    }
    return r.fulfill(
      ok({
        orders: 0,
        paid_orders: 0,
        available_money: "0.00",
        frozen_money: "0.00",
        summary: {},
        announcements: [],
        trend: [],
        merchant: { id: mid, name: "本地密钥商户" },
        business_date: "2026-10-26",
        payment_channels: [],
      }),
    );
  });
  await page.goto("/merchant/dashboard");
  await expect(page).toHaveURL(/\/merchant\/login\?return=/);
  await page.getByRole("button", { name: "密钥登录", exact: true }).click();
  await page.getByLabel("商户ID", { exact: true }).fill(mid);
  await page.getByLabel("商户密钥", { exact: true }).fill("local-only-key");
  await page.getByLabel("动态验证码").fill("123456");
  return { writes, errors, getSessions: () => sessions };
}
for (const width of [1440, 375])
  test(`explicit private key login preserves full owner and has no URL/storage secret at ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    expect(
      await page.getByLabel("商户密钥", { exact: true }).getAttribute("type"),
    ).toBe("password");
    await page.screenshot({
      path: info.outputPath(`merchant174-login-${width}.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "登录工作台", exact: true }).click();
    await expect(page).toHaveURL(/\/merchant\/dashboard$/);
    await page
      .getByRole("button", { name: "本地密钥商户", exact: true })
      .click();
    await expect(
      page.getByRole("menuitem", { name: "退出登录", exact: true }),
    ).toBeVisible();
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0].url).not.toContain("local-only-key");
    expect(
      await page.evaluate(() =>
        JSON.stringify({ ...localStorage, ...sessionStorage }),
      ),
    ).not.toContain("local-only-key");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(f.errors).toEqual([]);
  });
for (const mode of ["lost", "malformed"])
  test(`${mode} committed login clears secret and reads original session instead of retry`, async ({
    page,
  }) => {
    const f = await fixture(page, mode);
    const initialSessionReads = f.getSessions();
    await page.getByRole("button", { name: "登录工作台", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("重新检查登录状态");
    await expect(page.getByLabel("商户密钥", { exact: true })).toHaveValue("");
    await expect(
      page.getByRole("button", { name: "登录工作台", exact: true }),
    ).toBeDisabled();
    expect(f.writes).toHaveLength(1);
    await page
      .getByRole("button", { name: "重新检查登录状态", exact: true })
      .click();
    await expect(page).toHaveURL(/\/merchant\/dashboard$/);
    await page
      .getByRole("button", { name: "本地密钥商户", exact: true })
      .click();
    await expect(
      page.getByRole("menuitem", { name: "退出登录", exact: true }),
    ).toBeVisible();
    // One explicit read recovers the committed session; the fresh destination
    // document then reads it again without another credential write.
    await expect.poll(f.getSessions).toBe(initialSessionReads + 2);
    expect(f.writes).toHaveLength(1);
    expect(f.errors).toEqual([]);
  });
test("definite credential rejection permits explicit new input and mode switches erase key", async ({
  page,
}) => {
  const f = await fixture(page, "rejected");
  await page.getByRole("button", { name: "登录工作台", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("invalid credentials");
  await expect(page.getByLabel("商户密钥", { exact: true })).toHaveValue("");
  await expect(
    page.getByRole("button", { name: "登录工作台", exact: true }),
  ).toBeEnabled();
  await page.getByLabel("商户密钥", { exact: true }).fill("not-kept");
  await page.getByRole("button", { name: "密码登录", exact: true }).click();
  await page.getByRole("button", { name: "密钥登录", exact: true }).click();
  await expect(page.getByLabel("商户密钥", { exact: true })).toHaveValue("");
  expect(f.writes).toHaveLength(1);
});
