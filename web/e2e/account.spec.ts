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
  await page.getByLabel("当前登录密码").fill("current-secret-for-test");
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
    const p = new URL(route.request().url()).pathname;
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
    return route.fulfill(ok({ name: "Merchant", email: "m@example.com" }));
  });
  await page.goto("/merchant/account/settings");
  await page.getByLabel("当前密码", { exact: true }).fill("current-password");
  await page.getByLabel("新密码", { exact: true }).fill("new-password-long");
  await page
    .getByLabel("再次输入新密码", { exact: true })
    .fill("mismatched-password");
  await page.getByRole("button", { name: "修改密码", exact: true }).click();
  await expect(
    page
      .getByRole("region", { name: "账户资料与安全设置", exact: true })
      .getByRole("alert"),
  ).toContainText("两次新密码输入不一致");
  expect(changes).toBe(0);
  await page
    .getByLabel("再次输入新密码", { exact: true })
    .fill("new-password-long");
  await page.getByRole("button", { name: "修改密码", exact: true }).click();
  await expect(page.getByRole("button", { name: "登录工作台" })).toBeVisible();
  await expect(page.getByRole("status")).toHaveText("密码已修改，请重新登录");
  expect(changes).toBe(1);
});
test("cleanup requires preview and explicit confirmation with the echoed cutoff", async ({
  page,
}) => {
  const bodies: unknown[] = [];
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
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
  await page.getByRole("button", { name: "清理以上过期登录状态" }).click();
  expect(bodies).toHaveLength(1);
  await page.getByRole("button", { name: "确认清理", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("过期登录状态已清理");
  expect(bodies).toEqual([
    { preview: true, cutoff: "2026-10-22T00:00:00+08:00" },
    { preview: false, cutoff: "2026-10-21T16:00:00Z" },
  ]);
});
