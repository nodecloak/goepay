import { closedLoginCaptchaFixture205 } from "./fixtures/login-captcha205";
import { publicSiteFixture191 } from "./fixtures/site191";
import { test, expect } from "@playwright/test";
import { accountSettingsReadFixture } from "./fixtures/account-settings";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("API rotation requires explicit password confirmation and keeps new key out of storage", async ({
  page,
}) => {
  let rotations = 0;
  await page.route("**/console/v1/**", (route) => {
    const captcha205Reply = closedLoginCaptchaFixture205(route);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "test",
        }),
      );
    if (path.endsWith("/rotate")) {
      rotations++;
      expect(route.request().postDataJSON()).toEqual({
        current_password: "current-secret-for-test",
      });
      return route.fulfill(ok({ api_key: "new-test-only-key-value" }));
    }
    return route.fulfill(
      ok({
        pid: "1000",
        algorithm: "MD5",
        api_key_masked: "ab****cd",
        merchant_public_key: null,
        platform_public_key: "public-key",
      }),
    );
  });
  await page.goto("/merchant/account/api-info");
  await expect(page.getByText("ab****cd")).toBeVisible();
  await page
    .getByRole("button", { name: "重置 API 密钥", exact: true })
    .click();
  expect(rotations).toBe(0);
  await page.getByLabel("登录密码", { exact: true }).fill("current-secret-for-test");
  await page.getByRole("button", { name: "确认重置密钥" }).click();
  await expect(page.getByLabel("新 API 密钥", { exact: true })).toHaveValue(
    "new-test-only-key-value",
  );
  expect(rotations).toBe(1);
  expect(
    await page.evaluate(
      () =>
        Object.keys(localStorage).length + Object.keys(sessionStorage).length,
    ),
  ).toBe(0);
  await page.getByRole("button", { name: "已保存，隐藏密钥" }).click();
  await expect(page.getByLabel("新 API 密钥", { exact: true })).toHaveCount(0);
});
test("password confirmation blocks mismatched update then requires reauthentication on success", async ({
  page,
}) => {
  let changes = 0;
  await page.route("**/console/v1/**", (route) => {
    const captcha205Reply = closedLoginCaptchaFixture205(route);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session") && changes > 0)
      return route.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ code: -1, message: "original sessions revoked" }) });
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "test",
        }),
      );
    if (p.endsWith("/password")) {
      changes++;
      return route.fulfill(ok({}));
    }
    const widget = accountSettingsReadFixture(p);
    if (widget !== undefined) return route.fulfill(ok(widget));
    return route.fulfill(
      ok({ name: "Merchant", email: "m@example.com", has_pwd: true }),
    );
  });
  await page.goto("/merchant/account/settings");
  const account = page.getByRole("region", {
    name: "账户资料与安全设置",
    exact: true,
  });
  await expect(account.getByText("登录密码", { exact: true })).toBeVisible();
  await expect(account.getByText("已设置", { exact: true })).toBeVisible();
  await expect(account.getByLabel("当前密码", { exact: true })).toHaveCount(0);
  await account
    .getByRole("group", { name: "登录密码", exact: true })
    .getByRole("button", { name: "修改", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "修改登录密码", exact: true }),
  ).toBeVisible();
  await page.getByLabel("当前密码", { exact: true }).fill("current-password");
  await page.getByLabel("新密码", { exact: true }).fill("new-password-long");
  await page
    .getByLabel("确认密码", { exact: true })
    .fill("mismatched-password");
  await page.getByRole("button", { name: "修改密码", exact: true }).click();
  await expect(
    page
      .getByRole("dialog", { name: "修改登录密码", exact: true })
      .getByRole("alert"),
  ).toContainText("两次新密码输入不一致");
  expect(changes).toBe(0);
  await page
    .getByLabel("确认密码", { exact: true })
    .fill("new-password-long");
  await page.getByRole("button", { name: "修改密码", exact: true }).click();
  await expect(page.getByRole("button", { name: "登录工作台" })).toBeVisible();
  await expect(page).toHaveURL(/\/merchant\/login\?.*reauth=password/);
  await expect(page.getByRole("status")).toHaveText("请使用当前有效密码重新登录");
  expect(changes).toBe(1);
});

for (const width of [375, 1440]) {
  test(`original password row and dialog keyboard interactions at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let writes = 0;
    await page.route("**/console/v1/**", (route) => {
    const captcha205Reply = closedLoginCaptchaFixture205(route);
    if (captcha205Reply) return captcha205Reply;
      const siteReply191 = publicSiteFixture191(route);
      if (siteReply191) return siteReply191;
      const request = route.request(),
        path = new URL(request.url()).pathname;
      if (request.method() !== "GET") writes++;
      if (path.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "1000", name: "Merchant199", role: "merchant" },
            csrf_token: "credentials199-e2e",
          }),
        );
      if (path.endsWith("/account/profile"))
        return route.fulfill(
          ok({
            name: "Merchant199",
            email: "",
            uid: "9007199254740993",
            addtime: "2020-01-02 11:04:05",
            registration_time_known: true,
            has_pwd: true,
            pay: "1",
          }),
        );
      const widget = accountSettingsReadFixture(path);
      return route.fulfill(
        ok(
          widget === undefined
            ? { enabled: false, items: [], total: 0 }
            : widget,
        ),
      );
    });
    await page.goto("/merchant/account/settings");
    const account = page.getByRole("region", {
      name: "账户资料与安全设置",
      exact: true,
    });
    await expect(
      account.getByText("9007199254740993", { exact: true }),
    ).toBeVisible();
    await expect(
      account.getByText("2020-01-02 11:04:05", { exact: true }),
    ).toBeVisible();
    const modify = account
      .getByRole("group", { name: "登录密码", exact: true })
      .getByRole("button", { name: "修改", exact: true });
    await modify.click();
    const dialog = page.getByRole("dialog", {
        name: "修改登录密码",
        exact: true,
      }),
      current = dialog.getByLabel("当前密码", { exact: true });
    await expect(current).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    const close = dialog.getByRole("button", { name: "关闭", exact: true });
    await expect(close).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect(
      dialog.getByRole("button", { name: "修改密码", exact: true }),
    ).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(close).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(current).toBeFocused();
    await current.fill("local-current-secret199");
    await dialog
      .getByLabel("新密码", { exact: true })
      .fill("local-new-password199");
    await dialog
      .getByLabel("确认密码", { exact: true })
      .fill("local-new-password199");
    expect(
      await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth),
    ).toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(modify).toBeFocused();
    await modify.click();
    await expect(current).toHaveValue("");
    await expect(dialog.getByLabel("新密码", { exact: true })).toHaveValue("");
    await expect(
      dialog.getByLabel("确认密码", { exact: true }),
    ).toHaveValue("");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    expect(writes).toBe(0);
    expect(
      await page.evaluate(
        () =>
          Object.keys(localStorage).length + Object.keys(sessionStorage).length,
      ),
    ).toBe(0);
  });
}
test("cleanup requires preview and explicit confirmation with the echoed cutoff", async ({
  page,
}) => {
  const bodies: unknown[] = [];
  await page.route("**/console/v1/**", (route) => {
    const captcha205Reply = closedLoginCaptchaFixture205(route);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const p = new URL(route.request().url()).pathname;
    if (p === "/console/v1/public/layout") {
      expect(route.request().method()).toBe("GET");
      expect(new URL(route.request().url()).search).toBe("");
      return route.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    }
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "test",
        }),
      );
    bodies.push(route.request().postDataJSON());
    return route.fulfill(
      ok({
        expired_sessions: 2,
        expired_attempts: 3,
        cutoff: "2026-10-21T16:00:00Z",
      }),
    );
  });
  await page.goto("/admin/system/clean");
  await page.getByLabel("清理截止日期（UTC+08:00）").fill("2026-10-22");
  await page.getByRole("button", { name: "预览清理范围" }).click();
  await expect(
    page.getByRole("definition").filter({ hasText: "2026-10-21T16:00:00Z" }),
  ).toBeVisible();
  expect(bodies).toHaveLength(1);
  await page.getByRole("button", { name: "立即删除", exact: true }).click();
  expect(bodies).toHaveLength(1);
  await page.getByRole("button", { name: "确认清理", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("过期登录状态已清理");
  expect(bodies).toEqual([
    { preview: true, cutoff: "2026-10-22T00:00:00+08:00" },
    { preview: false, cutoff: "2026-10-21T16:00:00Z" },
  ]);
});
